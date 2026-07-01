package main

import (
	"errors"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
)

func TestPercentileNearestRank(t *testing.T) {
	xs := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	cases := []struct {
		p    float64
		want float64
	}{
		{50, 50}, // ceil(0.5*10)=5 -> idx 4 -> 50
		{95, 100},
		{99, 100},
		{100, 100},
		{10, 10},
	}
	for _, c := range cases {
		if got := percentile(xs, c.p); got != c.want {
			t.Errorf("percentile(%v) = %v, want %v", c.p, got, c.want)
		}
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile(nil) = %v, want 0", got)
	}
}

func TestSummarizeDropsWarmupAndCountsOutcomes(t *testing.T) {
	warmup := 10 * time.Second
	window := 10 * time.Second
	samples := []sample{
		{offset: 5 * time.Second, dur: 1 * time.Second, res: outcomeOK}, // warm-up: dropped
		{offset: 11 * time.Second, dur: 100 * time.Millisecond, res: outcomeOK},
		{offset: 12 * time.Second, dur: 300 * time.Millisecond, res: outcomeOK},
		{offset: 13 * time.Second, res: outcomeCAS},
		{offset: 14 * time.Second, res: outcomeErr},
	}
	r := summarize(samples, 8, "branch", 1, 3, warmup, window, "1-10 x 2048B")

	if r.Pushes != 4 {
		t.Errorf("Pushes = %d, want 4 (warm-up sample excluded)", r.Pushes)
	}
	if r.OK != 2 || r.CASFailures != 1 || r.OtherErrors != 1 {
		t.Errorf("outcomes ok=%d cas=%d err=%d, want 2/1/1", r.OK, r.CASFailures, r.OtherErrors)
	}
	if want := float64(2) / window.Seconds(); r.OpsPerSec != want {
		t.Errorf("OpsPerSec = %v, want %v", r.OpsPerSec, want)
	}
	// Percentiles computed over OK durations only (100ms, 300ms).
	if r.P50ms != 100 || r.Maxms != 300 {
		t.Errorf("p50=%v max=%v, want 100/300", r.P50ms, r.Maxms)
	}
	if r.Concurrency != 8 || r.Nodes != 3 {
		t.Errorf("metadata not carried through: %+v", r)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want outcome
	}{
		{"nil", nil, outcomeOK},
		{"up-to-date", git.NoErrAlreadyUpToDate, outcomeOK},
		{"non-ff sentinel", git.ErrNonFastForwardUpdate, outcomeCAS},
		{"ref changed text", errors.New("rpc: reference has changed"), outcomeCAS},
		{"fetch first text", errors.New("failed to push: fetch first"), outcomeCAS},
		{"transport", errors.New("dial tcp: connection refused"), outcomeErr},
	}
	for _, c := range cases {
		if got := classify(c.err); got != c.want {
			t.Errorf("%s: classify = %v, want %v", c.name, got, c.want)
		}
	}
}

// session strategy records clone (op=clone) and push (op=push) samples in one
// slice; summarize must report them as separate metrics, not conflate them.
func TestSummarizeSplitsCloneAndPush(t *testing.T) {
	warmup := 0 * time.Second
	window := 10 * time.Second
	samples := []sample{
		{offset: 1 * time.Second, dur: 200 * time.Millisecond, res: outcomeOK, op: opClone},
		{offset: 2 * time.Second, dur: 400 * time.Millisecond, res: outcomeOK, op: opClone},
		{offset: 3 * time.Second, res: outcomeErr, op: opClone}, // failed clone
		{offset: 4 * time.Second, dur: 30 * time.Millisecond, res: outcomeOK, op: opPush},
		{offset: 5 * time.Second, dur: 50 * time.Millisecond, res: outcomeOK, op: opPush},
	}
	r := summarize(samples, 8, "session", 1, 3, warmup, window, "1-10 x 2048B")

	if r.Pushes != 2 || r.OK != 2 {
		t.Fatalf("push side: Pushes=%d OK=%d, want 2/2 (clones excluded)", r.Pushes, r.OK)
	}
	if r.P50ms != 30 {
		t.Fatalf("push p50=%v, want 30 (clone durations must not leak in)", r.P50ms)
	}
	if r.Clones != 3 || r.CloneOK != 2 || r.CloneErrors != 1 {
		t.Fatalf("clone side: clones=%d ok=%d err=%d, want 3/2/1", r.Clones, r.CloneOK, r.CloneErrors)
	}
	if r.CloneP50ms != 200 {
		t.Fatalf("clone p50=%v, want 200", r.CloneP50ms)
	}
}
