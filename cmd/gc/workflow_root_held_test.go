package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

// The three gsc-tf857 shapes from 2026-10-01, all on GasCityDispatch's
// do-work-publish-first workflow. Ids are the live ones so a reader can find
// the events.
const (
	tf857Pool = "GasCityDispatch/portable-worker"
	// 08:39Z: gcd-lcredp's first holder died; root and step were released
	// together and the replacement claimed only the step.
	tf857DeadRoot = "gcd-lcredp"
	tf857DeadStep = "gcd-py5hiz"
	// 11:57Z: gcd-3zfl08 was offered to portable-worker-3 while its implement
	// gcd-qjyclp ran under portable-worker-2.
	tf857LiveRoot = "gcd-3zfl08"
	tf857LiveStep = "gcd-qjyclp"
	// 09:41Z / 10:40Z: gcd-ldpl8b's implement steps had closed and its
	// close-source-anchor ran under a run-operator.
	tf857LateRoot = "gcd-ldpl8b"
)

func tf857Root(id string) beads.Bead {
	return beads.Bead{
		ID:     id,
		Status: "open",
		Type:   "task",
		Metadata: map[string]string{
			"gc.kind":             "workflow",
			"gc.formula_contract": "graph.v2",
			"gc.routed_to":        tf857Pool,
		},
	}
}

func tf857Step(id, rootID, status, assignee, route string) beads.Bead {
	return beads.Bead{
		ID:       id,
		Status:   status,
		Assignee: assignee,
		Type:     "task",
		Metadata: map[string]string{
			"gc.root_bead_id": rootID,
			"gc.routed_to":    route,
			"gc.step_id":      "implement",
		},
	}
}

