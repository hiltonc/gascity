package beads

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// readinessBdRunner fakes the bd a readiness prime drives and records every
// command it was asked to run.
type readinessBdRunner struct {
	t       *testing.T
	version string

	mu    sync.Mutex
	calls [][]string
}

func (r *readinessBdRunner) run(_, name string, args ...string) ([]byte, error) {
	if name != "bd" {
		r.t.Fatalf("command name = %q, want bd", name)
	}
	r.mu.Lock()
	r.calls = append(r.calls, append([]string(nil), args...))
	r.mu.Unlock()
	switch args[0] {
	case "version":
		return []byte("bd version " + r.version + " (test)\n"), nil
	case "sql":
		return []byte(`[
			{"id":"bd-ready","is_blocked":0},
			{"id":"bd-blocked","is_blocked":1},
			{"id":"bd-working","is_blocked":0}
		]`), nil
	case "list":
		return []byte(`[
			{"id":"bd-ready","title":"ready","status":"open","issue_type":"task","created_at":"2026-01-01T00:00:00Z","labels":["task"],"metadata":{"gc.routed_to":"x"}},
			{"id":"bd-blocked","title":"blocked","status":"open","issue_type":"task","created_at":"2026-01-01T00:00:01Z","metadata":{}},
			{"id":"bd-working","title":"working","status":"in_progress","issue_type":"task","created_at":"2026-01-01T00:00:02Z","metadata":{}}
		]`), nil
	case "show":
		return []byte(`[{"id":"bd-ready","title":"ready","description":"full text","status":"open","issue_type":"task","created_at":"2026-01-01T00:00:00Z"}]`), nil
	}
	return []byte(`[]`), nil
}

func (r *readinessBdRunner) verbs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c[0])
	}
	return out
}

func (r *readinessBdRunner) callsOf(verb string) [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out [][]string
	for _, c := range r.calls {
		if c[0] == verb {
			out = append(out, c)
		}
	}
	return out
}

func TestCachingStorePrimeActiveReadinessListsOnceBriefAndIssueTierOnly(t *testing.T) {
	t.Parallel()
	runner := &readinessBdRunner{t: t, version: "1.3.0"}
	cache := NewCachingStoreForTest(NewBdStore("/city", runner.run), nil)

	if err := cache.PrimeActiveReadiness(); err != nil {
		t.Fatalf("PrimeActiveReadiness: %v", err)
	}

	lists := runner.callsOf("list")
	if len(lists) != 1 {
		t.Fatalf("bd list calls = %d (%v), want exactly 1", len(lists), lists)
	}
	line := strings.Join(lists[0], " ")
	if !strings.Contains(line, "--brief") {
		t.Errorf("bd list args = %q, want --brief", line)
	}
	if strings.Contains(line, "--status") {
		t.Errorf("bd list args = %q, want no --status: one list must cover every non-closed status", line)
	}
	if strings.Contains(line, "--all") {
		t.Errorf("bd list args = %q, must not include closed beads", line)
	}
	if q := runner.callsOf("query"); len(q) != 0 {
		t.Errorf("bd query calls = %v, want none: the ephemeral tier is not a ready candidate", q)
	}
	if got := cache.Stats().State; got != "ready-only" {
		t.Errorf("Stats().State = %q, want ready-only", got)
	}

	ready, ok := cache.CachedReady()
	if !ok {
		t.Fatal("CachedReady declined a readiness-primed snapshot")
	}
	if len(ready) != 1 || ready[0].ID != "bd-ready" {
		t.Fatalf("CachedReady = %v, want only bd-ready", ready)
	}
	if ready[0].Metadata["gc.routed_to"] != "x" || len(ready[0].Labels) != 1 {
		t.Errorf("CachedReady row = %+v, want metadata and labels kept by the brief row", ready[0])
	}
}

func TestCachingStorePrimeActiveReadinessServesOnlyCachedReady(t *testing.T) {
	t.Parallel()
	runner := &readinessBdRunner{t: t, version: "1.3.0"}
	cache := NewCachingStoreForTest(NewBdStore("/city", runner.run), nil)
	if err := cache.PrimeActiveReadiness(); err != nil {
		t.Fatalf("PrimeActiveReadiness: %v", err)
	}

	got, err := cache.Get("bd-ready")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description != "full text" {
		t.Fatalf("Get description = %q, want the backing's full row, not the brief snapshot", got.Description)
	}
	if shows := runner.callsOf("show"); len(shows) != 1 {
		t.Fatalf("bd show calls = %d, want 1: Get must go to the backing", len(shows))
	}
}

func TestCachingStorePrimeActiveReadinessRefusesAPrimedStore(t *testing.T) {
	t.Parallel()
	runner := &readinessBdRunner{t: t, version: "1.3.0"}
	cache := NewCachingStoreForTest(NewBdStore("/city", runner.run), nil)
	if err := cache.PrimeActive(); err != nil {
		t.Fatalf("PrimeActive: %v", err)
	}
	if err := cache.PrimeActiveReadiness(); err == nil {
		t.Fatal("PrimeActiveReadiness over a PrimeActive snapshot succeeded; it must refuse to mix brief rows into full ones")
	}
}

