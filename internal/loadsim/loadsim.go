// Package loadsim replays KV cache loads through a recorded network to
// compare how loaders that choose between loading and recomputing cope with
// it. A load is split into equal chunks, as CacheGen and Cake split it.
// Recompute time is a fixed cost per chunk, since this harness has no GPU;
// only the network is real.
package loadsim

import (
	"github.com/raghavs6/KVFlow/internal/nettrace"
	"github.com/raghavs6/KVFlow/internal/simulator"
)

// Load is the KV cache one request needs.
type Load struct {
	Chunks     int
	ChunkBytes float64
	// ChunkCompute is the seconds to recompute one chunk's KV cache.
	ChunkCompute float64
}

// Policy gets a request's KV cache ready and returns how long that took,
// which is its time to first token less the decode step.
type Policy interface {
	Name() string
	Load(tr *nettrace.Trace, at float64, ld Load) float64
}

// Fixed always loads or always recomputes.
type Fixed struct{ Recompute bool }

func (f Fixed) Name() string {
	if f.Recompute {
		return "always-recompute"
	}
	return "always-load"
}

func (f Fixed) Load(tr *nettrace.Trace, at float64, ld Load) float64 {
	if f.Recompute {
		return float64(ld.Chunks) * ld.ChunkCompute
	}
	return tr.SendTime(at, float64(ld.Chunks)*ld.ChunkBytes)
}

// Oracle knows the network. For each chunk in turn it takes whichever of
// loading or recomputing finishes first. That is the best choice per chunk;
// it isn't proven best over the whole load, since an earlier choice moves
// when later chunks meet the network. Like Chunked it does one chunk at a
// time, so Cake, which loads and recomputes at once, can beat it.
type Oracle struct{}

func (Oracle) Name() string { return "oracle, one at a time" }

func (Oracle) Load(tr *nettrace.Trace, at float64, ld Load) float64 {
	now := at
	for range ld.Chunks {
		now += min(tr.SendTime(now, ld.ChunkBytes), ld.ChunkCompute)
	}
	return now - at
}

// Chunked decides each chunk in turn from a learned belief, as CacheGen
// does: load it if the belief says loading is faster than recomputing,
// otherwise recompute it. Only loaded chunks are observed. The learner and
// the probe count carry over between requests, as a long-running loader's
// would.
type Chunked struct {
	Label   string
	Learner simulator.Learner
	// ProbeEvery forces a load after this many recomputed chunks in a row,
	// so a belief that the network is slow can be corrected. 0 never probes.
	ProbeEvery int

	sinceLoad int
}

func (c *Chunked) Name() string { return c.Label }

func (c *Chunked) Load(tr *nettrace.Trace, at float64, ld Load) float64 {
	now := at
	for range ld.Chunks {
		predicted := c.Learner.Startup().Seconds() + ld.ChunkBytes*c.Learner.SecondsPerByte()
		load := predicted < ld.ChunkCompute || (c.ProbeEvery > 0 && c.sinceLoad >= c.ProbeEvery)
		if !load {
			now += ld.ChunkCompute
			c.sinceLoad++
			continue
		}
		took := tr.SendTime(now, ld.ChunkBytes)
		c.Learner.Observe(ld.ChunkBytes, took)
		now += took
		c.sinceLoad = 0
	}
	return now - at
}

// Cake loads chunks from the back while recomputing from the front, at the
// same time, and is done when the two meet (Jin et al., ICML '25). It needs
// no estimate: whichever side is faster covers more chunks.
type Cake struct{}

func (Cake) Name() string { return "cake" }

func (Cake) Load(tr *nettrace.Trace, at float64, ld Load) float64 {
	computed, loaded := 0, 0
	nextCompute := at + ld.ChunkCompute
	nextLoad := at + tr.SendTime(at, ld.ChunkBytes)
	for {
		var now float64
		if nextCompute <= nextLoad {
			now = nextCompute
			computed++
			nextCompute += ld.ChunkCompute
		} else {
			now = nextLoad
			loaded++
			nextLoad += tr.SendTime(nextLoad, ld.ChunkBytes)
		}
		if computed+loaded == ld.Chunks {
			return now - at
		}
	}
}