func tf857Rows(t *testing.T, rows ...beads.Bead) string {
	t.Helper()
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func tf857ClaimOpts() hookClaimOptions {
	return hookClaimOptions{
		Assignee:           "bgc-wisp-5fqzb",
		IdentityCandidates: []string{"bgc-wisp-5fqzb"},
		RouteTargets:       []string{tf857Pool},
		JSON:               true,
	}
}

func TestWorkflowRootSkipReason(t *testing.T) {
	root := tf857Root(tf857LiveRoot)
	held := tf857Step(tf857LiveStep, tf857LiveRoot, "in_progress", "bgc-wisp-vpe9u", tf857Pool)
	list := func(steps ...beads.Bead) workflowRootStepLister {
		return func(rootID string) ([]beads.Bead, error) {
			if rootID != tf857LiveRoot {
				t.Fatalf("listed steps of %q, want %q", rootID, tf857LiveRoot)
			}
			return steps, nil
		}
	}
	readyHere := tf857Step("s-ready", tf857LiveRoot, "open", "", tf857Pool)
	elsewhere := tf857Step("s-other", tf857LiveRoot, "open", "", "GasCityDispatch/run-operator")
	for _, tc := range []struct {
		name string
		root beads.Bead
		list workflowRootStepLister
		want string
	}{
		{name: "a step in progress under a session holds the root", root: root, list: list(held), want: "its step " + tf857LiveStep + " is in progress under bgc-wisp-vpe9u"},
		{name: "no live steps is a launch", root: root, list: list()},
		{name: "a ready step routed here is a launch", root: root, list: list(readyHere, elsewhere)},
		{name: "an unrouted ready step is a launch", root: root, list: list(beads.Bead{ID: "s-1", Status: "open", Metadata: map[string]string{"gc.root_bead_id": tf857LiveRoot}})},
		{name: "a step blocked by another root's bead is a launch", root: root, list: list(jt6hWithDeps(readyHere, jt6hBlocks("s-ready", "gcd-other")))},
		{name: "an in-progress step with no assignee does not hold it", root: root, list: list(tf857Step("s-1", tf857LiveRoot, "in_progress", "", tf857Pool)), want: "none of its 1 open steps is ready for this route"},
		{name: "only steps routed elsewhere", root: root, list: list(elsewhere), want: "none of its 1 open steps is ready for this route"},
		{name: "a step routed here but assigned", root: root, list: list(tf857Step("s-1", tf857LiveRoot, "open", "bgc-wisp-vpe9u", tf857Pool)), want: "none of its 1 open steps is ready for this route"},
		{name: "a step routed here but blocked by a live step", root: root, list: list(jt6hWithDeps(readyHere, jt6hBlocks("s-ready", "s-other")), elsewhere), want: "none of its 2 open steps is ready for this route"},
		{name: "a step routed here but on a dispatch hold", root: root, list: list(jt6hWithLabels(readyHere, "hold:mayor")), want: "none of its 1 open steps is ready for this route"},
		{name: "a step routed here but deferred", root: root, list: list(jt6hDeferred(readyHere)), want: "none of its 1 open steps is ready for this route"},
		// A control bead stays OPEN while the dispatcher works it, so open
		// cannot tell "being worked" from "waiting", and every graph root has
		// an open finalizer from launch on. It neither holds the root nor is
		// work for a pool.
		{name: "an open control bead neither holds nor launches it", root: root, list: list(beads.Bead{ID: "s-1", Status: "open", Metadata: map[string]string{"gc.root_bead_id": tf857LiveRoot, "gc.kind": "workflow-finalize"}}), want: "none of its 1 open steps is ready for this route"},
		// bgc-3q3y: a step-spec sidecar and a scope latch are unrouted and stay
		// open for most of a workflow's life, but neither is work.
		{name: "an open step-spec sidecar is not a launch", root: root, list: list(q3ySpec(tf857LiveRoot)), want: "none of its 1 open steps is ready for this route"},
		{name: "an open scope latch is not a launch", root: root, list: list(beads.Bead{ID: "s-1", Status: "open", Metadata: map[string]string{"gc.root_bead_id": tf857LiveRoot, "gc.kind": "scope"}}), want: "none of its 1 open steps is ready for this route"},
		{name: "another root's step in a superset answer is ignored", root: root, list: list(tf857Step("s-1", "gcd-other", "in_progress", "bgc-wisp-vpe9u", tf857Pool))},
		{name: "a closed step in a superset answer is ignored", root: root, list: list(tf857Step("s-1", tf857LiveRoot, "closed", "", "GasCityDispatch/run-operator"))},
		{name: "the root itself in the answer is ignored", root: root, list: list(beads.Bead{ID: tf857LiveRoot, Status: "in_progress", Assignee: "x", Metadata: map[string]string{"gc.root_bead_id": tf857LiveRoot}})},
		{name: "an ordinary bead is never skipped", root: beads.Bead{ID: tf857LiveRoot, Status: "open"}, list: func(string) ([]beads.Bead, error) {
			t.Fatal("listed steps of a bead that is not a workflow root")
			return nil, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := workflowRootSkipReason(tc.root, tc.list, tf857RoutedHere, tf857Now)
			if err != nil {
				t.Fatalf("workflowRootSkipReason: %v", err)
			}
			if got != tc.want {
				t.Fatalf("workflowRootSkipReason = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("a failed read is returned", func(t *testing.T) {
		boom := errors.New("store down")
		got, err := workflowRootSkipReason(root, func(string) ([]beads.Bead, error) { return nil, boom }, tf857RoutedHere, tf857Now)
		if !errors.Is(err, boom) || got != "" {
			t.Fatalf("workflowRootSkipReason = (%q, %v), want (\"\", %v)", got, err, boom)
		}
	})
}

var tf857Now = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)

func tf857RoutedHere(step beads.Bead) bool {
	return hookClaimMatchesRoute(step, []string{tf857Pool})
}

func jt6hBlocks(issueID, dependsOnID string) beads.Dep {
	return beads.Dep{IssueID: issueID, DependsOnID: dependsOnID, Type: "blocks"}
}

func jt6hWithDeps(b beads.Bead, deps ...beads.Dep) beads.Bead {
	b.Dependencies = append(slices.Clone(b.Dependencies), deps...)
	return b
}

func jt6hWithLabels(b beads.Bead, labels ...string) beads.Bead {
	b.Labels = append(slices.Clone(b.Labels), labels...)
	return b
}

func jt6hDeferred(b beads.Bead) beads.Bead {
	until := tf857Now.Add(time.Hour)
	b.DeferUntil = &until
	return b
}

// 11:57Z and 09:41Z/10:40Z: a root routed to the pool is not claimable while a
// step of it runs under another session, and the claim moves on to the next
// routed row instead.
func TestDoHookClaimSkipsWorkflowRootHeldThroughItsStep(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rootID string
		held   beads.Bead
	}{
		{name: "implement held by another pool worker", rootID: tf857LiveRoot, held: tf857Step(tf857LiveStep, tf857LiveRoot, "in_progress", "bgc-wisp-portable-worker-2", tf857Pool)},
		{name: "close-source-anchor held by a run-operator", rootID: tf857LateRoot, held: tf857Step("gcd-anchor", tf857LateRoot, "in_progress", "bgc-wisp-run-operator", "GasCityDispatch/run-operator")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := beads.Bead{ID: "gcd-next", Status: "open", Type: "task", Metadata: map[string]string{"gc.routed_to": tf857Pool}}
			var claimed []string
			ops := hookClaimOps{
				Runner: func(string, string) (string, error) { return tf857Rows(t, tf857Root(tc.rootID), next), nil },
				ListRootSteps: func(_ context.Context, _ string, _ []string, rootID string) ([]beads.Bead, error) {
					if rootID != tc.rootID {
						return nil, nil
					}
					return []beads.Bead{tc.held}, nil
				},
				Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
					claimed = append(claimed, beadID)
					return beads.Bead{ID: beadID, Status: "in_progress", Assignee: assignee, Metadata: next.Metadata}, true, nil
				},
			}
			var stdout, stderr bytes.Buffer
			if code := doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), ops, &stdout, &stderr); code != 0 {
				t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
			}
			if got := strings.Join(claimed, ","); got != "gcd-next" {
				t.Fatalf("claimed %q, want only gcd-next; stderr=%s", got, stderr.String())
			}
			if !strings.Contains(stderr.String(), "skipping workflow root "+tc.rootID+": its step "+tc.held.ID) {
				t.Fatalf("stderr = %q, want the skipped root named with its held step", stderr.String())
			}
		})
	}
}

