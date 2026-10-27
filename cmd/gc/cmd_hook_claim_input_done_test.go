package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/convoy"
)

// seedDoneWorkflow builds a store holding a workflow root whose input convoy
// tracks a single member, plus one open step under the root. memberStatus
// controls whether the tracked work is terminal.
func seedDoneWorkflow(t *testing.T, memberStatus string) (*beads.MemStore, beads.Bead, beads.Bead) {
	t.Helper()
	store := beads.NewMemStore()
	work, err := store.Create(beads.Bead{Title: "the work"})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}
	if memberStatus == "closed" {
		if err := store.Close(work.ID); err != nil {
			t.Fatalf("close work: %v", err)
		}
	}
	if memberStatus == "handed_off" {
		if err := store.Update(work.ID, beads.UpdateOpts{Status: strPtr("open"), Assignee: strPtr("sysadmin/gastown.refinery")}); err != nil {
			t.Fatalf("update work: %v", err)
		}
	}
	conv, err := store.Create(beads.Bead{Title: "input convoy", Type: "convoy"})
	if err != nil {
		t.Fatalf("create convoy: %v", err)
	}
	if err := convoy.TrackItem(store, conv.ID, work.ID); err != nil {
		t.Fatalf("track: %v", err)
	}
	root, err := store.Create(beads.Bead{Title: "mol", Metadata: map[string]string{
		beadmeta.KindMetadataKey:          beadmeta.KindWorkflow,
		beadmeta.InputConvoyIDMetadataKey: conv.ID,
		beadmeta.RoutedToMetadataKey:      "rig/worker",
	}})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	step, err := store.Create(beads.Bead{Title: "Load context", Metadata: map[string]string{
		beadmeta.RootBeadIDMetadataKey:        root.ID,
		beadmeta.ContinuationGroupMetadataKey: "pool-workflow",
		beadmeta.RoutedToMetadataKey:          "rig/worker",
	}})
	if err != nil {
		t.Fatalf("create step: %v", err)
	}
	return store, root, step
}

func TestWorkflowInputDoneDetectsTerminalConvoyMember(t *testing.T) {
	store, root, step := seedDoneWorkflow(t, "closed")

	rootID, done, err := workflowInputDone(store, root, nil)
	if err != nil {
		t.Fatalf("workflowInputDone(root): %v", err)
	}
	if !done || rootID != root.ID {
		t.Fatalf("root: done=%v rootID=%q, want done=true rootID=%q", done, rootID, root.ID)
	}

	rootID, done, err = workflowInputDone(store, step, nil)
	if err != nil {
		t.Fatalf("workflowInputDone(step): %v", err)
	}
	if !done || rootID != root.ID {
		t.Fatalf("step: done=%v rootID=%q, want done=true rootID=%q", done, rootID, root.ID)
	}
}

func TestWorkflowInputDoneFalseWhileMemberOpen(t *testing.T) {
	store, root, _ := seedDoneWorkflow(t, "open")
	_, done, err := workflowInputDone(store, root, nil)
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if done {
		t.Fatal("done = true for an open convoy member, want false")
	}
}

