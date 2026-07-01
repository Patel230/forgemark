package main

import (
	"math"
	"sort"
	"time"
)

// outcome classifies a single push attempt.
type outcome int

const (
	outcomeOK  outcome = iota // push landed
	outcomeCAS                // rejected by ref CAS / non-fast-forward (contention)
	outcomeErr                // any other failure (transport, auth, server error)
)

// opKind distinguishes which operation a sample measured. Zero value is opPush
// so existing push-only call sites need no change.
type opKind int

const (
	opPush  opKind = iota // a receive-pack push (the write path)
	opClone               // a clone (the read path; session strategy)
)

// sample is one recorded operation. offset is measured from the start of the
// concurrency level so warm-up samples can be dropped post-hoc.
type sample struct {
	offset time.Duration
	dur    time.Duration
	res    outcome
	op     opKind
}

// levelResult is the published summary for one concurrency level. JSON tags
// are the columns charts consume.
type levelResult struct {
	Concurrency int     `json:"concurrency"`
	Strategy    string  `json:"strategy"`
	Repos       int     `json:"repos"`
	Nodes       int     `json:"nodes"`
	WindowSec   float64 `json:"window_sec"`
	Pushes      int     `json:"pushes"` // attempts in the measured window
	OK          int     `json:"ok"`     // successful pushes
	CASFailures int     `json:"cas_failures"`
	OtherErrors int     `json:"other_errors"`
	OpsPerSec   float64 `json:"ops_per_sec"` // OK / window
	P50ms       float64 `json:"p50_ms"`
	P95ms       float64 `json:"p95_ms"`
	P99ms       float64 `json:"p99_ms"`
	P999ms      float64 `json:"p999_ms"`
	Maxms       float64 `json:"max_ms"`
	CommitFiles string  `json:"commit_files"` // e.g. "1-10 x 2048B"

	// Session-strategy read side (op==clone). Omitted in push/repo modes.
	Clones      int     `json:"clones,omitempty"`
	CloneOK     int     `json:"clone_ok,omitempty"`
	CloneErrors int     `json:"clone_errors,omitempty"`
	CloneP50ms  float64 `json:"clone_p50_ms,omitempty"`
	CloneP95ms  float64 `json:"clone_p95_ms,omitempty"`
	CloneP99ms  float64 `json:"clone_p99_ms,omitempty"`
}

// summarize folds raw samples into a levelResult, dropping anything inside the
// warm-up window and computing exact percentiles over the OK samples.
func summarize(samples []sample, concurrency int, strategy string, repos, nodes int,
	warmup, window time.Duration, commitFiles string) levelResult {
	r := levelResult{
		Concurrency: concurrency,
		Strategy:    strategy,
		Repos:       repos,
		Nodes:       nodes,
		WindowSec:   window.Seconds(),
		CommitFiles: commitFiles,
	}

	var okDurs, cloneDurs []float64
	for _, s := range samples {
		if s.offset < warmup {
			continue // warm-up: excluded from every statistic
		}
		if s.op == opClone {
			r.Clones++
			if s.res == outcomeOK {
				r.CloneOK++
				cloneDurs = append(cloneDurs, float64(s.dur)/float64(time.Millisecond))
			} else {
				r.CloneErrors++
			}
			continue
		}
		r.Pushes++
		switch s.res {
		case outcomeOK:
			r.OK++
			okDurs = append(okDurs, float64(s.dur)/float64(time.Millisecond))
		case outcomeCAS:
			r.CASFailures++
		case outcomeErr:
			r.OtherErrors++
		}
	}

	if window > 0 {
		r.OpsPerSec = float64(r.OK) / window.Seconds()
	}
	if len(okDurs) > 0 {
		sort.Float64s(okDurs)
		r.P50ms = percentile(okDurs, 50)
		r.P95ms = percentile(okDurs, 95)
		r.P99ms = percentile(okDurs, 99)
		r.P999ms = percentile(okDurs, 99.9)
		r.Maxms = okDurs[len(okDurs)-1]
	}
	if len(cloneDurs) > 0 {
		sort.Float64s(cloneDurs)
		r.CloneP50ms = percentile(cloneDurs, 50)
		r.CloneP95ms = percentile(cloneDurs, 95)
		r.CloneP99ms = percentile(cloneDurs, 99)
	}
	return r
}

// percentile returns the nearest-rank percentile of an already-sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	rank = max(rank, 0)
	rank = min(rank, len(sorted)-1)
	return sorted[rank]
}
