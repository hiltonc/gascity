package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

// TestBeadListAllTrueWithLimitReturnsWithoutFullScan proves the unfiltered
// all=true first-page path pushes a store-level limit so the store returns
// O(limit) rows, not O(history). The limit is asserted via countingListStore's
// maxListLim — the same mechanism handler_beads_bounded_test.go uses.
func TestBeadListAllTrueWithLimitReturnsWithoutFullScan(t *testing.T) {
	const limit = 3
	state := newFakeState(t)
	_, mem := seedMoleculeStore("gc", 30)
	store := &countingListStore{Store: mem}
	state.stores["myrig"] = store

	h := newTestCityHandler(t, state)
	rec := getBeads(t, h, cityURL(state, "/beads?all=true&limit=3"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	items, _, next := decodeListBody(t, rec)
	if len(items) != limit {
		t.Fatalf("got %d items, want %d", len(items), limit)
	}
	if next == "" {
		t.Fatal("expected next_cursor for continuation, got empty")
	}
	if store.maxListLim != limit+1 {
		t.Errorf("max List limit = %d, want %d (page bound pushed into store)", store.maxListLim, limit+1)
	}
}

// slowListStore wraps a beads.Store whose List sleeps for a controlled
// duration, modeling a store under contention.
type slowListStore struct {
	beads.Store
	delay time.Duration
	mu    sync.Mutex
	calls int
}

func (s *slowListStore) List(q beads.ListQuery) ([]beads.Bead, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	time.Sleep(s.delay)
	return s.Store.List(q)
}

// TestBeadListAllTrueRespectsTimeout proves the all=true deadline fires at
// a leg boundary: two work legs, the first one blocks past the deadline, so
// the second leg's ctx.Err() check returns 503. This is stronger than the
// previous version (timeout=0), which proved only that a pre-loop check
// compiled.
//
// N.B. The deadline fires BETWEEN legs, not during a List call — Store.List
// takes no context (F2). A single-leg city whose List blocks past the deadline
// returns 200 because no second check runs.
func TestBeadListAllTrueRespectsTimeout(t *testing.T) {
	orig := beadListAllReadTimeout
	beadListAllReadTimeout = 10 * time.Millisecond
	t.Cleanup(func() { beadListAllReadTimeout = orig })

	fs := newFakeState(t)
	slow := &slowListStore{Store: beads.NewMemStore(), delay: 200 * time.Millisecond}
	fast := beads.NewMemStore()
	fs.stores = map[string]beads.Store{
		"arig": slow,
		"brig": fast,
	}
	fs.cityBeadStore = fast

	h := newTestCityHandler(t, fs)
	rec := getBeads(t, h, cityURL(fs, "/beads?all=true&limit=3"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "timed out") {
		t.Errorf("503 body should mention timeout: %s", rec.Body.String())
	}
}

// TestBeadListClientDisconnectReportsCancel proves that a zero-deadline read
// (which fires context.DeadlineExceeded before any leg) is reported as a
// timeout, not as a client disconnect. The distinction matters for F7: a
// real context.Canceled from a client disconnect carries a different message.
func TestBeadListClientDisconnectReportsCancel(t *testing.T) {
	orig := beadListAllReadTimeout
	beadListAllReadTimeout = 0
	t.Cleanup(func() { beadListAllReadTimeout = orig })

	state := newFakeState(t)
	h := newTestCityHandler(t, state)

	rec := getBeads(t, h, cityURL(state, "/beads?all=true&limit=3"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "timed out") {
		t.Errorf("zero deadline should surface as timeout, got: %s", body)
	}
}

// TestBeadListSplitCityUnfilteredPreservesHydrateLegTotal proves that an
// UNFILTERED all=true read in a split city does not truncate the hydrate leg's
// rows. F1 from the review: the else-if that pushes a store-level limit on
// non-bounded scans must not fire in boundedMode, because the hydrate leg's
// row count IS its total.
func TestBeadListSplitCityUnfilteredPreservesHydrateLegTotal(t *testing.T) {
	const limit = 5
	graph := &nonCounterLeg{Store: seedMoleculeLeg("gcg", 12)}
	fs, city, rig := newBoundedSplitState(t, graph)

	body := fetchBoundedBeads(t, fs, fmt.Sprintf("?all=true&limit=%d", limit))

	if city.maxListLim != limit+1 {
		t.Errorf("city leg max List limit = %d, want %d", city.maxListLim, limit+1)
	}
	if rig.maxListLim != limit+1 {
		t.Errorf("rig leg max List limit = %d, want %d", rig.maxListLim, limit+1)
	}
	if graph.maxListLim != 0 {
		t.Errorf("graph leg List limit = %d, want 0 — hydrate leg must not be capped", graph.maxListLim)
	}
	if want := 30 + 30 + 12; body.Total != want {
		t.Errorf("Total = %d, want %d (work counts from Count, graph count from hydrate rows)", body.Total, want)
	}
}
