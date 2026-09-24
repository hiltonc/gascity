package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runproj"
)

// staleRunRoot is a graph.v2 workflow root owned by the fake state's "myrig"
// store, last updated well before the reconcile threshold.
func staleRunRoot(id, status, outcome string, updatedAt time.Time) beads.Bead {
	root := runRootBead(id, "build-from-bead", status)
	root.UpdatedAt = updatedAt
	root.Metadata[beadmeta.RootStoreRefMetadataKey] = "rig:myrig"
	if outcome != "" {
		root.Metadata[beadmeta.OutcomeMetadataKey] = outcome
	}
	return root
}

// reconcileRunServer serves the given projected beads through a warm projection
// source and backs the "myrig" store with storeBeads, at the fixed clock now.
func reconcileRunServer(t *testing.T, now time.Time, projected, storeBeads []beads.Bead) (*Server, *fakeRunProjectionSource) {
	t.Helper()
	s := newRunServer(t)
	s.state.(*fakeState).stores["myrig"] = beads.NewMemStoreFrom(0, storeBeads, nil)
	source := &fakeRunProjectionSource{
		fakeRunCensusSource: fakeRunCensusSource{ok: true},
		projectionOK:        true,
		projection:          runproj.RunProjectionSnapshot{Ready: true, Beads: projected},
	}
	s.runCensusSource = source
	s.runRoots.now = func() time.Time { return now }
	return s, source
}

func listRunStatuses(t *testing.T, s *Server) map[string]RunStatus {
	t.Helper()
	out, err := s.humaHandleRunsList(context.Background(), &RunsListInput{
		CityScope: CityScope{CityName: "test-city"},
	})
	if err != nil {
		t.Fatalf("humaHandleRunsList error: %v", err)
	}
	statuses := make(map[string]RunStatus, len(out.Body.Runs))
	for _, run := range out.Body.Runs {
		statuses[run.RunID] = run.Status
	}
	return statuses
}

// A root whose close never reached the event log (the gcd-thjlcv, gcd-q4s2g4
// and gcd-jj98s shape) must read terminal once the store says so, while a
// root the store still reports in_progress (the hfa-ddfea2 control) stays
// active.
func TestRunsListReconcilesRootCloseTheProjectionMissed(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)
	started := map[string]string{"gc.step_id": "implement"}
	projected := []beads.Bead{
		staleRunRoot("mr-pass", "in_progress", "", old),
		runChildBead("mr-pass.1", "mr-pass", "in_progress", started),
		staleRunRoot("mr-fail", "open", "", old),
		runChildBead("mr-fail.1", "mr-fail", "in_progress", started),
		staleRunRoot("mr-live", "in_progress", "", old),
		runChildBead("mr-live.1", "mr-live", "in_progress", started),
	}
	store := []beads.Bead{
		staleRunRoot("mr-pass", "closed", beadmeta.OutcomePass, old.Add(time.Minute)),
		staleRunRoot("mr-fail", "closed", beadmeta.OutcomeFail, old.Add(time.Minute)),
		staleRunRoot("mr-live", "in_progress", "", old),
	}
	s, _ := reconcileRunServer(t, now, projected, store)

	got := listRunStatuses(t, s)
	want := map[string]RunStatus{
		"mr-pass": RunStatusCompleted,
		"mr-fail": RunStatusFailed,
		"mr-live": RunStatusActive,
	}
	for id, status := range want {
		if got[id] != status {
			t.Errorf("run %s status = %q, want %q (all: %v)", id, got[id], status, got)
		}
	}

	run, err := s.humaHandleRunGet(context.Background(), &RunGetInput{
		CityScope: CityScope{CityName: "test-city"}, RunID: "mr-pass",
	})
	if err != nil {
		t.Fatalf("humaHandleRunGet error: %v", err)
	}
	if run.Body.Status != RunStatusCompleted {
		t.Errorf("GET /runs/mr-pass status = %q, want completed", run.Body.Status)
	}
}

