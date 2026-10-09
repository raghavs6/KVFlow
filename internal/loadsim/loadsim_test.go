package loadsim

import (
	"math"
	"testing"

	"github.com/raghavs6/KVFlow/internal/learner"
	"github.com/raghavs6/KVFlow/internal/nettrace"
	"github.com/raghavs6/KVFlow/internal/xfercsv"
)

func trace(t *testing.T, rows ...xfercsv.Row) *nettrace.Trace {
	t.Helper()
	tr, err := nettrace.FromRows(rows)
	if err != nil {
		t.Fatalf("FromRows() error = %v", err)
	}
	return tr
}

func lastSample(t *testing.T, secondsPerByte float64, probeEvery int) *Chunked {
	t.Helper()
	l, err := learner.NewLastSample(secondsPerByte)
	if err != nil {
		t.Fatalf("NewLastSample() error = %v", err)
	}
	return &Chunked{Label: "last-sample", Learner: l, ProbeEvery: probeEvery}
}

func assertSeconds(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v s, want %v s", name, got, want)
	}
}

// On a steady link where loading is 10x faster than recomputing, every
// policy that can load should load everything and agree.
func TestSteadyFastLinkAllAgree(t *testing.T) {
	tr := trace(t, xfercsv.Row{Bytes: 1000, Seconds: 10}) // 100 B/s
	ld := Load{Chunks: 4, ChunkBytes: 10, ChunkCompute: 1}
	for _, p := range []Policy{Fixed{}, Oracle{}, Cake{}, lastSample(t, 0.01, 0)} {
		assertSeconds(t, p.Name(), p.Load(tr, 0, ld), 0.4)
	}
	assertSeconds(t, "always-recompute", Fixed{Recompute: true}.Load(tr, 0, ld), 4)
}

// When loading and recomputing a chunk take equally long, Cake does both
// at once and halves the time.
func TestCakeUsesBothSides(t *testing.T) {
	tr := trace(t, xfercsv.Row{Bytes: 1000, Seconds: 10})
	ld := Load{Chunks: 4, ChunkBytes: 100, ChunkCompute: 1}
	assertSeconds(t, "cake", Cake{}.Load(tr, 0, ld), 2)
	assertSeconds(t, "oracle", Oracle{}.Load(tr, 0, ld), 4)
}

// A slow link, then a fast one. The first chunk is loaded on the old fast
// belief and pays for the slow link; after that, last-sample believes the
// link is slow, and without a probe it never loads again.
func TestLastSampleNeedsAProbeToSeeRecovery(t *testing.T) {
	tr := trace(t,
		xfercsv.Row{Bytes: 10, Seconds: 10},     // 1 B/s for 10 s
		xfercsv.Row{Bytes: 10000, Seconds: 100}, // then 100 B/s
	)
	ld := Load{Chunks: 4, ChunkBytes: 10, ChunkCompute: 1}

	stuck := lastSample(t, 0.01, 0)
	assertSeconds(t, "first request", stuck.Load(tr, 0, ld), 13)
	assertSeconds(t, "after recovery, no probe", stuck.Load(tr, 20, ld), 4)

	probing := lastSample(t, 0.01, 3)
	probing.Load(tr, 0, ld)
	assertSeconds(t, "after recovery, probing", probing.Load(tr, 20, ld), 0.4)

	assertSeconds(t, "oracle, first request", Oracle{}.Load(tr, 0, ld), 4)
}
