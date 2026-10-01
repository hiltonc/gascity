package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// bgc-3q3y, 2026-10-04: bgc-wisp-82lkwea claimed Dispatch's gcd-40pr2a at
// launch, a run-operator took its first step, and from 11:55Z every
// gc hook --claim handed 82lkwea the root as existing_assignment until it
// closed at 15:06Z. Ids are the live ones so a reader can find the events.
const (
	q3yRoot   = "gcd-40pr2a"
	q3yHolder = "bgc-wisp-5fqzb" // tf857ClaimOpts' session
)

// q3yAssignedRoot is a root this session holds, the shape the work query's
// graph-anchor fallback serves.
func q3yAssignedRoot() beads.Bead {
	root := tf857Root(q3yRoot)
	root.Status, root.Assignee = "in_progress", q3yHolder
	return root
}

// q3ySpec is the step-spec sidecar every implement step carries: unrouted,
// open from launch until the finalizer closes it.
func q3ySpec(rootID string) beads.Bead {
	return beads.Bead{
		ID:       rootID + "-spec",
		Status:   "open",
		Type:     "task",
		Metadata: map[string]string{"gc.root_bead_id": rootID, "gc.kind": "spec"},
	}
}

type q3yRecorder struct {
	claimed  []string
	released []string
	events   []hookClaimReleaseRecord
}

func (r *q3yRecorder) ops(t *testing.T, rows []beads.Bead, steps []beads.Bead, listErr error) hookClaimOps {
	t.Helper()
	return hookClaimOps{
		Runner: func(string, string) (string, error) { return tf857Rows(t, rows...), nil },
		ListRootSteps: func(_ context.Context, _ string, _ []string, rootID string) ([]beads.Bead, error) {
			if rootID != q3yRoot {
				t.Fatalf("listed steps of %q, want %q", rootID, q3yRoot)
			}
			return steps, listErr
		},
		Claim: func(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
			r.claimed = append(r.claimed, beadID)
			for _, b := range append(append([]beads.Bead{}, rows...), steps...) {
				if b.ID == beadID {
					b.Status, b.Assignee = "in_progress", assignee
					return b, true, nil
				}
			}
			t.Fatalf("claimed unknown bead %q", beadID)
			return beads.Bead{}, false, nil
		},
		ReadWorkMeta: func(_ context.Context, _ string, _ []string, beadID, _ string) (beads.Bead, error) {
			if beadID == q3yRoot {
				return q3yAssignedRoot(), nil
			}
			return beads.Bead{ID: beadID, Status: "in_progress", Assignee: q3yHolder}, nil
		},
		Release: func(_ context.Context, _ string, _ []string, beadID, assignee string) (bool, error) {
			r.released = append(r.released, beadID+"="+assignee)
			return true, nil
		},
		EmitClaimReleased: func(rec hookClaimReleaseRecord) { r.events = append(r.events, rec) },
	}
}

func q3yResult(t *testing.T, stdout *bytes.Buffer) hookClaimJSONResult {
	t.Helper()
	var result hookClaimJSONResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v\nraw: %s", err, stdout.String())
	}
	return result
}

// The 11:55Z shape: the root's only open steps belong to another route, the
// control dispatcher, or are sidecars. The root is given back and the session
// is not handed it.
func TestDoHookClaimReleasesAnAssignedWorkflowRootWithNothingReadyHere(t *testing.T) {
	steps := []beads.Bead{
		tf857Step("gcd-wr8aep", q3yRoot, "in_progress", "bgc-wisp-lmob1iq", jt6hRunOperator),
		q3ySpec(q3yRoot),
		jt6hFinalize(q3yRoot, "gcd-wr8aep"),
	}
	next := beads.Bead{ID: "gcd-next", Status: "open", Type: "task", Metadata: map[string]string{"gc.routed_to": tf857Pool}}
	for _, tc := range []struct {
		name        string
		rows        []beads.Bead
		wantClaimed string
		wantAction  string
	}{
		{name: "the root alone drains", rows: []beads.Bead{q3yAssignedRoot()}, wantAction: "drain"},
		{name: "other routed work is claimed instead", rows: []beads.Bead{q3yAssignedRoot(), next}, wantClaimed: "gcd-next", wantAction: "work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rec q3yRecorder
			var stdout, stderr bytes.Buffer
			doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), rec.ops(t, tc.rows, steps, nil), &stdout, &stderr)
			if got, want := strings.Join(rec.released, ","), q3yRoot+"="+q3yHolder; got != want {
				t.Fatalf("released %q, want %q; stderr=%s", got, want, stderr.String())
			}
			if len(rec.events) != 1 || rec.events[0].Reason != hookClaimReleaseReasonWorkflowRoot {
				t.Fatalf("release events = %+v, want one %s", rec.events, hookClaimReleaseReasonWorkflowRoot)
			}
			if got := strings.Join(rec.claimed, ","); got != tc.wantClaimed {
				t.Fatalf("claimed %q, want %q", got, tc.wantClaimed)
			}
			result := q3yResult(t, &stdout)
			if result.Action != tc.wantAction || result.BeadID == q3yRoot {
				t.Fatalf("result = %+v, want action=%s and never the root", result, tc.wantAction)
			}
			if !strings.Contains(stderr.String(), "released workflow root "+q3yRoot) {
				t.Fatalf("stderr = %q, want the release reported", stderr.String())
			}
		})
	}
}