// A root updated inside the threshold is left to the event stream: its close
// is expected to arrive through the tail, and a store read per list request
// for every live run would put the store back on the hot path.
func TestRunsListLeavesRecentlyUpdatedRootsToTheEventStream(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Minute)
	projected := []beads.Bead{
		staleRunRoot("mr-fresh", "in_progress", "", recent),
		runChildBead("mr-fresh.1", "mr-fresh", "in_progress", map[string]string{"gc.step_id": "implement"}),
	}
	store := []beads.Bead{staleRunRoot("mr-fresh", "closed", beadmeta.OutcomePass, recent)}
	s, _ := reconcileRunServer(t, now, projected, store)

	if got := listRunStatuses(t, s)["mr-fresh"]; got != RunStatusActive {
		t.Fatalf("recent run status = %q, want active until the threshold passes", got)
	}
}

// Store truth is memoized once terminal, so a later list does not re-read the
// store, and the reconciled row does not leak into the shared projection
// snapshot the tailer published.
func TestRunRootReconcileMemoizesTerminalRootsAndCopiesTheSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	old := now.Add(-time.Hour)
	projected := []beads.Bead{
		staleRunRoot("mr-pass", "in_progress", "", old),
		runChildBead("mr-pass.1", "mr-pass", "in_progress", map[string]string{"gc.step_id": "implement"}),
	}
	store := []beads.Bead{staleRunRoot("mr-pass", "closed", beadmeta.OutcomePass, old)}
	s, source := reconcileRunServer(t, now, projected, store)

	if got := listRunStatuses(t, s)["mr-pass"]; got != RunStatusCompleted {
		t.Fatalf("first list status = %q, want completed", got)
	}
	if source.projection.Beads[0].Status != "in_progress" {
		t.Fatalf("reconcile mutated the shared snapshot: root status %q", source.projection.Beads[0].Status)
	}

	// Drop the store: a memoized terminal root must not need it again.
	s.state.(*fakeState).stores["myrig"] = beads.NewMemStore()
	if got := listRunStatuses(t, s)["mr-pass"]; got != RunStatusCompleted {
		t.Fatalf("memoized list status = %q, want completed", got)
	}
}

// A projection that later carries a newer row for the root (a reopen, or the
// real close finally arriving) wins over the memo.
func TestRunRootReconcileYieldsToANewerProjectedRow(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	old := now.Add(-time.Hour)
	projected := []beads.Bead{
		staleRunRoot("mr-reopened", "in_progress", "", old),
		runChildBead("mr-reopened.1", "mr-reopened", "in_progress", map[string]string{"gc.step_id": "implement"}),
	}
	store := []beads.Bead{staleRunRoot("mr-reopened", "closed", beadmeta.OutcomePass, old)}
	s, source := reconcileRunServer(t, now, projected, store)

	if got := listRunStatuses(t, s)["mr-reopened"]; got != RunStatusCompleted {
		t.Fatalf("first list status = %q, want completed", got)
	}

	reopened := staleRunRoot("mr-reopened", "in_progress", "", now.Add(-30*time.Second))
	source.projection.Beads = []beads.Bead{reopened, projected[1]}
	if got := listRunStatuses(t, s)["mr-reopened"]; got != RunStatusActive {
		t.Fatalf("status after a newer projected row = %q, want active", got)
	}
}

// A store read failure leaves the projected row standing and says so, rather
// than guessing either way.
func TestRunRootReconcileStoreFailureKeepsProjectedRow(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	old := now.Add(-time.Hour)
	projected := []beads.Bead{
		staleRunRoot("mr-pass", "in_progress", "", old),
		runChildBead("mr-pass.1", "mr-pass", "in_progress", map[string]string{"gc.step_id": "implement"}),
	}
	s, _ := reconcileRunServer(t, now, projected, nil)
	s.state.(*fakeState).stores["myrig"] = failingGetStore{Store: beads.NewMemStore()}

	out, err := s.humaHandleRunsList(context.Background(), &RunsListInput{
		CityScope: CityScope{CityName: "test-city"},
	})
	if err != nil {
		t.Fatalf("humaHandleRunsList error: %v", err)
	}
	if len(out.Body.Runs) != 1 || out.Body.Runs[0].Status != RunStatusActive {
		t.Fatalf("runs = %+v, want the projected active row", out.Body.Runs)
	}
	if !out.Body.Partial {
		t.Fatalf("store failure not reported as partial: %+v", out.Body)
	}
}

type failingGetStore struct{ beads.Store }

func (failingGetStore) Get(string) (beads.Bead, error) {
	return beads.Bead{}, errors.New("bd: database is locked")
}
