package simulator

import "errors"

var errInvalidPeriod = errors.New("period must be positive")

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

// Flapping returns n scenarios that switch between fast and slow every period
// requests, starting fast.
func Flapping(n, period int, fast, slow Scenario) ([]Scenario, error) {
	if period <= 0 {
		return nil, errInvalidPeriod
	}

	truths := Stable(n, fast)
	for i := range truths {
		if (i/period)%2 == 1 {
			truths[i] = slow
		}
	}
	return truths, nil
}