// A root none of whose steps is held is still a launch: the first claim of a
// workflow must keep working.
func TestDoHookClaimStillLaunchesAnUnheldWorkflowRoot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps func(context.Context, string, []string, string) ([]beads.Bead, error)
		warn  string
	}{
		{name: "no step in progress", steps: func(context.Context, string, []string, string) ([]beads.Bead, error) { return nil, nil }},
		{name: "steps unreadable fails open", steps: func(context.Context, string, []string, string) ([]beads.Bead, error) {
			return nil, errors.New("store down")
		}, warn: "offering it unchecked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var claimed []string
			ops := hookClaimOps{
				Runner:        func(string, string) (string, error) { return tf857Rows(t, tf857Root(tf857LiveRoot)), nil },
				ListRootSteps: tc.steps,
				Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
					claimed = append(claimed, beadID)
					root := tf857Root(beadID)
					root.Status, root.Assignee = "in_progress", assignee
					return root, true, nil
				},
			}
			var stdout, stderr bytes.Buffer
			if code := doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), ops, &stdout, &stderr); code != 0 {
				t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
			}
			if got := strings.Join(claimed, ","); got != tf857LiveRoot {
				t.Fatalf("claimed %q, want the root %s", got, tf857LiveRoot)
			}
			if tc.warn != "" && !strings.Contains(stderr.String(), tc.warn) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.warn)
			}
		})
	}
}

