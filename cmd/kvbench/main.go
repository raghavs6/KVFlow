// Command kvbench compares static and adaptive policies on simulated
// workloads and prints mean regret per request.
package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"text/tabwriter"
	"time"

	"github.com/raghavs6/KVFlow/internal/learner"
	"github.com/raghavs6/KVFlow/internal/scheduler"
	"github.com/raghavs6/KVFlow/internal/simulator"
)

const (
	requests      = 2000
	flapMinPeriod = 150
	flapMaxPeriod = 250
	workloadSeed  = 1
	seeds         = 20
	noiseSpread   = 0.3
	fastBandwidth = 10_000_000_000
	slowBandwidth = 1_000_000_000
	sourceQueue   = 1500 * time.Millisecond
	destQueue     = 50 * time.Millisecond
	prefixTokens  = 50_000
	// Only the startup workloads use these. The simulated truth pays the
	// startup on every transfer; the cost model can't see it.
	transferStartup   = 200 * time.Millisecond
	smallPrefixTokens = 2_000
	suffixTokens      = 1_000
	prefillRate       = 50_000
	kvBytesPerTokn    = 40_000
)

type workload struct {
	name   string
	truths []simulator.Scenario
}

type policy struct {
	name string
	// run returns per-request outcomes for one seed.
	run func(truths []simulator.Scenario, seed uint64) ([]simulator.Outcome, error)
}

