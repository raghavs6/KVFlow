// Command kvloadsim reads kvxfer's CSV on stdin, treats it as the network,
// and starts one KV cache load every -interval seconds across it. Each
// loader gets the same requests at the same moments, and it prints how long
// each took to get the KV cache ready, next to an oracle that knew the
// network.
//
// The network is real; recompute is not. -prefill is an estimate of how
// many tokens per second a GPU rebuilds, so results show how loaders react
// to the network, not what a real GPU would see.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"slices"
	"text/tabwriter"

	"github.com/raghavs6/KVFlow/internal/learner"
	"github.com/raghavs6/KVFlow/internal/loadsim"
	"github.com/raghavs6/KVFlow/internal/nettrace"
	"github.com/raghavs6/KVFlow/internal/simulator"
	"github.com/raghavs6/KVFlow/internal/xfercsv"
)

// initialSecondsPerByte is kvbench's starting belief, a 10 GB/s network.
const initialSecondsPerByte = 1.0 / 10e9

func main() {
	tokens := flag.Int("tokens", 4096, "prompt tokens whose KV cache each request needs")
	chunk := flag.Int("chunk", 256, "tokens per chunk")
	bytesPerToken := flag.Float64("kv-bytes", 131072, "KV cache bytes per token (Llama 3.1 8B in bf16)")
	prefill := flag.Float64("prefill", 2500, "tokens per second the GPU recomputes (an estimate)")
	interval := flag.Float64("interval", 1, "seconds between requests")
	probe := flag.Int("probe", 20, "recomputed chunks in a row before a probing loader loads one")
	flag.Parse()
	if *tokens <= 0 || *chunk <= 0 || *tokens%*chunk != 0 {
		log.Fatal("-tokens must be a positive multiple of -chunk")
	}

	rows, err := xfercsv.Parse(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	tr, err := nettrace.FromRows(rows)
	if err != nil {
		log.Fatal(err)
	}
	ld := loadsim.Load{
		Chunks:       *tokens / *chunk,
		ChunkBytes:   float64(*chunk) * *bytesPerToken,
		ChunkCompute: float64(*chunk) / *prefill,
	}
	policies, err := newPolicies(*probe)
	if err != nil {
		log.Fatal(err)
	}
	var starts []float64
	for at := 0.0; at < tr.Duration(); at += *interval {
		starts = append(starts, at)
	}
	times := make([][]float64, len(policies))
	for i, p := range policies {
		for _, at := range starts {
			times[i] = append(times[i], p.Load(tr, at, ld))
		}
	}
	fmt.Printf("%d requests, %d chunks of %.0f MiB, recompute %.0f ms per chunk; loading wins above %.2f GB/s\n\n",
		len(starts), ld.Chunks, ld.ChunkBytes/(1<<20), ld.ChunkCompute*1000, ld.ChunkBytes/ld.ChunkCompute/1e9)
	write(os.Stdout, policies, times)
}

func newPolicies(probe int) ([]loadsim.Policy, error) {
	policies := []loadsim.Policy{
		loadsim.Oracle{}, loadsim.Fixed{}, loadsim.Fixed{Recompute: true}, loadsim.Cake{},
	}
	learners := []struct {
		name string
		new  func() (simulator.Learner, error)
	}{
		{"last-sample", func() (simulator.Learner, error) { return learner.NewLastSample(initialSecondsPerByte) }},
		{"ewma a=0.5", func() (simulator.Learner, error) { return learner.NewEWMA(0.5, initialSecondsPerByte) }},
		{"line a=0.5", func() (simulator.Learner, error) { return learner.NewLine(0.5, initialSecondsPerByte) }},
	}
	for _, l := range learners {
		for _, k := range []int{0, probe} {
			lr, err := l.new()
			if err != nil {
				return nil, err
			}
			name := l.name
			if k > 0 {
				name = fmt.Sprintf("%s, probe %d", l.name, k)
			}
			policies = append(policies, &loadsim.Chunked{Label: name, Learner: lr, ProbeEvery: k})
		}
	}
	return policies, nil
}

// write prints, for each policy, its load times in seconds and how much
// longer than the oracle it took on average.
func write(w io.Writer, policies []loadsim.Policy, times [][]float64) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "policy\tmean\tp50\tp99\tmax\tvs oracle\t>2x oracle\t")
	oracle := times[0]
	for i, p := range policies {
		var sum, excess float64
		var over int
		for j, s := range times[i] {
			sum += s
			excess += s - oracle[j]
			if s > 2*oracle[j] {
				over++
			}
		}
		n := float64(len(times[i]))
		sorted := slices.Sorted(slices.Values(times[i]))
		fmt.Fprintf(tw, "%s\t%.3f\t%.3f\t%.3f\t%.3f\t%+.1f%%\t%d\t\n", p.Name(),
			sum/n, quantile(sorted, 0.5), quantile(sorted, 0.99), sorted[len(sorted)-1],
			100*excess/sumOf(oracle), over)
	}
	tw.Flush()
}

func quantile(sorted []float64, q float64) float64 {
	return sorted[int(math.Ceil(q*float64(len(sorted))))-1]
}

func sumOf(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s
}
