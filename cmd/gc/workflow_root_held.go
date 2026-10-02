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
// The held check alone left the gap BETWEEN steps (bgc-jt6h). A draining
// worker's root is released by releaseUnexecutedClaimsOnDrainAck, and until
// the next step is claimed nothing is held. On 2026-10-01 Dispatch's
// do-work-publish-first roots were claimed 266 times across 63 roots, on the
// Apple and portable routes alike: gcd-935h24's implement had closed and its
// only open steps were a run-operator's close-source-anchor and the control
// dispatcher's workflow-finalize, so each claimant found nothing to run,
// drained, and handed the root to the next. A root none of whose live steps is
// ready work for the claimant's route has nothing to launch either.
//
// So both readers of pool demand ask the same question here: the controller
// before it counts a root as demand, and gc hook --claim before it claims one.

import (
	"fmt"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/sourceworkflow"
)

// workflowRootStepLister lists the live (not closed) steps of the workflow
// root rootID, i.e. the beads whose gc.root_bead_id names it.
type workflowRootStepLister func(rootID string) ([]beads.Bead, error)

// workflowRootStepsQuery is the store query a workflowRootStepLister runs. It
// names no status, so closed steps are left out and every other one is read.
func workflowRootStepsQuery(rootID string) beads.ListQuery {
	return beads.ListQuery{
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

// workflowRootSkipReason reports why root must be neither claimed nor counted
// as launch demand by a claimant whose route routedHere matches, or "" when it
// is still a launch for that claimant. A bead that is not a workflow root is
// always "".
//
// The root is skipped when one of its steps is held, or when it has live steps
// and none of them is ready work routed to the claimant. A root with no live
// steps at all is a launch: a root-only molecule creates no steps, so its root
// IS the work, and a fully closed root is the finalizer's to close.
//
// The lister's rows are re-checked in memory, so a store that returns a
// superset for the metadata filter cannot make a root read held or idle.
func workflowRootSkipReason(root beads.Bead, list workflowRootStepLister, routedHere func(beads.Bead) bool, now time.Time) (string, error) {
	rootID := strings.TrimSpace(root.ID)
	if rootID == "" || list == nil || routedHere == nil || !sourceworkflow.IsWorkflowRoot(root) {
		return "", nil
	}
	rows, err := list(rootID)
	if err != nil {
		return "", err
	}
	steps := liveWorkflowRootSteps(rootID, rows)
	if step, held := heldWorkflowRootStep(steps); held {
		return fmt.Sprintf("its step %s is in progress under %s", step.ID, strings.TrimSpace(step.Assignee)), nil
	}
	if len(steps) == 0 || hasReadyWorkflowRootStep(steps, routedHere, now) {
		return "", nil
	}
	return fmt.Sprintf("none of its %d open steps is ready for this route", len(steps)), nil
}

// liveWorkflowRootSteps keeps the rows that are steps of rootID and not closed.
func liveWorkflowRootSteps(rootID string, rows []beads.Bead) []beads.Bead {
	steps := make([]beads.Bead, 0, len(rows))
	for _, step := range rows {
		if strings.TrimSpace(step.ID) == "" || step.ID == rootID ||
			strings.TrimSpace(step.Metadata[beadmeta.RootBeadIDMetadataKey]) != rootID ||
			strings.EqualFold(strings.TrimSpace(step.Status), "closed") {
			continue
		}
		steps = append(steps, step)
	}
	return steps
}

// heldWorkflowRootStep returns a step a session holds in progress.
//
// "Held" is in_progress with an assignee. Whether that assignee is ALIVE is the
// reconciler's question, not this one: a dead session's work is released by
// releaseWorkFromClosedSessionBead and releaseOrphanedPoolAssignments, and the
// step it held then reads open here.
//
// A control bead never holds the root, even while the control dispatcher is
// working it: control beads stay open throughout, so open cannot tell a control
// being processed from one waiting on its deps.
func heldWorkflowRootStep(steps []beads.Bead) (beads.Bead, bool) {
	for _, step := range steps {
		if strings.EqualFold(strings.TrimSpace(step.Status), "in_progress") && strings.TrimSpace(step.Assignee) != "" {
			return step, true
		}
	}
	return beads.Bead{}, false
}

// hasReadyWorkflowRootStep reports whether a step is ready work the claimant
// could be served: open, unassigned, not deferred, off every dispatch hold,
// routed to the claimant, and not blocked by another live step of the root.
//
// It leans toward ready wherever the rows cannot prove otherwise, because a
// wrong "ready" only costs the loop this check exists to stop, while a wrong
// "not ready" would keep a workflow from launching. An unrouted step counts as
// the claimant's, as unrouted work is claimable by any route, unless it is a
// control bead; and a blocker that is not a live step of this root (another
// root's bead, or a row the store left out) counts as met.
func hasReadyWorkflowRootStep(steps []beads.Bead, routedHere func(beads.Bead) bool, now time.Time) bool {
	live := make(map[string]struct{}, len(steps))
	for _, step := range steps {
		live[step.ID] = struct{}{}
	}
	for _, step := range steps {
		if !strings.EqualFold(strings.TrimSpace(step.Status), "open") ||
			beads.IsDeferred(step, now) || hookCandidateBudgetDeferred(step, now) ||
			!demandRowServable(step) {
			continue
		}
		if strings.TrimSpace(step.Metadata[beadmeta.RoutedToMetadataKey]) == "" {
			if beadmeta.IsControlKind(strings.TrimSpace(step.Metadata[beadmeta.KindMetadataKey])) {
				continue
			}
		} else if !routedHere(step) {
			continue
		}
		if workflowRootStepBlocked(step, live) {
			continue
		}
		return true
	}
	return false
}

// workflowRootStepBlocked reports whether step waits on a live step of its
// root through a ready-blocking dependency.
func workflowRootStepBlocked(step beads.Bead, live map[string]struct{}) bool {
	for _, dep := range step.Dependencies {
		if issue := strings.TrimSpace(dep.IssueID); issue != "" && issue != step.ID {
			continue
		}
		if !beads.IsReadyBlockingDependencyType(dep.Type) {
			continue
		}
		if _, ok := live[strings.TrimSpace(dep.DependsOnID)]; ok {
			return true
		}
	}
	return false
}