// 08:39Z: the replacement session claims the released step, and with it the
// released root, so the root is never left as open demand for another worker.
func TestDoHookClaimAdoptsTheReleasedRootOfAClaimedStep(t *testing.T) {
	step := tf857Step(tf857DeadStep, tf857DeadRoot, "open", "", tf857Pool)
	root := tf857Root(tf857DeadRoot)
	var claimed []string
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) { return tf857Rows(t, step), nil },
		Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
			claimed = append(claimed, beadID+"="+assignee)
			bead := step
			if beadID == tf857DeadRoot {
				bead = root
			}
			bead.Status, bead.Assignee = "in_progress", assignee
			return bead, true, nil
		},
		ReadWorkMeta: func(_ context.Context, _ string, _ []string, beadID, _ string) (beads.Bead, error) {
			if beadID != tf857DeadRoot {
				t.Fatalf("read %q, want only the root", beadID)
			}
			return root, nil
		},
	}
	var stdout, stderr bytes.Buffer
	if code := doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), ops, &stdout, &stderr); code != 0 {
		t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
	}
	want := tf857DeadStep + "=bgc-wisp-5fqzb," + tf857DeadRoot + "=bgc-wisp-5fqzb"
	if got := strings.Join(claimed, ","); got != want {
		t.Fatalf("claims = %q, want %q", got, want)
	}
	var result hookClaimJSONResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v\nraw: %s", err, stdout.String())
	}
	if result.BeadID != tf857DeadStep || result.RootBeadID != tf857DeadRoot {
		t.Fatalf("result = %+v, want the step %s under root %s", result, tf857DeadStep, tf857DeadRoot)
	}
	if !strings.Contains(stderr.String(), "adopted released workflow root "+tf857DeadRoot) {
		t.Fatalf("stderr = %q, want the adoption reported", stderr.String())
	}
}

// Adoption only re-links a root that was RELEASED to this session's route; any
// other root is left exactly as it is.
func TestDoHookClaimLeavesAnUnreleasedRootAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*beads.Bead)
	}{
		{name: "held by another session", edit: func(b *beads.Bead) { b.Status, b.Assignee = "in_progress", "bgc-wisp-other" }},
		{name: "launched in progress with no assignee", edit: func(b *beads.Bead) { b.Status = "in_progress" }},
		{name: "routed to another template", edit: func(b *beads.Bead) { b.Metadata["gc.routed_to"] = "GasCityDispatch/run-operator" }},
		{name: "not a workflow root", edit: func(b *beads.Bead) { b.Metadata = map[string]string{"gc.routed_to": tf857Pool} }},
		{name: "closed", edit: func(b *beads.Bead) { b.Status = "closed" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := tf857Step(tf857DeadStep, tf857DeadRoot, "open", "", tf857Pool)
			root := tf857Root(tf857DeadRoot)
			tc.edit(&root)
			var claimed []string
			ops := hookClaimOps{
				Runner: func(string, string) (string, error) { return tf857Rows(t, step), nil },
				Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
					claimed = append(claimed, beadID)
					bead := step
					bead.Status, bead.Assignee = "in_progress", assignee
					return bead, true, nil
				},
				ReadWorkMeta: func(context.Context, string, []string, string, string) (beads.Bead, error) { return root, nil },
			}
			var stdout, stderr bytes.Buffer
			if code := doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), ops, &stdout, &stderr); code != 0 {
				t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
			}
			if got := strings.Join(claimed, ","); got != tf857DeadStep {
				t.Fatalf("claims = %q, want only the step", got)
			}
		})
	}
}

