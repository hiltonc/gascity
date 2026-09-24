package dashboardbff

import (
	"strings"
	"sync"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runproj"
)

// The tailer folds the event log, so a workflow root whose close never reached
// the log stays in the projection as open, and runs/summary keeps it in lanes
// (gcd-thjlcv: closed with gc.outcome=pass on 2026-09-19, still in Finalization
// five days later). The supervisor already heals that for GET /runs by
// confirming stale roots against their stores. It hands the same reconcile to
// the plane, so the summary and the census confirm a root against the one memo
// /runs uses instead of keeping a second opinion.

// RunRootReconciler returns projected with the store's terminal row in place of
// every stale workflow root the store reports closed, and whether a store read
// failed. It must not modify projected: a replacement returns a copy.
type RunRootReconciler func(cityName string, projected []beads.Bead) (reconciled []beads.Bead, failed bool)

// SetRunRootReconciler installs the run-root reconcile runs/summary and the run
// census apply to the projection before they answer. Without one they serve the
// projection as folded. The supervisor mux calls it when the plane is installed
// as its run source; it is safe to call while requests are in flight.
func (p *Plane) SetRunRootReconciler(reconcile func(cityName string, projected []beads.Bead) ([]beads.Bead, bool)) {
	p.runTailers.runRoots.Store(RunRootReconciler(reconcile))
}

// runRootReconciler returns the installed reconcile, or nil when none is
// installed or the tailer was built without a manager.
func (m *runTailerManager) runRootReconciler() RunRootReconciler {
	if m == nil {
		return nil
	}
	reconcile, _ := m.runRoots.Load().(RunRootReconciler)
	return reconcile
}

// reconciledRunView is the published summary, census and marks after the run
// root reconcile.
type reconciledRunView struct {
	summary runproj.RunSummary
	census  runproj.CanonicalRunStatusCounts
	marks   map[string]runproj.LaneProgressMark
	ready   bool
}

// reconciledProjectionMemo holds the one summary and census a replacement
// forced, keyed by the fold generation and the roots replaced in it, so a
// steady poll re-projects the city once rather than on every request.
type reconciledProjectionMemo struct {
	mu       sync.Mutex
	built    bool
	lastSeq  uint64
	replaced string
	summary  runproj.RunSummary
	census   runproj.CanonicalRunStatusCounts
	// builds counts rebuilds; tests read it through reconciledMemo.
	builds int
}

// reconciledView reads the published projection and applies the run-root
// reconcile. When no root was replaced it is the published view; when one
// was, the summary and census are rebuilt from the reconciled beads so a root
// the store reports closed is a historical lane, not an active one. A failed
// store read marks the summary partial and leaves the projected row standing.
func (t *cityRunTailer) reconciledView() reconciledRunView {
	t.mu.RLock()
	view := reconciledRunView{summary: t.summary, census: t.census, marks: t.marks, ready: t.ready}
	projected := t.beads
	lastSeq := t.lastSeq
	t.mu.RUnlock()

	reconcile := t.mgr.runRootReconciler()
	if reconcile == nil || !view.ready {
		return view
	}
	reconciled, failed := reconcile(t.name, projected)
	partial := view.summary.LanesPartial || failed
	if replaced := replacedRunRoots(projected, reconciled); replaced != "" {
		view.summary, view.census = t.reconciledProjection.get(lastSeq, replaced, reconciled)
	}
	view.summary.LanesPartial = partial
	return view
}

// get returns the summary and census projected from reconciled, building them
// only when the fold generation or the replaced roots changed.
func (m *reconciledProjectionMemo) get(lastSeq uint64, replaced string, reconciled []beads.Bead) (runproj.RunSummary, runproj.CanonicalRunStatusCounts) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.built && m.lastSeq == lastSeq && m.replaced == replaced {
		return m.summary, m.census
	}
	summary, lanes := runproj.BuildRunSummaryWithAllLanes(reconciled)
	m.summary = summary
	m.census = runproj.CountCanonicalRunStatuses(reconciled, lanes)
	m.built, m.lastSeq, m.replaced = true, lastSeq, replaced
	m.builds++
	return m.summary, m.census
}

// replacedRunRoots names the rows the reconcile replaced, in projection order,
// or "" when it replaced none. The reconcile only swaps a row for the same id's
// terminal store row, so a changed status is exactly a replacement.
func replacedRunRoots(projected, reconciled []beads.Bead) string {
	var b strings.Builder
	for i := range reconciled {
		if i < len(projected) && projected[i].ID == reconciled[i].ID && projected[i].Status == reconciled[i].Status {
			continue
		}
		b.WriteString(reconciled[i].ID)
		b.WriteByte('\n')
	}
	if len(projected) != len(reconciled) {
		b.WriteString("\x00length")
	}
	return b.String()
}
