package simulator

import (
	"errors"
	"math"
	"math/rand/v2"
)

var (
	errInvalidPeriod = errors.New("periods must satisfy 0 < minPeriod <= maxPeriod")
	errNoPrefixSizes = errors.New("at least one prefix size is required")
)

// Stable returns n copies of scenario: conditions never change.
func Stable(n int, scenario Scenario) []Scenario {
	truths := make([]Scenario, n)
	for i := range truths {
		truths[i] = scenario
	}
	return truths
}

// SlowdownThenRecovery returns n scenarios that are fast for the first third,
// slow for the second third, and fast again for the rest.
func SlowdownThenRecovery(n int, fast, slow Scenario) []Scenario {
	truths := Stable(n, fast)
	for i := n / 3; i < 2*n/3; i++ {
		truths[i] = slow
	}
	return truths
}

// Flapping returns n scenarios that alternate between fast and slow phases,
// starting fast. Each phase lasts a uniformly random number of requests in
// [minPeriod, maxPeriod], so a policy that probes on a fixed schedule cannot
// line up with the changes by coincidence. Equal rng seeds give equal
// workloads.
func Flapping(rng *rand.Rand, n, minPeriod, maxPeriod int, fast, slow Scenario) ([]Scenario, error) {
	if minPeriod <= 0 || maxPeriod < minPeriod {
		return nil, errInvalidPeriod
	}

	truths := Stable(n, fast)
	isSlow := false
	for start := 0; start < n; {
		end := min(n, start+minPeriod+rng.IntN(maxPeriod-minPeriod+1))
		if isSlow {
			for i := start; i < end; i++ {
				truths[i] = slow
			}
		}
		isSlow = !isSlow
		start = end
	}
	return truths, nil
}

// MixedPrefixes returns n copies of scenario whose prefix size is drawn
// uniformly from prefixTokens for each request. Equal rng seeds give equal
// workloads.
func MixedPrefixes(rng *rand.Rand, n int, scenario Scenario, prefixTokens []int) ([]Scenario, error) {
	if len(prefixTokens) == 0 {
		return nil, errNoPrefixSizes
	}

	truths := Stable(n, scenario)
	for i := range truths {
		truths[i].Request.PrefixTokens = prefixTokens[rng.IntN(len(prefixTokens))]
	}
	return truths, nil
}

// Drift returns n copies of fast whose bandwidth drifts to slow's by the
// middle of the run and back to fast's by the end. Seconds per byte changes
// by the same amount each request, so transfer time rises and falls steadily,
// like a link that gradually congests and then clears.
func Drift(n int, fast, slow Scenario) []Scenario {
	truths := Stable(n, fast)
	if n < 2 {
		return truths
	}

	fastSPB := 1 / fast.BandwidthBytesPerSec
	slowSPB := 1 / slow.BandwidthBytesPerSec
	half := float64(n-1) / 2
	for i := range truths {
		// 0 at either end, 1 in the middle.
		toSlow := 1 - math.Abs(float64(i)-half)/half
		truths[i].BandwidthBytesPerSec = 1 / (fastSPB + toSlow*(slowSPB-fastSPB))
	}
	return truths
}
