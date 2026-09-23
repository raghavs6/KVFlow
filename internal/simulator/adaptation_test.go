package simulator

import (
	"testing"
	"time"

	"github.com/raghavs6/KVFlow/internal/scheduler"
)

func TestAdaptationTimes(t *testing.T) {
	// Fast: transfer is best. Congested: wait is best.
	F := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	C := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	const (
		T = scheduler.ActionTransfer
		W = scheduler.ActionWait
	)

	tests := []struct {
		name     string
		truths   []Scenario
		believed []scheduler.Action
		want     []Adaptation
	}{
		{
			name:     "no change point",
			truths:   []Scenario{F, F, F},
			believed: []scheduler.Action{T, T, T},
			want:     nil,
		},
		{
			name:     "adapts two requests after the change",
			truths:   []Scenario{F, F, C, C, C, C},
			believed: []scheduler.Action{T, T, T, T, W, W},
			want:     []Adaptation{{At: 2, From: T, To: W, Requests: 2, Adapted: true}},
		},
		{
			name:     "already believes the new best action",
			truths:   []Scenario{C, C, F, F},
			believed: []scheduler.Action{T, T, T, T},
			want:     []Adaptation{{At: 2, From: W, To: T, Requests: 0, Adapted: true}},
		},
		{
			// The recovery window ends with the run before belief returns
			// to transfer.
			name:     "never adapts to recovery",
			truths:   []Scenario{F, F, C, C, F, F},
			believed: []scheduler.Action{T, T, T, W, W, W},
			want: []Adaptation{
				{At: 2, From: T, To: W, Requests: 1, Adapted: true},
				{At: 4, From: W, To: T, Adapted: false},
			},
		},
		{
			// Matching after the next change point does not count for the
			// earlier one.
			name:     "window ends at the next change point",
			truths:   []Scenario{F, C, C, F, F},
			believed: []scheduler.Action{T, T, T, W, W},
			want: []Adaptation{
				{At: 1, From: T, To: W, Adapted: false},
				{At: 3, From: W, To: T, Adapted: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcomes := make([]Outcome, len(tt.believed))
			for i, action := range tt.believed {
				outcomes[i].Believed = action
			}

			got, err := AdaptationTimes(tt.truths, outcomes)
			if err != nil {
				t.Fatalf("AdaptationTimes() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("AdaptationTimes() = %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("AdaptationTimes()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestAdaptationTimesRejectsLengthMismatch(t *testing.T) {
	F := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)

	if _, err := AdaptationTimes([]Scenario{F, F}, []Outcome{{}}); err == nil {
		t.Fatal("AdaptationTimes() error = nil, want a length-mismatch error")
	}
}