func main() {
	fast := scenario(fastBandwidth)
	slow := scenario(slowBandwidth)
	flapping, err := simulator.Flapping(
		rand.New(rand.NewPCG(workloadSeed, workloadSeed)), requests, flapMinPeriod, flapMaxPeriod, fast, slow)
	if err != nil {
		log.Fatal(err)
	}
	// Only these workloads have change points to adapt to.
	changing := []workload{
		{"slowdown+recovery", simulator.SlowdownThenRecovery(requests, fast, slow)},
		{"flapping", flapping},
	}
	mixed, err := simulator.MixedPrefixes(
		rand.New(rand.NewPCG(workloadSeed, workloadSeed)), requests, startupScenario(), []int{smallPrefixTokens, prefixTokens})
	if err != nil {
		log.Fatal(err)
	}
	workloads := append([]workload{
		{"stable-fast", simulator.Stable(requests, fast)},
		{"stable-slow", simulator.Stable(requests, slow)},
	}, changing...)
	workloads = append(workloads,
		workload{"startup", simulator.Stable(requests, startupScenario())},
		workload{"startup+mixed", mixed},
	)

	policies := []policy{{
		name: "static",
		run: func(truths []simulator.Scenario, _ uint64) ([]simulator.Outcome, error) {
			return simulator.RunStatic(fastBandwidth, truths)
		},
	}}
	learners := []struct {
		name string
		new  func(alpha float64) (simulator.Learner, error)
	}{
		{"adaptive", func(alpha float64) (simulator.Learner, error) { return learner.NewEWMA(alpha, 1.0/fastBandwidth) }},
		{"line", func(alpha float64) (simulator.Learner, error) { return learner.NewLine(alpha, 1.0/fastBandwidth) }},
	}
	for _, lr := range learners {
		for _, alpha := range []float64{0.1, 0.5} {
			for _, probeEvery := range []int{0, 5, 20, 100} {
				policies = append(policies, policy{
					name: fmt.Sprintf("%s K=%d α=%.1f", lr.name, probeEvery, alpha),
					run: func(truths []simulator.Scenario, seed uint64) ([]simulator.Outcome, error) {
						observe, err := simulator.NoisyObserve(rand.New(rand.NewPCG(seed, seed)), noiseSpread)
						if err != nil {
							return nil, err
						}
						l, err := lr.new(alpha)
						if err != nil {
							return nil, err
						}
						return simulator.RunAdaptive(l, probeEvery, observe, truths)
					},
				})
			}
		}
	}

	fmt.Printf("Mean regret per request (ms), %d requests, %d seeds, noise ±%.0f%%, flap period %d–%d\n\n",
		requests, seeds, noiseSpread*100, flapMinPeriod, flapMaxPeriod)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprint(w, "policy\t")
	for _, wl := range workloads {
		fmt.Fprintf(w, "%s\t", wl.name)
	}
	fmt.Fprintln(w)

	for _, p := range policies {
		fmt.Fprintf(w, "%s\t", p.name)
		for _, wl := range workloads {
			mean, err := meanRegretMs(p, wl.truths)
			if err != nil {
				log.Fatalf("%s on %s: %v", p.name, wl.name, err)
			}
			fmt.Fprintf(w, "%.1f\t", mean)
		}
		fmt.Fprintln(w)
	}
	w.Flush()

	fmt.Println("\nMean requests until the policy believes the new best action")
	fmt.Println("(↓ slowdown: recompute becomes best, ↑ recovery: transfer becomes best)")
	fmt.Println()
	w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprint(w, "policy\t")
	for _, wl := range changing {
		fmt.Fprintf(w, "%s ↓\t%s ↑\t", wl.name, wl.name)
	}
	fmt.Fprintln(w)

	for _, p := range policies {
		fmt.Fprintf(w, "%s\t", p.name)
		for _, wl := range changing {
			for _, to := range []scheduler.Action{scheduler.ActionRecompute, scheduler.ActionTransfer} {
				mean, adapted, err := meanAdaptation(p, wl.truths, to)
				if err != nil {
					log.Fatalf("%s on %s: %v", p.name, wl.name, err)
				}
				if adapted {
					fmt.Fprintf(w, "%.1f\t", mean)
				} else {
					fmt.Fprint(w, "never\t")
				}
			}
		}
		fmt.Fprintln(w)
	}
	w.Flush()

	fmt.Println("\nMean transfer underprediction (ms): actual − predicted TTFT over executed transfers")
	fmt.Println()
	w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprint(w, "policy\t")
	for _, wl := range workloads {
		fmt.Fprintf(w, "%s\t", wl.name)
	}
	fmt.Fprintln(w)

	for _, p := range policies {
		fmt.Fprintf(w, "%s\t", p.name)
		for _, wl := range workloads {
			mean, transferred, err := meanTransferErrorMs(p, wl.truths)
			if err != nil {
				log.Fatalf("%s on %s: %v", p.name, wl.name, err)
			}
			if transferred {
				fmt.Fprintf(w, "%.1f\t", mean)
			} else {
				fmt.Fprint(w, "—\t")
			}
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}

// meanRegretMs averages regret over every request of every seed.
func meanRegretMs(p policy, truths []simulator.Scenario) (float64, error) {
	var total time.Duration
	for seed := range uint64(seeds) {
		outcomes, err := p.run(truths, seed)
		if err != nil {
			return 0, err
		}
		for _, o := range outcomes {
			total += o.Regret
		}
	}
	return float64(total) / float64(time.Millisecond) / float64(seeds*len(truths)), nil
}

// meanAdaptation averages adaptation time over every change point, across all
// seeds, where to becomes the best action. adapted is false if the policy
// failed to adapt at any of them.
func meanAdaptation(p policy, truths []simulator.Scenario, to scheduler.Action) (mean float64, adapted bool, err error) {
	total, count := 0, 0
	for seed := range uint64(seeds) {
		outcomes, err := p.run(truths, seed)
		if err != nil {
			return 0, false, err
		}
		adaptations, err := simulator.AdaptationTimes(truths, outcomes)
		if err != nil {
			return 0, false, err
		}
		for _, a := range adaptations {
			if a.To != to {
				continue
			}
			if !a.Adapted {
				return 0, false, nil
			}
			total += a.Requests
			count++
		}
	}
	return float64(total) / float64(count), true, nil
}

// meanTransferErrorMs averages actual minus predicted TTFT over every executed
// transfer of every seed. Only transfers are counted because the other
// actions' inputs are reported fresh and can't be mispredicted. transferred
// is false if the policy never transferred.
func meanTransferErrorMs(p policy, truths []simulator.Scenario) (mean float64, transferred bool, err error) {
	var total time.Duration
	count := 0
	for seed := range uint64(seeds) {
		outcomes, err := p.run(truths, seed)
		if err != nil {
			return 0, false, err
		}
		for _, o := range outcomes {
			if o.Executed == scheduler.ActionTransfer {
				total += o.Actual - o.Predicted
				count++
			}
		}
	}
	if count == 0 {
		return 0, false, nil
	}
	return float64(total) / float64(time.Millisecond) / float64(count), true, nil
}

// startupScenario is a fast network whose transfers pay a hidden startup.
func startupScenario() simulator.Scenario {
	s := scenario(fastBandwidth)
	s.TransferStartup = transferStartup
	return s
}

func scenario(bandwidth float64) simulator.Scenario {
	return simulator.Scenario{
		Source:               simulator.Worker{Queue: sourceQueue, PrefillTokensPerSec: prefillRate},
		Destination:          simulator.Worker{Queue: destQueue, PrefillTokensPerSec: prefillRate},
		Request:              simulator.Request{PrefixTokens: prefixTokens, SuffixTokens: suffixTokens},
		KVBytesPerToken:      kvBytesPerTokn,
		BandwidthBytesPerSec: bandwidth,
	}
}
