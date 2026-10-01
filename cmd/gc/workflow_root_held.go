package main

// A workflow root is a launch fallback, not a unit of work.
//
// A pool claims a graph workflow root so that the claimant can walk the root's
// steps through the assigned-anchor tier of its work query. Once a step is in
// progress under a session, the workflow is running: the root has nothing left
// to launch, and claiming it gives a second worker a root whose only live step
// belongs to somebody else.
//
// It happened three ways on 2026-10-01 (gsc-tf857). A dead session's root and
// step were both released, the replacement claimed only the step, and the
// still-open root was offered to another worker eleven minutes later. A root
// whose implement steps had closed was claimed while its next step ran under a
// run-operator. And each such root was also counted as capacity demand, so it
// spawned a worker whose only possible claim was that root.
//
// So both readers of pool demand ask the same question here: the controller
// before it counts a root as demand, and gc hook --claim before it claims one.

import (
	"strings"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/sourceworkflow"
)

// workflowRootStepLister lists the in-progress steps of the workflow root
// rootID, i.e. the in-progress beads whose gc.root_bead_id names it.
type workflowRootStepLister func(rootID string) ([]beads.Bead, error)

// workflowRootStepsQuery is the store query a workflowRootStepLister runs.
func workflowRootStepsQuery(rootID string) beads.ListQuery {
	return beads.ListQuery{
		Status:   "in_progress",
		Metadata: map[string]string{beadmeta.RootBeadIDMetadataKey: rootID},
		TierMode: beads.TierBoth,
	}
}

// storeWorkflowRootStepLister lists root steps from one store.
func storeWorkflowRootStepLister(store beads.Store) workflowRootStepLister {
	return func(rootID string) ([]beads.Bead, error) {
		return store.List(workflowRootStepsQuery(rootID))
	}
}

// workflowRootHeldStep returns a step of root that a session holds in
// progress, or ok=false when root is not a workflow root or none of its steps
// is held.
//
// "Held" is in_progress with an assignee. Whether that assignee is ALIVE is the
// reconciler's question, not this one: a dead session's work is released by
// releaseWorkFromClosedSessionBead and releaseOrphanedPoolAssignments, and the
// step it held then reads open here, so the root becomes claimable again on the
// tick its step does.
//
// A control bead never holds the root, even while the control dispatcher is
// working it: control beads stay open throughout, so open cannot tell a control
// being processed from one waiting on its deps, and every graph root carries an
// open workflow-finalize from launch on. Counting them would make every running
// root unlaunchable. The cost is the window in which a released root's only
// live work is a control: the root is offered, and the session that claims it
// holds it for the steps that control emits, as step-claim adoption would.
//
// The lister's rows are re-checked in memory, so a store that returns a
// superset for the metadata filter cannot make a root read held.
func workflowRootHeldStep(root beads.Bead, list workflowRootStepLister) (beads.Bead, bool, error) {
	rootID := strings.TrimSpace(root.ID)
	if rootID == "" || list == nil || !sourceworkflow.IsWorkflowRoot(root) {
		return beads.Bead{}, false, nil
	}
	steps, err := list(rootID)
	if err != nil {
		return beads.Bead{}, false, err
	}
	for _, step := range steps {
		if strings.TrimSpace(step.ID) == "" || step.ID == rootID ||
			strings.TrimSpace(step.Metadata[beadmeta.RootBeadIDMetadataKey]) != rootID ||
			!strings.EqualFold(strings.TrimSpace(step.Status), "in_progress") ||
			strings.TrimSpace(step.Assignee) == "" {
			continue
		}
		return step, true, nil
	}
	return beads.Bead{}, false, nil
}