// The controller counts a routed root as capacity demand only while it is a
// launch: once a step runs under a session, a seat spawned for the root could
// only drain.
func TestDefaultScaleCheckSkipsWorkflowRootHeldThroughItsStep(t *testing.T) {
	store := beads.NewMemStore()
	root, err := store.Create(tf857Root(""))
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	count := func() int {
		t.Helper()
		counts, _, errs := defaultScaleCheckCounts([]defaultScaleCheckTarget{{template: tf857Pool, storeKey: "rig:gcd", store: store}})
		if len(errs) != 0 {
			t.Fatalf("defaultScaleCheckCounts errs = %v", errs)
		}
		return counts[tf857Pool]
	}
	if got := count(); got != 1 {
		t.Fatalf("unheld root counts %d, want 1 (a launch is demand)", got)
	}
	// MemStore.Create always writes status open, so the step is put in
	// progress the way a claim does it: by an update.
	step, err := store.Create(tf857Step("", root.ID, "open", "", tf857Pool))
	if err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := store.Update(step.ID, beads.UpdateOpts{Status: strPtr("in_progress"), Assignee: strPtr("bgc-wisp-portable-worker-2")}); err != nil {
		t.Fatalf("put step in progress: %v", err)
	}
	if got := count(); got != 0 {
		t.Fatalf("root held through its step counts %d, want 0", got)
	}
}

// The bgc-jt6h shapes from 2026-10-01: Dispatch do-work-publish-first roots
// claimed and dropped over and over with nothing held, on both routes.
const (
	// 23:21Z / 00:15Z: gcd-935h24's implement had closed. Its only open steps
	// were close-source-anchor (a run-operator's) and workflow-finalize (the
	// control dispatcher's), so five portable workers in turn claimed it,
	// found nothing to run, and drained.
	jt6hLateRoot = "gcd-935h24"
	// 13:19Z-13:31Z: gcd-xwry0b was claimed before its implement could be,
	// while the implement still waited on a step routed elsewhere.
	jt6hEarlyRoot   = "gcd-xwry0b"
	jt6hRunOperator = "GasCityDispatch/run-operator"
	jt6hControl     = "core.control-dispatcher"
)

func jt6hFinalize(rootID string, blockedBy ...string) beads.Bead {
	step := beads.Bead{
		ID:     rootID + "-finalize",
		Status: "open",
		Type:   "task",
		Metadata: map[string]string{
			"gc.root_bead_id": rootID,
			"gc.routed_to":    jt6hControl,
			"gc.kind":         "workflow-finalize",
		},
	}
	for _, id := range blockedBy {
		step = jt6hWithDeps(step, jt6hBlocks(step.ID, id))
	}
	return step
}

// A routed root none of whose live steps is ready work for this session is
// passed over, and the claim moves on to the next routed row.
func TestDoHookClaimSkipsWorkflowRootWithNothingReadyHere(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rootID string
		steps  []beads.Bead
	}{
		{name: "implement closed, only another route's step and the finalizer left", rootID: jt6hLateRoot, steps: []beads.Bead{
			tf857Step("gcd-anchor", jt6hLateRoot, "open", "", jt6hRunOperator),
			jt6hFinalize(jt6hLateRoot, "gcd-anchor"),
		}},
		{name: "implement waiting on a step routed elsewhere", rootID: jt6hEarlyRoot, steps: []beads.Bead{
			tf857Step("gcd-prepare", jt6hEarlyRoot, "open", "", jt6hRunOperator),
			jt6hWithDeps(tf857Step("gcd-implement", jt6hEarlyRoot, "open", "", tf857Pool), jt6hBlocks("gcd-implement", "gcd-prepare")),
			jt6hFinalize(jt6hEarlyRoot, "gcd-implement"),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := beads.Bead{ID: "gcd-next", Status: "open", Type: "task", Metadata: map[string]string{"gc.routed_to": tf857Pool}}
			var claimed []string
			ops := hookClaimOps{
				Runner: func(string, string) (string, error) { return tf857Rows(t, tf857Root(tc.rootID), next), nil },
				ListRootSteps: func(_ context.Context, _ string, _ []string, rootID string) ([]beads.Bead, error) {
					if rootID != tc.rootID {
						return nil, nil
					}
					return tc.steps, nil
				},
				Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
					claimed = append(claimed, beadID)
					return beads.Bead{ID: beadID, Status: "in_progress", Assignee: assignee, Metadata: next.Metadata}, true, nil
				},
			}
			var stdout, stderr bytes.Buffer
			if code := doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), ops, &stdout, &stderr); code != 0 {
				t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
			}
			if got := strings.Join(claimed, ","); got != "gcd-next" {
				t.Fatalf("claimed %q, want only gcd-next; stderr=%s", got, stderr.String())
			}
			if !strings.Contains(stderr.String(), "skipping workflow root "+tc.rootID+": none of its") {
				t.Fatalf("stderr = %q, want the skipped root named", stderr.String())
			}
		})
	}
}

