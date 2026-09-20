package scheduler

import (
	"errors"
	"testing"
	"time"
)

func TestChooseLowestTTFT(t *testing.T) {
	tests := []struct {
		name       string
		candidates []Candidate
		want       Candidate
	}{
		{
			name: "project example selects transfer",
			candidates: []Candidate{
				{Action: ActionWait, EstimatedTTFT: 420 * time.Millisecond},
				{Action: ActionTransfer, EstimatedTTFT: 140 * time.Millisecond},
				{Action: ActionRecompute, EstimatedTTFT: 260 * time.Millisecond},
			},
			want: Candidate{Action: ActionTransfer, EstimatedTTFT: 140 * time.Millisecond},
		},
		{
			name: "wait can win",
			candidates: []Candidate{
				{Action: ActionWait, EstimatedTTFT: 100 * time.Millisecond},
				{Action: ActionTransfer, EstimatedTTFT: 200 * time.Millisecond},
				{Action: ActionRecompute, EstimatedTTFT: 300 * time.Millisecond},
			},
			want: Candidate{Action: ActionWait, EstimatedTTFT: 100 * time.Millisecond},
		},
		{
			name: "recompute can win",
			candidates: []Candidate{
				{Action: ActionWait, EstimatedTTFT: 300 * time.Millisecond},
				{Action: ActionTransfer, EstimatedTTFT: 200 * time.Millisecond},
				{Action: ActionRecompute, EstimatedTTFT: 100 * time.Millisecond},
			},
			want: Candidate{Action: ActionRecompute, EstimatedTTFT: 100 * time.Millisecond},
		},
		{
			name: "tie preserves input order",
			candidates: []Candidate{
				{Action: ActionRecompute, EstimatedTTFT: 100 * time.Millisecond},
				{Action: ActionTransfer, EstimatedTTFT: 100 * time.Millisecond},
			},
			want: Candidate{Action: ActionRecompute, EstimatedTTFT: 100 * time.Millisecond},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ChooseLowestTTFT(tt.candidates)
			if err != nil {
				t.Fatalf("ChooseLowestTTFT() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ChooseLowestTTFT() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestChooseLowestTTFTRejectsEmptyCandidates(t *testing.T) {
	_, err := ChooseLowestTTFT(nil)
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("ChooseLowestTTFT() error = %v, want %v", err, ErrNoCandidates)
	}
}
