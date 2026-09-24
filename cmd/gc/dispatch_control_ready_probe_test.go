package main

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
)

// installControlReadyChangeTokenFn swaps the change-probe seam for the duration
// of a test.
func installControlReadyChangeTokenFn(t *testing.T, fn func(dir, cityPath string) (string, error)) {
	t.Helper()
	prev := controlReadyChangeTokenFn
	controlReadyChangeTokenFn = fn
	t.Cleanup(func() { controlReadyChangeTokenFn = prev })
}

// scriptedChangeToken answers the probe from a fixed script and counts calls.
// Once the script is exhausted it keeps answering the last entry.
type scriptedChangeToken struct {
	mu     sync.Mutex
	script []changeTokenAnswer
	calls  int
}

type changeTokenAnswer struct {
	token string
	err   error
}

func (s *scriptedChangeToken) fn(_, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	if i >= len(s.script) {
		i = len(s.script) - 1
	}
	s.calls++
	return s.script[i].token, s.script[i].err
}

func (s *scriptedChangeToken) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// countPrimes installs a single-owned-leg source seam and returns a counter of
// how many times the scope was (re-)primed.
func countPrimes(t *testing.T, dir string) func() int {
	t.Helper()
	var mu sync.Mutex
	primes := 0
	installControlReadyCacheSourcesFn(t, dir, func(_, _ string, _ *config.City) ([]beads.Store, []beads.Store, error) {
		f := newCloseCountingStore(t, true)
		mu.Lock()
		primes++
		mu.Unlock()
		return []beads.Store{f}, []beads.Store{f}, nil
	})
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return primes
	}
}

// ageControlReadyCache rewinds a scope's primed timestamp by d.
func ageControlReadyCache(t *testing.T, dir string, d time.Duration) {
	t.Helper()
	controlReadyCacheRegistry.mu.Lock()
	defer controlReadyCacheRegistry.mu.Unlock()
	entry, ok := controlReadyCacheRegistry.byDir[dir]
	if !ok {
		t.Fatalf("ageControlReadyCache: no cache entry for %q", dir)
	}
	entry.primedAt = entry.primedAt.Add(-d)
}

// TestControlReadyCachesForReusesStaleSnapshotWhenChangeTokenUnchanged is the
// gsc-dtay pin: an idle --follow sweep past the TTL must answer from the primed
// snapshot when the scope's ledger has not changed, instead of re-listing the
// whole rig.
func TestControlReadyCachesForReusesStaleSnapshotWhenChangeTokenUnchanged(t *testing.T) {
	dir := t.TempDir()
	primes := countPrimes(t, dir)
	probe := &scriptedChangeToken{script: []changeTokenAnswer{{token: "h1"}}}
	installControlReadyChangeTokenFn(t, probe.fn)

	first := controlReadyCachesFor(dir, dir, nil)
	if len(first) == 0 {
		t.Fatal("first call: expected a primed snapshot")
	}
	forceControlReadyCacheStale(t, dir)

	second := controlReadyCachesFor(dir, dir, nil)
	if got := primes(); got != 1 {
		t.Fatalf("primes = %d, want 1: an unchanged ledger must not be re-listed", got)
	}
	if len(second) != len(first) || second[0] != first[0] {
		t.Fatal("second call must return the snapshot primed by the first")
	}
	if ready, ok := cachedControlReadyUnion(second); !ok || len(ready) != 1 {
		t.Fatalf("reused snapshot must still answer: ok=%t ready=%d", ok, len(ready))
	}
}

func TestControlReadyCachesForRePrimesWhenChangeTokenMoves(t *testing.T) {
	dir := t.TempDir()
	primes := countPrimes(t, dir)
	probe := &scriptedChangeToken{script: []changeTokenAnswer{{token: "h1"}, {token: "h2"}}}
	installControlReadyChangeTokenFn(t, probe.fn)

	controlReadyCachesFor(dir, dir, nil)
	forceControlReadyCacheStale(t, dir)
	controlReadyCachesFor(dir, dir, nil)

	if got := primes(); got != 2 {
		t.Fatalf("primes = %d, want 2: a moved change token must re-prime", got)
	}
}

// TestControlReadyCachesForRePrimesWhenProbeFails keeps the pre-probe behavior
// as the fallback: an unanswerable probe is never read as "unchanged".
func TestControlReadyCachesForRePrimesWhenProbeFails(t *testing.T) {
	dir := t.TempDir()
	primes := countPrimes(t, dir)
	probe := &scriptedChangeToken{script: []changeTokenAnswer{{token: "h1"}, {err: errors.New("dolt unreachable")}}}
	installControlReadyChangeTokenFn(t, probe.fn)

	controlReadyCachesFor(dir, dir, nil)
	forceControlReadyCacheStale(t, dir)
	controlReadyCachesFor(dir, dir, nil)

	if got := primes(); got != 2 {
		t.Fatalf("primes = %d, want 2: a failed probe must fall back to re-priming", got)
	}
}

