package simulator

import (
	"errors"
	"math/rand/v2"
)

var errInvalidPeriod = errors.New("periods must satisfy 0 < minPeriod <= maxPeriod")

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
