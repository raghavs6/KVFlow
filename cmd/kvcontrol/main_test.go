package main

import (
	"testing"
	"time"

	"github.com/raghavs6/KVFlow/internal/costmodel"
	"github.com/raghavs6/KVFlow/internal/scheduler"
)

// Transfer wins above bytes/recompute and recompute wins below it.
func TestScenarioTipsAtBytesOverRecompute(t *testing.T) {
	// Tips at 0.5 MiB/s. Not 1 s, where 1/recompute and recompute are equal
	// and an inverted rate would pass.
	s := scenario(1<<20, 2*time.Second)
	for _, tc := range []struct {
		bandwidth float64
		want      scheduler.Action
	}{
		{1 << 20, scheduler.ActionTransfer},
		{0.25 * (1 << 20), scheduler.ActionRecompute},
	} {
		candidates, err := costmodel.Estimate(costmodel.Inputs{
			QueueA:               s.Source.Queue,
			QueueB:               s.Destination.Queue,
			PrefixTokens:         s.Request.PrefixTokens,
			SuffixTokens:         s.Request.SuffixTokens,
			PrefillTokensPerSecA: s.Source.PrefillTokensPerSec,
			PrefillTokensPerSecB: s.Destination.PrefillTokensPerSec,
			KVBytesPerToken:      s.KVBytesPerToken,
			BandwidthBytesPerSec: tc.bandwidth,
		})
		if err != nil {
			t.Fatal(err)
		}
		choice, err := scheduler.ChooseLowestTTFT(candidates)
		if err != nil {
			t.Fatal(err)
		}
		if choice.Action != tc.want {
			t.Errorf("at %.0f B/s chose %s, want %s (candidates %v)", tc.bandwidth, choice.Action, tc.want, candidates)
		}
	}
}
