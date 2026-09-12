package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/runtime"
)

// metaReads counts the session-environment reads the list made, by session.
func metaReads(sp *runtime.Fake) map[string]int {
	reads := map[string]int{}
	for _, call := range sp.Calls {
		if call.Method == "GetMeta" {
			reads[call.Name]++
		}
	}
	return reads
}

func getAgents(t *testing.T, h http.Handler, state *fakeState) {
	t.Helper()
	req := httptest.NewRequest("GET", cityURL(state, "/agents"), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// The suspended flag lives in the tmux session environment, so a stopped
// agent cannot carry it and reading it only costs a fork that fails. On a
// fleet where most agents are stopped, those forks were most of GET /agents.
func TestAgentListReadsSessionEnvOnlyForRunningAgents(t *testing.T) {
	state := newFakeState(t)
	state.cfg.Agents = append(state.cfg.Agents, config.Agent{Name: "idle", Dir: "myrig", Provider: "test-agent"})
	state.sp.Start(context.Background(), "myrig--worker", runtime.Config{}) //nolint:errcheck
	srv := New(state)
	h := newTestCityHandlerWith(t, state, srv)

	getAgents(t, h, state)

	reads := metaReads(state.sp)
	if reads["myrig--worker"] == 0 {
		t.Errorf("no env read for the running agent; calls=%#v", state.sp.Calls)
	}
	for session, n := range reads {
		if strings.Contains(session, "idle") {
			t.Errorf("%d env read(s) for the stopped agent %q; a stopped session cannot carry the flag", n, session)
		}
	}
}

// The response cache is keyed on a wall-clock bucket, not the event index:
// on a busy city the index advances every tick, so an index-keyed entry never
// hit and every poll rebuilt the list, one tmux fork per running agent.
func TestAgentListServesTheCachedListAcrossEventChurn(t *testing.T) {
	prev := timeBucketResponseCacheTTL
	timeBucketResponseCacheTTL = time.Hour
	t.Cleanup(func() { timeBucketResponseCacheTTL = prev })

	state := newFakeState(t)
	state.sp.Start(context.Background(), "myrig--worker", runtime.Config{}) //nolint:errcheck
	srv := New(state)
	h := newTestCityHandlerWith(t, state, srv)

	getAgents(t, h, state)
	before := metaReads(state.sp)["myrig--worker"]
	if before == 0 {
		t.Fatalf("first list made no env read; the fixture is not exercising the probe")
	}

	fake, ok := state.eventProv.(*events.Fake)
	if !ok {
		t.Fatalf("fixture event provider is %T, want *events.Fake", state.eventProv)
	}
	fake.Record(events.Event{Type: "test.churn", Ts: time.Now()})
	getAgents(t, h, state)

	if after := metaReads(state.sp)["myrig--worker"]; after != before {
		t.Errorf("env reads went %d -> %d across an event: the second list was rebuilt instead of served from the bucket cache", before, after)
	}
}