func TestCachingStorePrimeActiveReadinessPropagatesListFailure(t *testing.T) {
	t.Parallel()
	runner := func(_, _ string, args ...string) ([]byte, error) {
		if args[0] == "version" {
			return []byte("bd version 1.3.0\n"), nil
		}
		return nil, errors.New("dolt unavailable")
	}
	cache := NewCachingStoreForTest(NewBdStore("/city", runner), nil)
	if err := cache.PrimeActiveReadiness(); err == nil {
		t.Fatal("PrimeActiveReadiness succeeded over a failing list")
	}
	if _, ok := cache.CachedReady(); ok {
		t.Fatal("CachedReady answered after a failed readiness prime")
	}
}

func TestBdStoreListBriefGatedOnBdVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version string
		brief   bool
		want    bool
	}{
		{version: "1.3.0", brief: true, want: true},
		{version: "1.4.1", brief: true, want: true},
		{version: "1.2.9", brief: true, want: false},
		{version: "1.3.0", brief: false, want: false},
	} {
		runner := &readinessBdRunner{t: t, version: tc.version}
		store := NewBdStore("/city", runner.run)
		if _, err := store.List(ListQuery{AllowScan: true, Brief: tc.brief}); err != nil {
			t.Fatalf("List: %v", err)
		}
		lists := runner.callsOf("list")
		if len(lists) != 1 {
			t.Fatalf("bd list calls = %d, want 1", len(lists))
		}
		got := strings.Contains(strings.Join(lists[0], " "), "--brief")
		if got != tc.want {
			t.Errorf("bd %s, Brief=%v: --brief passed = %v, want %v", tc.version, tc.brief, got, tc.want)
		}
		if !tc.brief {
			if v := runner.callsOf("version"); len(v) != 0 {
				t.Errorf("Brief=false spawned bd version %d times, want 0", len(v))
			}
		}
	}
}

// installBdBinaryIdentity swaps the memo's key seam. Tests that call it must
// not run in parallel.
func installBdBinaryIdentity(t *testing.T, fn func() (bdBinaryIdentity, bool)) {
	t.Helper()
	prev := bdBinaryIdentityFn
	bdBinaryIdentityFn = fn
	t.Cleanup(func() { bdBinaryIdentityFn = prev })
}

func TestBdVersionMemoSharesOneProbeAcrossStores(t *testing.T) {
	identity := bdBinaryIdentity{path: "/bin/bd", size: 1, modTime: time.Unix(1, 0)}
	installBdBinaryIdentity(t, func() (bdBinaryIdentity, bool) { return identity, true })
	runner := &readinessBdRunner{t: t, version: "1.3.0"}
	memo := NewBdVersionMemo()

	for i := 0; i < 3; i++ {
		cache := NewCachingStoreForTest(NewBdStore("/city", runner.run, WithBdStoreVersionMemo(memo)), nil)
		if err := cache.PrimeActiveReadiness(); err != nil {
			t.Fatalf("PrimeActiveReadiness %d: %v", i, err)
		}
	}
	if v := runner.callsOf("version"); len(v) != 1 {
		t.Fatalf("bd version spawned %d times across 3 stores sharing a memo, want 1 (verbs %v)", len(v), runner.verbs())
	}

	identity.modTime = time.Unix(2, 0)
	cache := NewCachingStoreForTest(NewBdStore("/city", runner.run, WithBdStoreVersionMemo(memo)), nil)
	if err := cache.PrimeActiveReadiness(); err != nil {
		t.Fatalf("PrimeActiveReadiness after upgrade: %v", err)
	}
	if v := runner.callsOf("version"); len(v) != 2 {
		t.Fatalf("bd version spawned %d times after bd was replaced, want 2: a new binary must be re-probed", len(v))
	}
}

func TestBdVersionMemoDoesNotMemoizeWithoutBinaryIdentity(t *testing.T) {
	installBdBinaryIdentity(t, func() (bdBinaryIdentity, bool) { return bdBinaryIdentity{}, false })
	runner := &readinessBdRunner{t: t, version: "1.3.0"}
	memo := NewBdVersionMemo()

	for i := 0; i < 2; i++ {
		cache := NewCachingStoreForTest(NewBdStore("/city", runner.run, WithBdStoreVersionMemo(memo)), nil)
		if err := cache.PrimeActiveReadiness(); err != nil {
			t.Fatalf("PrimeActiveReadiness %d: %v", i, err)
		}
	}
	if v := runner.callsOf("version"); len(v) != 2 {
		t.Fatalf("bd version spawned %d times with no binary identity, want one per store (2)", len(v))
	}
}

func TestBdVersionMemoDoesNotMemoizeAFailedProbe(t *testing.T) {
	identity := bdBinaryIdentity{path: "/bin/bd", size: 1, modTime: time.Unix(1, 0)}
	installBdBinaryIdentity(t, func() (bdBinaryIdentity, bool) { return identity, true })
	var versionCalls int
	runner := func(_, _ string, args ...string) ([]byte, error) {
		if args[0] == "version" {
			versionCalls++
			if versionCalls == 1 {
				return nil, errors.New("bd hung")
			}
			return []byte("bd version 1.3.0\n"), nil
		}
		return []byte(`[]`), nil
	}
	memo := NewBdVersionMemo()
	store := NewBdStore("/city", runner, WithBdStoreVersionMemo(memo))
	if _, err := store.bdCLIVersion(); err == nil {
		t.Fatal("bdCLIVersion succeeded over a failing probe")
	}
	version, err := NewBdStore("/city", runner, WithBdStoreVersionMemo(memo)).bdCLIVersion()
	if err != nil || version != "1.3.0" {
		t.Fatalf("bdCLIVersion after a failed probe = %q, %v; want a fresh probe answering 1.3.0", version, err)
	}
}
