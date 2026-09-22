package costmodel

import (
	"math"
	"testing"
	"time"

	"github.com/raghavs6/KVFlow/internal/scheduler"
)

func TestEstimate(t *testing.T) {
	tests := []struct {
		name   string
		inputs Inputs
		want   []scheduler.Candidate
	}{
		{
			name: "transfer wins with fast network",
			inputs: baseInputs(
				400*time.Millisecond,
				50*time.Millisecond,
				10_000_000_000,
			),
			want: []scheduler.Candidate{
				{Action: scheduler.ActionWait, EstimatedTTFT: 420 * time.Millisecond},
				{Action: scheduler.ActionTransfer, EstimatedTTFT: 220 * time.Millisecond},
				{Action: scheduler.ActionRecompute, EstimatedTTFT: 1070 * time.Millisecond},
			},
		},
		{
			name: "recompute beats transfer with congested network",
			inputs: baseInputs(
				400*time.Millisecond,
				50*time.Millisecond,
				1_000_000_000,
			),
			want: []scheduler.Candidate{
				{Action: scheduler.ActionWait, EstimatedTTFT: 420 * time.Millisecond},
				{Action: scheduler.ActionTransfer, EstimatedTTFT: 2020 * time.Millisecond},
				{Action: scheduler.ActionRecompute, EstimatedTTFT: 1070 * time.Millisecond},
			},
		},
		{
			name: "destination queue overlaps transfer",
			inputs: baseInputs(
				400*time.Millisecond,
				300*time.Millisecond,
				10_000_000_000,
			),
			want: []scheduler.Candidate{
				{Action: scheduler.ActionWait, EstimatedTTFT: 420 * time.Millisecond},
				{Action: scheduler.ActionTransfer, EstimatedTTFT: 320 * time.Millisecond},
				{Action: scheduler.ActionRecompute, EstimatedTTFT: 1320 * time.Millisecond},
			},
		},
		{
			name: "exact tie keeps wait first",
			inputs: Inputs{
				PrefixTokens:         0,
				SuffixTokens:         0,
				PrefillTokensPerSecA: 50_000,
				PrefillTokensPerSecB: 50_000,
				KVBytesPerToken:      40_000,
				BandwidthBytesPerSec: 10_000_000_000,
			},
			want: []scheduler.Candidate{
				{Action: scheduler.ActionWait, EstimatedTTFT: 0},
				{Action: scheduler.ActionTransfer, EstimatedTTFT: 0},
				{Action: scheduler.ActionRecompute, EstimatedTTFT: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Estimate(tt.inputs)
			if err != nil {
				t.Fatalf("Estimate() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Estimate() returned %d candidates, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("Estimate()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestEstimateRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Inputs)
	}{
		{name: "negative queue A", mutate: func(in *Inputs) { in.QueueA = -time.Nanosecond }},
		{name: "negative queue B", mutate: func(in *Inputs) { in.QueueB = -time.Nanosecond }},
		{name: "negative prefix tokens", mutate: func(in *Inputs) { in.PrefixTokens = -1 }},
		{name: "negative suffix tokens", mutate: func(in *Inputs) { in.SuffixTokens = -1 }},
		{name: "zero prefill rate A", mutate: func(in *Inputs) { in.PrefillTokensPerSecA = 0 }},
		{name: "negative prefill rate A", mutate: func(in *Inputs) { in.PrefillTokensPerSecA = -1 }},
		{name: "NaN prefill rate A", mutate: func(in *Inputs) { in.PrefillTokensPerSecA = math.NaN() }},
		{name: "infinite prefill rate A", mutate: func(in *Inputs) { in.PrefillTokensPerSecA = math.Inf(1) }},
		{name: "zero prefill rate B", mutate: func(in *Inputs) { in.PrefillTokensPerSecB = 0 }},
		{name: "negative prefill rate B", mutate: func(in *Inputs) { in.PrefillTokensPerSecB = -1 }},
		{name: "NaN prefill rate B", mutate: func(in *Inputs) { in.PrefillTokensPerSecB = math.NaN() }},
		{name: "infinite prefill rate B", mutate: func(in *Inputs) { in.PrefillTokensPerSecB = math.Inf(1) }},
		{name: "zero KV bytes per token", mutate: func(in *Inputs) { in.KVBytesPerToken = 0 }},
		{name: "negative KV bytes per token", mutate: func(in *Inputs) { in.KVBytesPerToken = -1 }},
		{name: "NaN KV bytes per token", mutate: func(in *Inputs) { in.KVBytesPerToken = math.NaN() }},
		{name: "infinite KV bytes per token", mutate: func(in *Inputs) { in.KVBytesPerToken = math.Inf(1) }},
		{name: "zero bandwidth", mutate: func(in *Inputs) { in.BandwidthBytesPerSec = 0 }},
		{name: "negative bandwidth", mutate: func(in *Inputs) { in.BandwidthBytesPerSec = -1 }},
		{name: "NaN bandwidth", mutate: func(in *Inputs) { in.BandwidthBytesPerSec = math.NaN() }},
		{name: "infinite bandwidth", mutate: func(in *Inputs) { in.BandwidthBytesPerSec = math.Inf(1) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := baseInputs(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
			tt.mutate(&inputs)

			if _, err := Estimate(inputs); err == nil {
				t.Fatal("Estimate() error = nil, want an invalid-input error")
			}
		})
	}
}

func TestEstimateFeedsScheduler(t *testing.T) {
	candidates, err := Estimate(baseInputs(
		400*time.Millisecond,
		50*time.Millisecond,
		10_000_000_000,
	))
	if err != nil {
		t.Fatalf("Estimate() error = %v", err)
	}

	got, err := scheduler.ChooseLowestTTFT(candidates)
	if err != nil {
		t.Fatalf("ChooseLowestTTFT() error = %v", err)
	}
	want := scheduler.Candidate{
		Action:        scheduler.ActionTransfer,
		EstimatedTTFT: 220 * time.Millisecond,
	}
	if got != want {
		t.Fatalf("ChooseLowestTTFT() = %+v, want %+v", got, want)
	}
}

func TestEstimateTieFeedsSchedulerInFixedOrder(t *testing.T) {
	candidates, err := Estimate(Inputs{
		PrefillTokensPerSecA: 50_000,
		PrefillTokensPerSecB: 50_000,
		KVBytesPerToken:      40_000,
		BandwidthBytesPerSec: 10_000_000_000,
	})
	if err != nil {
		t.Fatalf("Estimate() error = %v", err)
	}

	got, err := scheduler.ChooseLowestTTFT(candidates)
	if err != nil {
		t.Fatalf("ChooseLowestTTFT() error = %v", err)
	}
	want := scheduler.Candidate{Action: scheduler.ActionWait, EstimatedTTFT: 0}
	if got != want {
		t.Fatalf("ChooseLowestTTFT() = %+v, want %+v", got, want)
	}
}

func baseInputs(queueA, queueB time.Duration, bandwidth float64) Inputs {
	return Inputs{
		QueueA:               queueA,
		QueueB:               queueB,
		PrefixTokens:         50_000,
		SuffixTokens:         1_000,
		PrefillTokensPerSecA: 50_000,
		PrefillTokensPerSecB: 50_000,
		KVBytesPerToken:      40_000,
		BandwidthBytesPerSec: bandwidth,
	}
}