// The first claimant of a fresh workflow root still launches it: its first
// step is ready and routed here, so the root is claimed exactly as before.
func TestDoHookClaimLaunchesAFreshWorkflowRoot(t *testing.T) {
	implement := tf857Step("gcd-implement", jt6hEarlyRoot, "open", "", tf857Pool)
	steps := []beads.Bead{implement, jt6hFinalize(jt6hEarlyRoot, implement.ID)}
	var claimed []string
	ops := hookClaimOps{
		Runner: func(string, string) (string, error) { return tf857Rows(t, tf857Root(jt6hEarlyRoot)), nil },
		ListRootSteps: func(_ context.Context, _ string, _ []string, rootID string) ([]beads.Bead, error) {
			if rootID != jt6hEarlyRoot {
				t.Fatalf("listed steps of %q, want %q", rootID, jt6hEarlyRoot)
			}
			return steps, nil
		},
		Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
			claimed = append(claimed, beadID)
			root := tf857Root(beadID)
			root.Status, root.Assignee = "in_progress", assignee
			return root, true, nil
		},
	}
	var stdout, stderr bytes.Buffer
	if code := doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), ops, &stdout, &stderr); code != 0 {
		t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := strings.Join(claimed, ","); got != jt6hEarlyRoot {
		t.Fatalf("claimed %q, want the root %s; stderr=%s", got, jt6hEarlyRoot, stderr.String())
	}
	if strings.Contains(stderr.String(), "skipping workflow root") {
		t.Fatalf("stderr = %q, want the fresh root offered", stderr.String())
	}
}

// The controller counts a routed root as capacity demand only while one of its
// live steps is ready work for the template; otherwise a seat spawned for it
// could only drain.
func TestDefaultScaleCheckCountsWorkflowRootOnlyWithReadyWorkHere(t *testing.T) {
	store := beads.NewMemStore()
	root, err := store.Create(tf857Root(""))
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	rootDemanded := func() bool {
		t.Helper()
		_, demand, _, errs := defaultScaleCheckCountsAndDemand(nil, []defaultScaleCheckTarget{{template: tf857Pool, storeKey: "rig:gcd", store: store}})
		if len(errs) != 0 {
			t.Fatalf("defaultScaleCheckCountsAndDemand errs = %v", errs)
		}
		return slices.Contains(demand[tf857Pool].WorkBeadIDs, root.ID)
	}
	if _, err := store.Create(tf857Step("", root.ID, "open", "", jt6hRunOperator)); err != nil {
		t.Fatalf("create anchor: %v", err)
	}
	if rootDemanded() {
		t.Fatalf("root whose only live step is a run-operator's counts as %s demand", tf857Pool)
	}
	if _, err := store.Create(tf857Step("", root.ID, "open", "", tf857Pool)); err != nil {
		t.Fatalf("create implement: %v", err)
	}
	if !rootDemanded() {
		t.Fatalf("root with a ready %s step does not count as demand (a launch is demand)", tf857Pool)
	}
}