func TestWorkflowInputDoneFalseWithoutInputConvoy(t *testing.T) {
	store := beads.NewMemStore()
	root, err := store.Create(beads.Bead{Metadata: map[string]string{
		beadmeta.KindMetadataKey: beadmeta.KindWorkflow,
	}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, done, err := workflowInputDone(store, root, nil)
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if done {
		t.Fatal("done = true with no input convoy, want false")
	}
}

func TestWorkflowInputDoneTrueWhenMemberHandedOffToOtherLane(t *testing.T) {
	store, root, _ := seedDoneWorkflow(t, "handed_off")
	_, done, err := workflowInputDone(store, root, []string{"gastown__polecat-gc-xxxx"})
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if !done {
		t.Fatal("done = false for a member handed off to another lane, want true")
	}
}

// A member still assigned to a seat that ran a step of this same workflow was
// not handed off: that seat is the workflow's own pool worker (typically dead,
// its routed steps already reopened). A successor seat must adopt the workflow,
// not retire it (sys-pxryan.44).
func TestWorkflowInputDoneFalseWhenMemberHeldBySiblingSeatOfSameWorkflow(t *testing.T) {
	store, root, step := seedDoneWorkflow(t, "open")
	const deadSeat = "homeops__homeops-luna-gc-5g2aze"
	claimStep, err := store.Create(beads.Bead{Title: "claim input", Assignee: deadSeat, Metadata: map[string]string{
		beadmeta.RootBeadIDMetadataKey:        root.ID,
		beadmeta.ContinuationGroupMetadataKey: "pool-workflow",
		beadmeta.RoutedToMetadataKey:          "rig/worker",
	}})
	if err != nil {
		t.Fatalf("create claim step: %v", err)
	}
	if err := store.Close(claimStep.ID); err != nil {
		t.Fatalf("close claim step: %v", err)
	}
	members, err := convoy.Members(store, root.Metadata[beadmeta.InputConvoyIDMetadataKey], true)
	if err != nil || len(members) != 1 {
		t.Fatalf("convoy.Members = %d, %v; want 1 member", len(members), err)
	}
	if err := store.Update(members[0].ID, beads.UpdateOpts{Status: strPtr("in_progress"), Assignee: strPtr(deadSeat)}); err != nil {
		t.Fatalf("update work: %v", err)
	}

	_, done, err := workflowInputDone(store, step, []string{"homeops__homeops-luna-gc-9b2b0a"})
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if done {
		t.Fatal("done = true for a member held by a sibling seat of the same workflow, want false")
	}
}

// A seat that died before closing its step leaves that step reopened with the
// assignee cleared; only the step's gc.session_name/gc.session_id claim
// provenance still names it. That seat is still this workflow's own worker.
func TestWorkflowInputDoneFalseWhenMemberHeldBySiblingSeatWhoseStepWasReopened(t *testing.T) {
	store, root, step := seedDoneWorkflow(t, "open")
	const deadSeat = "homeops__homeops-luna-gc-5g2aze"
	if _, err := store.Create(beads.Bead{Title: "claim input", Status: "open", Metadata: map[string]string{
		beadmeta.RootBeadIDMetadataKey:        root.ID,
		beadmeta.ContinuationGroupMetadataKey: "pool-workflow",
		beadmeta.RoutedToMetadataKey:          "rig/worker",
		beadmeta.SessionNameMetadataKey:       deadSeat,
		beadmeta.SessionIDMetadataKey:         "gc-5g2aze",
	}}); err != nil {
		t.Fatalf("create reopened claim step: %v", err)
	}
	members, err := convoy.Members(store, root.Metadata[beadmeta.InputConvoyIDMetadataKey], true)
	if err != nil || len(members) != 1 {
		t.Fatalf("convoy.Members = %d, %v; want 1 member", len(members), err)
	}
	if err := store.Update(members[0].ID, beads.UpdateOpts{Status: strPtr("in_progress"), Assignee: strPtr(deadSeat)}); err != nil {
		t.Fatalf("update work: %v", err)
	}

	_, done, err := workflowInputDone(store, step, []string{"homeops__homeops-luna-gc-9b2b0a"})
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if done {
		t.Fatal("done = true for a member held by a sibling seat whose step was reopened, want false")
	}
}

// A reopened step may keep only gc.session_id as provenance; a member whose
// assignee is that session id is still held by this workflow's own seat.
func TestWorkflowInputDoneFalseWhenMemberHeldBySessionIDOnlyProvenance(t *testing.T) {
	store, root, step := seedDoneWorkflow(t, "open")
	const deadSessionID = "gc-5g2aze"
	if _, err := store.Create(beads.Bead{Title: "claim input", Status: "open", Metadata: map[string]string{
		beadmeta.RootBeadIDMetadataKey:        root.ID,
		beadmeta.ContinuationGroupMetadataKey: "pool-workflow",
		beadmeta.RoutedToMetadataKey:          "rig/worker",
		beadmeta.SessionIDMetadataKey:         deadSessionID,
	}}); err != nil {
		t.Fatalf("create reopened claim step: %v", err)
	}
	members, err := convoy.Members(store, root.Metadata[beadmeta.InputConvoyIDMetadataKey], true)
	if err != nil || len(members) != 1 {
		t.Fatalf("convoy.Members = %d, %v; want 1 member", len(members), err)
	}
	if err := store.Update(members[0].ID, beads.UpdateOpts{Status: strPtr("in_progress"), Assignee: strPtr(deadSessionID)}); err != nil {
		t.Fatalf("update work: %v", err)
	}

	_, done, err := workflowInputDone(store, step, []string{"homeops__homeops-luna-gc-9b2b0a"})
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if done {
		t.Fatal("done = true for a member held by a seat named only by gc.session_id provenance, want false")
	}
}

func TestWorkflowInputDoneFalseWhenMemberAssignedToIdentityCandidate(t *testing.T) {
	store, root, _ := seedDoneWorkflow(t, "handed_off")
	convoyID := root.Metadata[beadmeta.InputConvoyIDMetadataKey]
	members, err := convoy.Members(store, convoyID, true)
	if err != nil {
		t.Fatalf("convoy.Members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members = %d, want 1", len(members))
	}
	if err := store.Update(members[0].ID, beads.UpdateOpts{Assignee: strPtr("gastown__polecat-gc-xxxx")}); err != nil {
		t.Fatalf("update work: %v", err)
	}
	_, done, err := workflowInputDone(store, root, []string{"gastown__polecat-gc-xxxx"})
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if done {
		t.Fatal("done = true for a member assigned to one of identityCandidates, want false")
	}
}

func TestWorkflowInputDoneFalseWhenMemberUnassignedOpen(t *testing.T) {
	store, root, _ := seedDoneWorkflow(t, "open")
	_, done, err := workflowInputDone(store, root, []string{"gastown__polecat-gc-xxxx"})
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if done {
		t.Fatal("done = true for an unassigned open member, want false")
	}
}

func TestSkipDoneWorkflowClosesRootAndSteps(t *testing.T) {
	store, root, step := seedDoneWorkflow(t, "closed")
	if err := store.Update(step.ID, beads.UpdateOpts{Status: strPtr("in_progress"), Assignee: strPtr("worker-1")}); err != nil {
		t.Fatalf("update step: %v", err)
	}

	if err := skipDoneWorkflow(store, root.ID); err != nil {
		t.Fatalf("skipDoneWorkflow: %v", err)
	}
	for _, id := range []string{root.ID, step.ID} {
		got, err := store.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if got.Status != "closed" {
			t.Fatalf("%s status = %q, want closed", id, got.Status)
		}
		if got.Metadata[beadmeta.OutcomeMetadataKey] != beadmeta.OutcomeSkipped {
			t.Fatalf("%s gc.outcome = %q, want skipped", id, got.Metadata[beadmeta.OutcomeMetadataKey])
		}
	}
}

func TestDoHookClaimSkipsWorkflowWhoseInputIsDone(t *testing.T) {
	candidates := []beads.Bead{{
		ID:     "root-1",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:          beadmeta.KindWorkflow,
			beadmeta.InputConvoyIDMetadataKey: "convoy-1",
			beadmeta.RoutedToMetadataKey:      "worker",
		},
	}}
	output, err := json.Marshal(candidates)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	claimed := false
	skipped := ""
	drained := false
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) { return string(output), nil },
		Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
			claimed = true
			return beads.Bead{ID: beadID, Assignee: assignee}, true, nil
		},
		InputDone: func(_ context.Context, _ string, _ []string, _ beads.Bead, _ []string) (string, bool, error) {
			return "root-1", true, nil
		},
		SkipDoneWorkflow: func(_ context.Context, _ string, _ []string, rootID string) error {
			skipped = rootID
			return nil
		},
		DrainAck: func(io.Writer) error {
			drained = true
			return nil
		},
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "/rig", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker"},
		DrainAck:     true,
		JSON:         true,
	}, ops, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("doHookClaim = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if claimed {
		t.Fatal("claimed a workflow whose input convoy work is already terminal")
	}
	if skipped != "root-1" {
		t.Fatalf("skipped root = %q, want root-1", skipped)
	}
	if !drained {
		t.Fatal("no-work drain not acknowledged after skipping the done workflow")
	}
	if !strings.Contains(stdout.String(), `"reason":"no_work"`) {
		t.Fatalf("stdout = %q, want no_work drain", stdout.String())
	}
}

