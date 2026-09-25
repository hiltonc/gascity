package dashboardbff

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// closeRootsReconciler stands in for the supervisor's run-root reconcile: it
// returns projected with every root in closed replaced by a closed row, copying
// the slice before the first replacement exactly as the real one does.
func closeRootsReconciler(calls *atomic.Int64, failed bool, closed ...string) func(string, []beads.Bead) ([]beads.Bead, bool) {
	want := make(map[string]bool, len(closed))
	for _, id := range closed {
		want[id] = true
	}
	return func(_ string, projected []beads.Bead) ([]beads.Bead, bool) {
		calls.Add(1)
		out := projected
		copied := false
		for i, b := range projected {
			if !want[b.ID] || b.Status == "closed" {
				continue
			}
			if !copied {
				out = append([]beads.Bead(nil), projected...)
				copied = true
			}
			b.Status = "closed"
			b.Metadata = beads.StringMap{"gc.outcome": "pass"}
			out[i] = b
		}
		return out, failed
	}
}

func reconcilePlane(t *testing.T, reconcile func(string, []beads.Bead) ([]beads.Bead, bool)) *Plane {
	t.Helper()
	dir := t.TempDir()
	writeEventLog(t, filepath.Join(dir, ".gc", "events.jsonl"),
		runMoleculeEvent(1, "run-closed", "test-formula", ""),
		runMoleculeEvent(2, "run-live", "test-formula", ""),
	)
	p := New(Deps{Resolver: fakeResolver{paths: map[string]string{"alpha": dir}}})
	if reconcile != nil {
		p.SetRunRootReconciler(reconcile)
	}
	p.Start(t.Context())
	t.Cleanup(p.Stop)
	return p
}

// A root whose close the event log missed (gcd-thjlcv) must leave runs/summary
// once the supervisor's reconcile reports it closed: not in lanes, not in
// totalActive, not in the census, while a live root stays in all three.
func TestRunSummaryDropsARootTheReconcileReportsClosed(t *testing.T) {
	var calls atomic.Int64
	p := reconcilePlane(t, closeRootsReconciler(&calls, false, "run-closed"))

	resp := getRunSummary(t, p, "alpha")
	if ids := wireLaneIDs(resp); len(ids) != 1 || ids[0] != "run-live" {
		t.Fatalf("lanes = %v, want only run-live", ids)
	}
	if resp.TotalActive != len(resp.Lanes) {
		t.Fatalf("totalActive = %d, lanes = %d; want them to agree", resp.TotalActive, len(resp.Lanes))
	}
	if resp.Census.Data.TotalInFlight != 1 {
		t.Fatalf("census in-flight = %d, want 1", resp.Census.Data.TotalInFlight)
	}
	if resp.LanesPartial {
		t.Fatal("a complete reconcile marked the summary partial")
	}

	census, ok := p.RunCensus(context.Background(), "alpha")
	if !ok {
		t.Fatal("RunCensus reported a registered city as unknown")
	}
	counts := census.StatusCounts
	if inFlight := counts.Pending + counts.Active + counts.Waiting; inFlight != 1 || counts.Completed != 1 {
		t.Fatalf("status_counts = %+v, want one in-flight run and one completed", counts)
	}
	if calls.Load() == 0 {
		t.Fatal("runs/summary never consulted the run-root reconcile")
	}
}

// Without a reconcile (a plane no supervisor has wired) the summary is the
// projection as folded: both roots are active.
func TestRunSummaryWithoutReconcileServesTheProjection(t *testing.T) {
	p := reconcilePlane(t, nil)

	resp := getRunSummary(t, p, "alpha")
	if len(resp.Lanes) != 2 || resp.TotalActive != 2 {
		t.Fatalf("lanes = %v totalActive = %d, want both roots active", wireLaneIDs(resp), resp.TotalActive)
	}
}

// A reconcile that could not confirm some root leaves the projected rows
// standing and says so, rather than presenting an unconfirmed answer as whole.
func TestRunSummaryReportsAFailedReconcileAsPartial(t *testing.T) {
	var calls atomic.Int64
	p := reconcilePlane(t, closeRootsReconciler(&calls, true))

	resp := getRunSummary(t, p, "alpha")
	if len(resp.Lanes) != 2 {
		t.Fatalf("lanes = %v, want both projected roots", wireLaneIDs(resp))
	}
	if !resp.LanesPartial {
		t.Fatal("a failed reconcile was not reported as partial")
	}
	census, _ := p.RunCensus(context.Background(), "alpha")
	if !census.Partial {
		t.Fatalf("census = %+v, want partial after a failed reconcile", census)
	}
}

// The rebuild a replacement forces is memoized per fold generation and
// replaced set, so a steady poll does not re-project the whole city.
func TestRunSummaryReconcileRebuildIsMemoized(t *testing.T) {
	var calls atomic.Int64
	p := reconcilePlane(t, closeRootsReconciler(&calls, false, "run-closed"))
	tl, ok := p.cityRunTailer("alpha")
	if !ok {
		t.Fatal("tailer missing for a registered city")
	}

	getRunSummary(t, p, "alpha")
	first := tl.reconciledMemo()
	getRunSummary(t, p, "alpha")
	if second := tl.reconciledMemo(); second != first {
		t.Fatalf("reconciled rebuild ran again at an unchanged generation: %d -> %d builds", first, second)
	}
	if first != 1 {
		t.Fatalf("reconciled builds = %d, want 1", first)
	}
}

// reconciledMemo reports how many reconciled rebuilds the tailer has run.
func (t *cityRunTailer) reconciledMemo() int {
	t.reconciledProjection.mu.Lock()
	defer t.reconciledProjection.mu.Unlock()
	return t.reconciledProjection.builds
}

func wireLaneIDs(resp runSummaryWire) []string {
	ids := make([]string, 0, len(resp.Lanes))
	for _, lane := range resp.Lanes {
		ids = append(ids, lane.ID)
	}
	return ids
}
