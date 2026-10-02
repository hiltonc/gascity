package dispatch

import (
	"errors"
	"fmt"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
)

// processStartGate closes a workflow's start bead (molecule.GateRecipe) once
// every bead it waits on is satisfied, which releases the workflow's entry
// steps and root (bgc-acn). The dispatcher only sees the bead when it reads
// Ready, so the check here guards a blocker added or reopened since.
func processStartGate(store beads.Store, bead beads.Bead, opts ProcessOptions) (ControlResult, error) {
	deps, err := store.DepList(bead.ID, "down")
	if err != nil {
		return ControlResult{}, fmt.Errorf("%s: listing start-gate blockers: %w", bead.ID, err)
	}
	for _, dep := range deps {
		if !beads.IsReadyBlockingDependencyType(dep.Type) {
			continue
		}
		blocker, err := store.Get(dep.DependsOnID)
		if errors.Is(err, beads.ErrNotFound) {
			opts.tracef("process-control bead=%s kind=%s pending reason=blocker_missing blocker=%s", bead.ID, beadmeta.KindStartGate, dep.DependsOnID)
			return ControlResult{}, ErrControlPending
		}
		if err != nil {
			return ControlResult{}, fmt.Errorf("%s: reading start-gate blocker %s: %w", bead.ID, dep.DependsOnID, err)
		}
		if !beads.DependencySatisfied(blocker.Status, blocker.Metadata[beadmeta.WorkOutcomeMetadataKey]) {
			opts.tracef("process-control bead=%s kind=%s pending reason=blocker_open blocker=%s", bead.ID, beadmeta.KindStartGate, blocker.ID)
			return ControlResult{}, ErrControlPending
		}
	}
	if err := updateMetadataAndClose(store, bead.ID, map[string]string{beadmeta.OutcomeMetadataKey: beadmeta.OutcomePass}); err != nil {
		return ControlResult{}, fmt.Errorf("%s: closing start gate: %w", bead.ID, err)
	}
	return ControlResult{Processed: true, Action: "start-gate-open"}, nil
}