func TestDoHookClaimSkipsExistingAssignmentWhoseInputIsDone(t *testing.T) {
	// The per-seat treadmill: the seat already holds an in_progress step of a
	// molecule whose work finished elsewhere. The hook must not hand that step
	// back as existing_assignment; it must close the molecule and drain.
	candidates := []beads.Bead{{
		ID:       "step-1",
		Status:   "in_progress",
		Assignee: "worker-1",
		Metadata: map[string]string{
			beadmeta.RootBeadIDMetadataKey:        "root-1",
			beadmeta.ContinuationGroupMetadataKey: "pool-workflow",
			beadmeta.RoutedToMetadataKey:          "worker",
		},
	}}
	output, err := json.Marshal(candidates)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	skipped := ""
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) { return string(output), nil },
		Claim: func(_ context.Context, _ string, _ []string, beadID, _ string) (beads.Bead, bool, error) {
			t.Fatalf("unexpected claim of %s", beadID)
			return beads.Bead{}, false, nil
		},
		InputDone: func(_ context.Context, _ string, _ []string, _ beads.Bead, _ []string) (string, bool, error) {
			return "root-1", true, nil
		},
		SkipDoneWorkflow: func(_ context.Context, _ string, _ []string, rootID string) error {
			skipped = rootID
			return nil
		},
		DrainAck: func(io.Writer) error { return nil },
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "/rig", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker"},
		DrainAck:     true,
		JSON:         true,
	}, ops, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("doHookClaim = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if skipped != "root-1" {
		t.Fatalf("skipped root = %q, want root-1", skipped)
	}
	if strings.Contains(stdout.String(), "existing_assignment") {
		t.Fatalf("stdout = %q, handed back a step of a done workflow", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"reason":"no_work"`) {
		t.Fatalf("stdout = %q, want no_work drain", stdout.String())
	}
}

func TestDoHookClaimStillClaimsWhenInputNotDone(t *testing.T) {
	candidates := []beads.Bead{{
		ID:     "root-1",
		Status: "open",
		Metadata: map[string]string{
			beadmeta.KindMetadataKey:          beadmeta.KindWorkflow,
			beadmeta.InputConvoyIDMetadataKey: "convoy-1",
			beadmeta.RoutedToMetadataKey:      "worker",
		},
	}}
	output, err := json.Marshal(candidates)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	claimedID := ""
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) { return string(output), nil },
		Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
			claimedID = beadID
			return beads.Bead{ID: beadID, Assignee: assignee, Status: "in_progress"}, true, nil
		},
		InputDone: func(_ context.Context, _ string, _ []string, _ beads.Bead, _ []string) (string, bool, error) {
			return "root-1", false, nil
		},
		SkipDoneWorkflow: func(_ context.Context, _ string, _ []string, rootID string) error {
			t.Fatalf("unexpected skip of %s", rootID)
			return nil
		},
		ListContinuation: func(_ context.Context, _ string, _ []string, _, _ string) ([]beads.Bead, error) {
			return nil, nil
		},
		AssignContinuation: func(_ context.Context, _ string, _ []string, _, _ string) error { return nil },
		ResolveWorkBranch:  func(hookClaimWorkTree) string { return "" },
		StampWorkMeta: func(_ context.Context, _ string, _ []string, _, _ string, _ map[string]string) error {
			return nil
		},
		PublishRunMap: func(_, _ string, _ ...string) error { return nil },
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "/rig", hookClaimOptions{
		Assignee:     "worker-1",
		RouteTargets: []string{"worker"},
		JSON:         true,
	}, ops, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("doHookClaim = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if claimedID != "root-1" {
		t.Fatalf("claimed = %q, want root-1", claimedID)
	}
}

// seedDrainedWorkflow builds a build-from-bead shaped workflow whose input
// convoy item was taken by the workflow's own drain step and closed with
// gc.outcome=pass when its item published, plus an open, ready review step
// that runs after the drain. manifest controls whether the drain recorded the
// input (expanded) or never started (no manifest).
func seedDrainedWorkflow(t *testing.T, manifest bool) (*beads.MemStore, beads.Bead, beads.Bead) {
	t.Helper()
	store, root, _ := seedDoneWorkflow(t, "open")
	members, err := convoy.Members(store, root.Metadata[beadmeta.InputConvoyIDMetadataKey], true)
	if err != nil || len(members) != 1 {
		t.Fatalf("convoy.Members = %d, %v; want 1 member", len(members), err)
	}
	if _, err := store.CloseAll([]string{members[0].ID}, map[string]string{beadmeta.OutcomeMetadataKey: beadmeta.OutcomePass}); err != nil {
		t.Fatalf("close work: %v", err)
	}
	drainMeta := map[string]string{
		beadmeta.RootBeadIDMetadataKey: root.ID,
		beadmeta.KindMetadataKey:       beadmeta.KindDrain,
		beadmeta.RoutedToMetadataKey:   "rig/control-dispatcher",
	}
	if manifest {
		drainMeta[beadmeta.DrainStateMetadataKey] = beadmeta.DrainStateSucceeded
		drainMeta[beadmeta.DrainManifestMetadataKey] = `{"version":1,"rows":[{"index":0,"member_id":"` + members[0].ID + `","status":"wired"}]}`
	}
	drain, err := store.Create(beads.Bead{Title: "Drain implementation convoy", Metadata: drainMeta})
	if err != nil {
		t.Fatalf("create drain: %v", err)
	}
	if manifest {
		if _, err := store.CloseAll([]string{drain.ID}, map[string]string{beadmeta.OutcomeMetadataKey: beadmeta.OutcomePass}); err != nil {
			t.Fatalf("close drain: %v", err)
		}
	}
	review, err := store.Create(beads.Bead{Title: "Review", Metadata: map[string]string{
		beadmeta.RootBeadIDMetadataKey:        root.ID,
		beadmeta.ContinuationGroupMetadataKey: "pool-workflow",
		beadmeta.RoutedToMetadataKey:          "rig/worker",
	}})
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	if err := store.DepAdd(review.ID, drain.ID, "blocks"); err != nil {
		t.Fatalf("review depends on drain: %v", err)
	}
	return store, root, review
}

// An input item the workflow's own drain took and closed with
// gc.outcome=pass is the workflow's output, not work finished elsewhere: the
// review, repair, publish and finalize stages after the drain are still the
// workflow's to run.
func TestWorkflowInputDoneFalseWhenOwnDrainClosedTheInput(t *testing.T) {
	store, root, review := seedDrainedWorkflow(t, true)
	for _, candidate := range []beads.Bead{root, review} {
		_, done, err := workflowInputDone(store, candidate, []string{"rig__worker-gc-fresh"})
		if err != nil {
			t.Fatalf("workflowInputDone(%s): %v", candidate.ID, err)
		}
		if done {
			t.Fatalf("workflowInputDone(%s) = done after the workflow's own drain closed the input, want not done", candidate.ID)
		}
	}
}

// A drain that has not recorded the input yet has not taken it: an input
// closed elsewhere before the drain ran still retires the workflow.
func TestWorkflowInputDoneTrueWhenInputClosedBeforeDrainRan(t *testing.T) {
	store, root, review := seedDrainedWorkflow(t, false)
	_, done, err := workflowInputDone(store, review, nil)
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if !done {
		t.Fatalf("workflowInputDone(%s) = not done for an input closed before the drain of %s recorded it, want done", review.ID, root.ID)
	}
}

// While the drain's item workflow holds the input, its seat is not one of the
// parent workflow's step seats, but the input was not handed off either.
func TestWorkflowInputDoneFalseWhenOwnDrainHoldsTheInput(t *testing.T) {
	store, root, review := seedDrainedWorkflow(t, true)
	members, err := convoy.Members(store, root.Metadata[beadmeta.InputConvoyIDMetadataKey], true)
	if err != nil || len(members) != 1 {
		t.Fatalf("convoy.Members = %d, %v; want 1 member", len(members), err)
	}
	if err := store.Update(members[0].ID, beads.UpdateOpts{Status: strPtr("in_progress"), Assignee: strPtr("rig__item-worker-gc-other")}); err != nil {
		t.Fatalf("update work: %v", err)
	}
	_, done, err := workflowInputDone(store, review, []string{"rig__worker-gc-fresh"})
	if err != nil {
		t.Fatalf("workflowInputDone: %v", err)
	}
	if done {
		t.Fatal("workflowInputDone = done while the workflow's own drain item holds the input, want not done")
	}
}

// A fresh seat claiming the post-drain review step of a workflow whose input
// item closed with gc.outcome=pass claims it, and leaves the workflow alone.
func TestDoHookClaimClaimsPostDrainStepAfterInputPassed(t *testing.T) {
	store, root, review := seedDrainedWorkflow(t, true)
	output, err := json.Marshal([]beads.Bead{review})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	claimedID := ""
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) { return string(output), nil },
		Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
			claimedID = beadID
			return beads.Bead{ID: beadID, Assignee: assignee, Status: "in_progress", Metadata: review.Metadata}, true, nil
		},
		InputDone: func(_ context.Context, _ string, _ []string, candidate beads.Bead, identities []string) (string, bool, error) {
			return workflowInputDone(store, candidate, identities)
		},
		SkipDoneWorkflow: func(_ context.Context, _ string, _ []string, rootID string) error {
			return skipDoneWorkflow(store, rootID)
		},
		ListContinuation: func(_ context.Context, _ string, _ []string, _, _ string) ([]beads.Bead, error) {
			return nil, nil
		},
		AssignContinuation: func(_ context.Context, _ string, _ []string, _, _ string) error { return nil },
		ResolveWorkBranch:  func(hookClaimWorkTree) string { return "" },
		StampWorkMeta: func(_ context.Context, _ string, _ []string, _, _ string, _ map[string]string) error {
			return nil
		},
		PublishRunMap: func(_, _ string, _ ...string) error { return nil },
	}

	var stdout, stderr bytes.Buffer
	code := doHookClaim("query", "/rig", hookClaimOptions{
		Assignee:     "rig__worker-gc-fresh",
		RouteTargets: []string{"rig/worker"},
		JSON:         true,
	}, ops, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("doHookClaim = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if claimedID != review.ID {
		t.Fatalf("claimed = %q, want review step %s (stdout=%q stderr=%q)", claimedID, review.ID, stdout.String(), stderr.String())
	}
	for _, id := range []string{root.ID, review.ID} {
		got, err := store.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if got.Status == "closed" || got.Metadata[beadmeta.OutcomeMetadataKey] == beadmeta.OutcomeSkipped {
			t.Fatalf("%s status=%q gc.outcome=%q, want left open by the claim", id, got.Status, got.Metadata[beadmeta.OutcomeMetadataKey])
		}
	}
}
