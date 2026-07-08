package main

import (
	"flag"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
)

func TestExpandRepos(t *testing.T) {
	tests := []struct {
		name     string
		reposCSV string
		pattern  string
		count    int
		want     []string
		wantErr  bool
	}{
		{name: "repos passthrough", reposCSV: "a/b,c/d", want: []string{"a/b", "c/d"}},
		{name: "pattern expands 1..N", pattern: "org/bench-{n}", count: 3,
			want: []string{"org/bench-1", "org/bench-2", "org/bench-3"}},
		{name: "pattern repeats {n}", pattern: "grp-{n}/bench-{n}", count: 2,
			want: []string{"grp-1/bench-1", "grp-2/bench-2"}},
		{name: "pattern keeps verbatim suffix", pattern: "you/bench-{n}.git", count: 2,
			want: []string{"you/bench-1.git", "you/bench-2.git"}},
		{name: "missing {n}", pattern: "bench-", count: 3, wantErr: true},
		{name: "pattern without count", pattern: "bench-{n}", count: 0, wantErr: true},
		{name: "count without pattern", count: 3, wantErr: true},
		{name: "repos plus count", reposCSV: "a/b", count: 3, wantErr: true},
		{name: "both repos and pattern", reposCSV: "a/b", pattern: "c-{n}", count: 2, wantErr: true},
		{name: "neither", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandRepos(tt.reposCSV, tt.pattern, tt.count)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expandRepos(%q,%q,%d) err = %v, wantErr %v", tt.reposCSV, tt.pattern, tt.count, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expandRepos(%q,%q,%d) = %v, want %v", tt.reposCSV, tt.pattern, tt.count, got, tt.want)
			}
		})
	}
}

func TestParseFlagsCloneStrategyIgnoresCommitShape(t *testing.T) {
	cfg, err := parseFlagsForTest(t,
		"-strategy", "clone",
		"-repos", "org/repo",
		"-clone-depth", "3",
		"-base-ref", "main",
		"-files-min", "10",
		"-files-max", "1",
		"-file-size", "0",
	)
	if err != nil {
		t.Fatalf("parseFlags clone = %v, want commit-shape flags ignored", err)
	}
	if cfg.strategy != "clone" {
		t.Fatalf("strategy = %q, want clone", cfg.strategy)
	}
	if cfg.cloneDepth != 3 || cfg.baseRef != "main" {
		t.Fatalf("clone knobs = depth %d base %q, want 3/main", cfg.cloneDepth, cfg.baseRef)
	}
	if !reflect.DeepEqual(cfg.repos, []string{"org/repo"}) {
		t.Fatalf("repos = %v, want [org/repo]", cfg.repos)
	}
}

func TestParseFlagsCloneStrategyNeedsRepos(t *testing.T) {
	_, err := parseFlagsForTest(t, "-strategy", "clone")
	if err == nil || !strings.Contains(err.Error(), "no repos") {
		t.Fatalf("parseFlags clone without repos err = %v, want no repos error", err)
	}
}

func TestParseFlagsCommitShapeStillValidatedForPushStrategies(t *testing.T) {
	_, err := parseFlagsForTest(t, "-strategy", "branch", "-repos", "org/repo", "-files-min", "0")
	if err == nil || !strings.Contains(err.Error(), "-files-min") {
		t.Fatalf("parseFlags branch with -files-min 0 err = %v, want -files-min validation", err)
	}
}

func parseFlagsForTest(t *testing.T, args ...string) (*runConfig, error) {
	t.Helper()
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	fs := flag.NewFlagSet("forgemark", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flag.CommandLine = fs
	os.Args = append([]string{"forgemark"}, args...)
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	})
	return parseFlags()
}

func TestDestRef(t *testing.T) {
	// The prefix is prepended verbatim before the run ID; the assembled ref is what
	// parseFlags validates. Readable prefixes pass; typo shapes git rejects fail fast
	// (before any push pollutes the measured window).
	tests := []struct {
		name      string
		prefix    string
		c, i      int
		want      string
		wantValid bool
	}{
		{name: "no prefix", prefix: "", c: 32, i: 5, want: "refs/heads/fmX-c32-a5", wantValid: true},
		{name: "slash namespace", prefix: "bench/", c: 32, i: 5, want: "refs/heads/bench/fmX-c32-a5", wantValid: true},
		{name: "dash separator", prefix: "bench-", c: 8, i: 0, want: "refs/heads/bench-fmX-c8-a0", wantValid: true},
		{name: "no separator", prefix: "bench", c: 1, i: 0, want: "refs/heads/benchfmX-c1-a0", wantValid: true}, // ugly-but-valid
		{name: "space", prefix: "my bench/", c: 1, i: 0, want: "refs/heads/my bench/fmX-c1-a0", wantValid: false},
		{name: "tilde", prefix: "bench~1", c: 1, i: 0, want: "refs/heads/bench~1fmX-c1-a0", wantValid: false},
		{name: "leading slash", prefix: "/bench", c: 1, i: 0, want: "refs/heads//benchfmX-c1-a0", wantValid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := destRef(tt.prefix, "fmX", tt.c, tt.i)
			if got != tt.want {
				t.Errorf("destRef(%q, ...) = %q, want %q", tt.prefix, got, tt.want)
			}
			if valid := plumbing.ReferenceName(got).Validate() == nil; valid != tt.wantValid {
				t.Errorf("Validate(%q) valid = %v, want %v", got, valid, tt.wantValid)
			}
		})
	}
}
