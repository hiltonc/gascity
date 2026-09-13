package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
)

// snapshotFake is a runtime.Fake that also keeps a fleet snapshot, which
// runtime.Fake deliberately does not. Without it no internal/api test can
// reach the snapshot branch in lastActivityOf/attachedOf: the type assertion
// on runtime.SessionSnapshotProvider fails and every list falls through to
// the per-session probes, leaving the whole point of the change — one tmux
// fork for the fleet rather than one per agent — unproven at the handler.
type snapshotFake struct {
	*runtime.Fake
	known    bool
	attached map[string]bool
	activity map[string]time.Time
}

var _ runtime.SessionSnapshotProvider = (*snapshotFake)(nil)

func newSnapshotFake(base *runtime.Fake, known bool) *snapshotFake {
	return &snapshotFake{
		Fake:     base,
		known:    known,
		attached: map[string]bool{},
		activity: map[string]time.Time{},
	}
}

func (f *snapshotFake) SnapshotAttached(name string) (bool, bool) {
	if !f.known {
		return false, false
	}
	return f.attached[name], true
}

func (f *snapshotFake) SnapshotLastActivity(name string) (time.Time, bool) {
	if !f.known {
		return time.Time{}, false
	}
	return f.activity[name], true
}

// probeCalls counts the per-session runtime probes the list made, by method.
// These are the forks the snapshot exists to replace.
func probeCalls(sp *runtime.Fake) map[string]int {
	calls := map[string]int{}
	for _, call := range sp.Calls {
		switch call.Method {
		case "GetLastActivity", "IsAttached":
			calls[call.Method]++
		}
	}
	return calls
}

func agentsBody(t *testing.T, h http.Handler, state *fakeState) ListBody[agentResponse] {
	t.Helper()
	req := httptest.NewRequest("GET", cityURL(state, "/agents"), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body ListBody[agentResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v: %s", err, rec.Body.String())
	}
	return body
}

func agentByNameIn(t *testing.T, body ListBody[agentResponse], name string) agentResponse {
	t.Helper()
	for _, a := range body.Items {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("agent %q not in the list; got %d agents", name, len(body.Items))
	return agentResponse{}
}

// The headline claim: when the provider keeps a fleet snapshot, a list of N
// running agents costs zero per-session probes rather than 2N, and the values
// it reports come from that snapshot. A refactor that dropped the interface
// assertion or renamed a snapshot method would restore the per-agent forks
// this change exists to remove, so this is the proof that owns that risk.
func TestAgentListReadsAttachAndActivityFromTheFleetSnapshot(t *testing.T) {
	state := newFakeState(t)
	state.cfg.Agents = append(state.cfg.Agents, config.Agent{Name: "second", Dir: "myrig", Provider: "test-agent", MaxActiveSessions: intPtr(1)})
	state.sp.Start(context.Background(), "myrig--worker", runtime.Config{}) //nolint:errcheck
	state.sp.Start(context.Background(), "myrig--second", runtime.Config{}) //nolint:errcheck

	at := time.Unix(1700000900, 0).UTC()
	snap := newSnapshotFake(state.sp, true)
	snap.attached["myrig--worker"] = true
	snap.activity["myrig--worker"] = at
	state.sessionProvider = snap

	srv := New(state)
	h := newTestCityHandlerWith(t, state, srv)
	body := agentsBody(t, h, state)

	if calls := probeCalls(state.sp); len(calls) != 0 {
		t.Errorf("per-session probes = %#v, want none: the snapshot answered for the whole fleet", calls)
	}

	worker := agentByNameIn(t, body, "myrig/worker")
	if worker.Session == nil {
		t.Fatal("worker has no session info")
	}
	if !worker.Session.Attached {
		t.Errorf("worker attached = false, want true from the snapshot")
	}
	if worker.Session.LastActivity == nil || !worker.Session.LastActivity.Equal(at) {
		t.Errorf("worker last activity = %v, want %v from the snapshot", worker.Session.LastActivity, at)
	}

	second := agentByNameIn(t, body, "myrig/second")
	if second.Session == nil {
		t.Fatal("second has no session info")
	}
	if second.Session.Attached {
		t.Errorf("second attached = true, want false: the snapshot reported no client")
	}
	if second.Session.LastActivity != nil {
		t.Errorf("second last activity = %v, want nil: the snapshot has no timestamp for it", second.Session.LastActivity)
	}
}

// When the window listing was unavailable the snapshot answers "not known",
// and the handler must fall back to the per-session probes rather than report
// a stopped, never-attached fleet. Nothing regresses when tmux is slow.
func TestAgentListFallsBackToProbesWhenTheSnapshotCannotAnswer(t *testing.T) {
	state := newFakeState(t)
	state.sp.Start(context.Background(), "myrig--worker", runtime.Config{}) //nolint:errcheck

	at := time.Unix(1700000900, 0).UTC()
	state.sp.SetActivity("myrig--worker", at)
	state.sp.Attached["myrig--worker"] = true

	snap := newSnapshotFake(state.sp, false)
	// Values the snapshot would have returned if it could answer; it cannot,
	// so none of them may reach the response.
	snap.attached["myrig--worker"] = false
	state.sessionProvider = snap

	srv := New(state)
	h := newTestCityHandlerWith(t, state, srv)
	body := agentsBody(t, h, state)

	calls := probeCalls(state.sp)
	if calls["GetLastActivity"] != 1 {
		t.Errorf("GetLastActivity calls = %d, want 1 for the one running agent; calls=%#v", calls["GetLastActivity"], state.sp.Calls)
	}
	if calls["IsAttached"] != 1 {
		t.Errorf("IsAttached calls = %d, want 1 for the one running agent; calls=%#v", calls["IsAttached"], state.sp.Calls)
	}

	worker := agentByNameIn(t, body, "myrig/worker")
	if worker.Session == nil {
		t.Fatal("worker has no session info")
	}
	if !worker.Session.Attached {
		t.Errorf("worker attached = false, want true from the per-session probe")
	}
	if worker.Session.LastActivity == nil || !worker.Session.LastActivity.Equal(at) {
		t.Errorf("worker last activity = %v, want %v from the per-session probe", worker.Session.LastActivity, at)
	}
}