// A root whose next step is ready and routed here hands the session that step,
// and the session keeps the root while it runs it.
func TestDoHookClaimServesTheReadyStepOfAnAssignedWorkflowRoot(t *testing.T) {
	implement := tf857Step("gcd-gfwi92", q3yRoot, "open", "", tf857Pool)
	steps := []beads.Bead{implement, q3ySpec(q3yRoot), jt6hFinalize(q3yRoot, implement.ID)}
	var rec q3yRecorder
	var stdout, stderr bytes.Buffer
	if code := doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), rec.ops(t, []beads.Bead{q3yAssignedRoot()}, steps, nil), &stdout, &stderr); code != 0 {
		t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := strings.Join(rec.claimed, ","); got != implement.ID {
		t.Fatalf("claimed %q, want only the step %s; stderr=%s", got, implement.ID, stderr.String())
	}
	if len(rec.released) != 0 {
		t.Fatalf("released %v, want the root kept while its step runs", rec.released)
	}
	result := q3yResult(t, &stdout)
	if result.Action != "work" || result.BeadID != implement.ID || result.RootBeadID != q3yRoot {
		t.Fatalf("result = %+v, want the step %s under root %s", result, implement.ID, q3yRoot)
	}
}

// A step another session wins first leaves the root with nothing for this
// session, so it is given back rather than served on the next claim.
func TestDoHookClaimReleasesAnAssignedWorkflowRootWhoseStepWasLost(t *testing.T) {
	implement := tf857Step("gcd-gfwi92", q3yRoot, "open", "", tf857Pool)
	var rec q3yRecorder
	ops := rec.ops(t, []beads.Bead{q3yAssignedRoot()}, []beads.Bead{implement}, nil)
	ops.Claim = func(_ context.Context, _ string, _ []string, beadID, _ string) (beads.Bead, bool, error) {
		rec.claimed = append(rec.claimed, beadID)
		return beads.Bead{ID: beadID, Status: "in_progress", Assignee: "bgc-wisp-iiu2gpm"}, false, nil
	}
	ops.EmitClaimRejected = func(string, string, string) {}
	var stdout, stderr bytes.Buffer
	doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), ops, &stdout, &stderr)
	if got, want := strings.Join(rec.released, ","), q3yRoot+"="+q3yHolder; got != want {
		t.Fatalf("released %q, want %q; stderr=%s", got, want, stderr.String())
	}
	if result := q3yResult(t, &stdout); result.Action != "drain" {
		t.Fatalf("result = %+v, want a drain", result)
	}
}

// The root is still served while it is the work: a root-only molecule has no
// steps, and a root whose steps cannot be read fails open as before.
func TestDoHookClaimStillServesAnAssignedWorkflowRootThatIsTheWork(t *testing.T) {
	for _, tc := range []struct {
		name    string
		steps   []beads.Bead
		listErr error
		warn    string
	}{
		{name: "no live steps", steps: []beads.Bead{tf857Step("gcd-done", q3yRoot, "closed", "", tf857Pool)}},
		{name: "steps unreadable fails open", listErr: errors.New("store down"), warn: "serving it unchecked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rec q3yRecorder
			var stdout, stderr bytes.Buffer
			if code := doHookClaim("bd ready --json", "/tmp/work", tf857ClaimOpts(), rec.ops(t, []beads.Bead{q3yAssignedRoot()}, tc.steps, tc.listErr), &stdout, &stderr); code != 0 {
				t.Fatalf("doHookClaim = %d, want 0; stderr=%s", code, stderr.String())
			}
			if len(rec.claimed) != 0 || len(rec.released) != 0 {
				t.Fatalf("claimed %v released %v, want the root served untouched", rec.claimed, rec.released)
			}
			result := q3yResult(t, &stdout)
			if result.Action != "work" || result.Reason != "existing_assignment" || result.BeadID != q3yRoot {
				t.Fatalf("result = %+v, want the root as existing_assignment", result)
			}
			if tc.warn != "" && !strings.Contains(stderr.String(), tc.warn) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.warn)
			}
		})
	}
}