// TestControlReadyCachesForRePrimesWithoutPrimeTimeToken covers a scope whose
// probe was unavailable when the snapshot was primed: there is nothing to
// compare against, so the TTL alone governs reuse, as before.
func TestControlReadyCachesForRePrimesWithoutPrimeTimeToken(t *testing.T) {
	dir := t.TempDir()
	primes := countPrimes(t, dir)
	probe := &scriptedChangeToken{script: []changeTokenAnswer{{err: errors.New("no dolt endpoint")}, {token: "h1"}}}
	installControlReadyChangeTokenFn(t, probe.fn)

	controlReadyCachesFor(dir, dir, nil)
	forceControlReadyCacheStale(t, dir)
	controlReadyCachesFor(dir, dir, nil)

	if got := primes(); got != 2 {
		t.Fatalf("primes = %d, want 2: no prime-time token means no reuse past the TTL", got)
	}
}

func TestControlReadyCachesForRePrimesPastMaxAgeEvenWhenTokenUnchanged(t *testing.T) {
	dir := t.TempDir()
	primes := countPrimes(t, dir)
	probe := &scriptedChangeToken{script: []changeTokenAnswer{{token: "h1"}}}
	installControlReadyChangeTokenFn(t, probe.fn)

	controlReadyCachesFor(dir, dir, nil)
	ageControlReadyCache(t, dir, controlReadyCacheMaxAge)
	controlReadyCachesFor(dir, dir, nil)

	if got := primes(); got != 2 {
		t.Fatalf("primes = %d, want 2: the max age bounds reuse regardless of the probe", got)
	}
}

// TestControlReadyCachesForNeverProbesMultiLegSnapshot pins the probe's scope:
// it hashes only the scope's own database, so a snapshot that also read a
// shared graph binding cannot be vouched for by it.
func TestControlReadyCachesForNeverProbesMultiLegSnapshot(t *testing.T) {
	dir := t.TempDir()
	binding := newCloseCountingStore(t, false)
	primes := 0
	installControlReadyCacheSourcesFn(t, dir, func(_, _ string, _ *config.City) ([]beads.Store, []beads.Store, error) {
		primes++
		scoped := newCloseCountingStore(t, true)
		return []beads.Store{scoped, binding}, []beads.Store{scoped}, nil
	})
	probe := &scriptedChangeToken{script: []changeTokenAnswer{{token: "h1"}}}
	installControlReadyChangeTokenFn(t, probe.fn)

	controlReadyCachesFor(dir, dir, nil)
	forceControlReadyCacheStale(t, dir)
	controlReadyCachesFor(dir, dir, nil)

	if primes != 2 {
		t.Fatalf("primes = %d, want 2: a multi-leg snapshot must keep TTL-only reuse", primes)
	}
	if got := probe.callCount(); got != 0 {
		t.Fatalf("probe calls = %d, want 0 for a multi-leg snapshot", got)
	}
}

// orderRecordingStore logs each List so a test can see what ran before it.
type orderRecordingStore struct {
	*closeCountingStore
	log *[]string
}

func (s *orderRecordingStore) List(q beads.ListQuery) ([]beads.Bead, error) {
	*s.log = append(*s.log, "list")
	return s.closeCountingStore.List(q)
}

// TestControlReadyCachesForCapturesTokenBeforePriming pins the race rule: the
// token must describe a ledger state no newer than the snapshot. Read after
// the prime, a write landing mid-prime would be folded into the token but
// missing from the snapshot, and every later sweep would reuse the short read.
func TestControlReadyCachesForCapturesTokenBeforePriming(t *testing.T) {
	dir := t.TempDir()
	var log []string
	installControlReadyCacheSourcesFn(t, dir, func(_, _ string, _ *config.City) ([]beads.Store, []beads.Store, error) {
		s := &orderRecordingStore{closeCountingStore: newCloseCountingStore(t, true), log: &log}
		return []beads.Store{s}, []beads.Store{s}, nil
	})
	installControlReadyChangeTokenFn(t, func(_, _ string) (string, error) {
		log = append(log, "probe")
		return "h1", nil
	})

	controlReadyCachesFor(dir, dir, nil)

	if len(log) < 2 || log[0] != "probe" || log[1] != "list" {
		t.Fatalf("call order = %v, want the probe before the first list", log)
	}
}

func TestControlReadyChangeTokenRefusesNonBdProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GC_BEADS", "file")

	if _, err := controlReadyChangeToken(dir, dir); err == nil {
		t.Fatal("expected an error for a file-backed scope, which has no Dolt database to hash")
	}
}

func TestControlReadyChangeTokenFailsWithoutConnectionContract(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GC_BEADS", "bd")

	if _, err := controlReadyChangeToken(dir, dir); err == nil {
		t.Fatal("expected an error for a scope with no resolvable Dolt endpoint")
	}
}
