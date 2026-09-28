package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/api"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
	"github.com/gastownhall/gascity/internal/session"
)

// sessionCLIRouteTestRuntimeName is the runtime session name of the one
// session setupSessionCLIRouteTestCity creates.
const sessionCLIRouteTestRuntimeName = "runtime-session"

// setupSessionCLIRouteTestCity builds a file-backed city whose session
// provider is fake, with one active session bead whose runtime is
// sessionCLIRouteTestRuntimeName. It forces the local path by disabling the
// supervisor API seams.
func setupSessionCLIRouteTestCity(t *testing.T) (*runtime.Fake, string) {
	t.Helper()
	runtimeName := sessionCLIRouteTestRuntimeName
	clearGCEnv(t)
	clearInheritedCityRoutingEnv(t)
	t.Setenv("GC_BEADS", "file")
	t.Setenv("GC_SESSION", "fake")

	cityDir := t.TempDir()
	t.Setenv("GC_CITY", cityDir)
	writeNamedSessionCityTOML(t, cityDir)

	fake := runtime.NewFake()
	oldBuild := buildSessionProviderByName
	buildSessionProviderByName = func(*config.City, string, config.SessionConfig, string, string) (runtime.Provider, error) {
		return fake, nil
	}
	t.Cleanup(func() { buildSessionProviderByName = oldBuild })

	oldPending := sessionPendingAPIClient
	sessionPendingAPIClient = func(string) (*api.Client, string) { return nil, "controller-down" }
	t.Cleanup(func() { sessionPendingAPIClient = oldPending })
	oldStop := sessionStopTurnAPIClient
	sessionStopTurnAPIClient = func(string) *api.Client { return nil }
	t.Cleanup(func() { sessionStopTurnAPIClient = oldStop })

	store, err := openCityStoreAt(cityDir)
	if err != nil {
		t.Fatalf("openCityStoreAt(%q): %v", cityDir, err)
	}
	b, err := store.Create(beads.Bead{
		Title:  "cli route session",
		Type:   session.BeadType,
		Labels: []string{session.LabelSession},
		Metadata: map[string]string{
			"session_name": runtimeName,
			"template":     "worker",
			"state":        "active",
			"work_dir":     cityDir,
		},
	})
	if err != nil {
		t.Fatalf("store.Create(session): %v", err)
	}
	if err := fake.Start(context.Background(), runtimeName, runtime.Config{}); err != nil {
		t.Fatalf("fake.Start(%q): %v", runtimeName, err)
	}
	return fake, b.ID
}

func decodeCityPendingJSON(t *testing.T, stdout []byte) (api.ListBody[api.CityPendingEntry], map[string]json.RawMessage) {
	t.Helper()
	var body api.ListBody[api.CityPendingEntry]
	if err := json.Unmarshal(stdout, &body); err != nil {
		t.Fatalf("stdout is not the pending route body: %v\n%s", err, stdout)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout, &raw); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, stdout)
	}
	return body, raw
}

func TestCmdSessionPendingJSONListsSessionAwaitingDecision(t *testing.T) {
	fake, sessionID := setupSessionCLIRouteTestCity(t)
	fake.SetPendingInteraction(sessionCLIRouteTestRuntimeName, &runtime.PendingInteraction{RequestID: "req-1", Kind: "approval", Prompt: "approve?"})

	var stdout, stderr bytes.Buffer
	if code := cmdSessionPending(true, &stdout, &stderr); code != 0 {
		t.Fatalf("cmdSessionPending(--json) = %d, want 0; stderr=%s", code, stderr.String())
	}
	body, raw := decodeCityPendingJSON(t, stdout.Bytes())
	want := []api.CityPendingEntry{{SessionID: sessionID, RequestID: "req-1", Kind: "approval"}}
	if !reflect.DeepEqual(body.Items, want) || body.Total != 1 || body.Partial {
		t.Fatalf("body = %#v, want items %#v total 1 not partial", body, want)
	}
	if string(raw["ok"]) != "true" {
		t.Fatalf("ok = %s, want true (CLI JSON convention)", raw["ok"])
	}
}

func TestCmdSessionPendingJSONNothingPending(t *testing.T) {
	setupSessionCLIRouteTestCity(t)

	var stdout, stderr bytes.Buffer
	if code := cmdSessionPending(true, &stdout, &stderr); code != 0 {
		t.Fatalf("cmdSessionPending(--json) = %d, want 0; stderr=%s", code, stderr.String())
	}
	body, raw := decodeCityPendingJSON(t, stdout.Bytes())
	if body.Items == nil || len(body.Items) != 0 || body.Total != 0 {
		t.Fatalf("body = %#v, want empty items array and total 0", body)
	}
	if string(raw["items"]) != "[]" {
		t.Fatalf("items = %s, want [] (never null)", raw["items"])
	}
	if _, ok := raw["partial"]; ok {
		t.Fatalf("partial present with nothing failed: %s", stdout.String())
	}
}

func TestCmdSessionPendingTextNothingPending(t *testing.T) {
	setupSessionCLIRouteTestCity(t)

	var stdout, stderr bytes.Buffer
	if code := cmdSessionPending(false, &stdout, &stderr); code != 0 {
		t.Fatalf("cmdSessionPending = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "No sessions awaiting a decision.\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestCmdSessionPendingTextListsSession(t *testing.T) {
	fake, sessionID := setupSessionCLIRouteTestCity(t)
	fake.SetPendingInteraction(sessionCLIRouteTestRuntimeName, &runtime.PendingInteraction{RequestID: "req-1", Kind: "approval"})

	var stdout, stderr bytes.Buffer
	if code := cmdSessionPending(false, &stdout, &stderr); code != 0 {
		t.Fatalf("cmdSessionPending = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"SESSION", sessionID, "approval", "req-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestRouteSessionPendingAPIEmitsRouteBody(t *testing.T) {
	routeBody := api.ListBody[api.CityPendingEntry]{
		Items:         []api.CityPendingEntry{{SessionID: "gc-1", RequestID: "req-1", Kind: "approval"}},
		Total:         1,
		Partial:       true,
		PartialErrors: []string{"session gc-2: boom"},
	}
	c := inProcessAPIClient("test-city", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/city/test-city/pending" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(routeBody) //nolint:errcheck
	}))

	var stdout, stderr bytes.Buffer
	if code := routeSessionPending(c, "", true, &stdout, &stderr); code != 0 {
		t.Fatalf("routeSessionPending = %d, want 0; stderr=%s", code, stderr.String())
	}
	body, _ := decodeCityPendingJSON(t, stdout.Bytes())
	if !reflect.DeepEqual(body, routeBody) {
		t.Fatalf("CLI body = %#v, want the route body %#v", body, routeBody)
	}
}

// ---------------------------------------------------------------------------
// Read-path routing matrix for `gc session pending` (enforced by
// scripts/check-routed-test-rows.sh). The API leg serves session "gc-api"; the
// local leg probes the fake runtime, whose one session is awaiting approval, so
// stdout names which leg answered.
//
//   api-happy-path       /pending 200                   route=api, exit 0
//   api-cache-not-live   503 cache_not_live             fallback, exit 0
//   api-500-fallback     generic 500                    fallback (conn-refused)
//   api-404-error        404 problem+json               no fallback, exit 1
//   controller-down      apiClient returns nil          fallback (controller-down)
//   escape-hatch         GC_NO_API truthy               fallback (escape-hatch)
//
// `gc session stop-turn` and `gc status readiness` carry no rows: stop-turn is
// a write that routes API-first to reach in-process runtimes, not a read with a
// store fallback, and readiness probes the host in-process with no API client.
// ---------------------------------------------------------------------------

// okSessionPendingHandler serves the pending route with one API-only session.
func okSessionPendingHandler(_ *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/city/test-city/pending" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.ListBody[api.CityPendingEntry]{ //nolint:errcheck
			Items: []api.CityPendingEntry{{SessionID: "gc-api", RequestID: "req-api", Kind: "approval"}},
			Total: 1,
		})
	})
}

func TestRouteSessionPending_SixRowMatrix(t *testing.T) {
	tests := []struct {
		name         string
		handler      func(*testing.T) http.Handler
		useNilClient bool
		nilReason    string
		wantExit     int
		wantRoute    string
		wantReason   string
		wantStderr   string
		wantLocal    bool
	}{
		{name: "api-happy-path", handler: okSessionPendingHandler, wantExit: 0, wantRoute: "api"},
		{name: "api-cache-not-live", handler: problemHandler(http.StatusServiceUnavailable, "cache_not_live: priming"), wantExit: 0, wantRoute: "fallback", wantReason: "cache-not-live", wantLocal: true},
		{name: "api-500-fallback", handler: problemHandler(http.StatusInternalServerError, "internal: explode"), wantExit: 0, wantRoute: "fallback", wantReason: "conn-refused", wantLocal: true},
		{name: "api-404-error", handler: problemHandler(http.StatusNotFound, "not_found: no such city"), wantExit: 1, wantRoute: "api", wantReason: "error", wantStderr: "not_found"},
		{name: "controller-down", useNilClient: true, nilReason: "controller-down", wantExit: 0, wantRoute: "fallback", wantReason: "controller-down", wantLocal: true},
		{name: "escape-hatch", useNilClient: true, nilReason: "escape-hatch", wantExit: 0, wantRoute: "fallback", wantReason: "escape-hatch", wantLocal: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake, localID := setupSessionCLIRouteTestCity(t)
			fake.SetPendingInteraction(sessionCLIRouteTestRuntimeName, &runtime.PendingInteraction{RequestID: "req-local", Kind: "approval"})
			t.Setenv("GC_DEBUG", "1")

			var c *api.Client
			if !tc.useNilClient {
				c = inProcessAPIClient("test-city", tc.handler(t))
			}

			var stdout, stderr bytes.Buffer
			code := routeSessionPending(c, tc.nilReason, true, &stdout, &stderr)

			if code != tc.wantExit {
				t.Fatalf("exit = %d, want %d; stderr=%q stdout=%q", code, tc.wantExit, stderr.String(), stdout.String())
			}
			want := "route=" + tc.wantRoute
			if tc.wantReason != "" {
				want += " reason=" + tc.wantReason
			}
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr.String())
			}
			if n := strings.Count(stderr.String(), "route="); n != 1 {
				t.Errorf("route=... lines = %d, want 1:\n%s", n, stderr.String())
			}
			if tc.wantStderr != "" && !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr missing %q:\n%s", tc.wantStderr, stderr.String())
			}
			if tc.wantExit != 0 {
				if stdout.Len() != 0 {
					t.Errorf("stdout = %q, want empty on error", stdout.String())
				}
				return
			}
			body, _ := decodeCityPendingJSON(t, stdout.Bytes())
			wantID := "gc-api"
			if tc.wantLocal {
				wantID = localID
			}
			if len(body.Items) != 1 || body.Items[0].SessionID != wantID {
				t.Errorf("items = %#v, want one entry for %s", body.Items, wantID)
			}
		})
	}
}

func TestCmdSessionStopTurnInterruptsLocallyAndEmitsRouteBody(t *testing.T) {
	fake, sessionID := setupSessionCLIRouteTestCity(t)

	var stdout, stderr bytes.Buffer
	if code := cmdSessionStopTurn(sessionID, true, &stdout, &stderr); code != 0 {
		t.Fatalf("cmdSessionStopTurn(--json) = %d, want 0; stderr=%s", code, stderr.String())
	}
	var got sessionStopTurnJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not the stop route body: %v\n%s", err, stdout.String())
	}
	if got != (sessionStopTurnJSON{Status: "ok", ID: sessionID}) {
		t.Fatalf("body = %#v, want status ok id %s", got, sessionID)
	}
	interrupted := false
	for _, call := range fake.Calls {
		if call.Method == "Interrupt" && call.Name == sessionCLIRouteTestRuntimeName {
			interrupted = true
		}
	}
	if !interrupted {
		t.Fatalf("fake provider calls = %#v, want an Interrupt of runtime-session", fake.Calls)
	}
}

func TestCmdSessionStopTurnUnknownSessionFails(t *testing.T) {
	setupSessionCLIRouteTestCity(t)

	var stdout, stderr bytes.Buffer
	if code := cmdSessionStopTurn("no-such-session", false, &stdout, &stderr); code == 0 {
		t.Fatalf("cmdSessionStopTurn = 0, want failure; stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "gc session stop-turn:") {
		t.Fatalf("stderr = %q, want a stop-turn error", stderr.String())
	}
}

func TestCmdSessionStopTurnRoutesThroughAPI(t *testing.T) {
	setupSessionCLIRouteTestCity(t)
	var gotPath string
	c := inProcessAPIClient("test-city", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": "gc-42"}) //nolint:errcheck
	}))
	sessionStopTurnAPIClient = func(string) *api.Client { return c }

	var stdout, stderr bytes.Buffer
	if code := cmdSessionStopTurn("mayor", false, &stdout, &stderr); code != 0 {
		t.Fatalf("cmdSessionStopTurn = %d, want 0; stderr=%s", code, stderr.String())
	}
	if gotPath != "/v0/city/test-city/session/mayor/stop" {
		t.Fatalf("path = %q, want the stop route", gotPath)
	}
	if got := stdout.String(); got != "Stopped turn for gc-42\n" {
		t.Fatalf("stdout = %q, want the server-resolved id", got)
	}
}

func TestCmdStatusReadinessJSONEmitsRouteBody(t *testing.T) {
	want := api.ReadinessResponse{Items: map[string]api.ReadinessItem{
		"claude":     {Name: "claude", Kind: api.ProbeKindProvider, DisplayName: "Claude Code", Status: api.ProbeStatusConfigured},
		"github_cli": {Name: "github_cli", Kind: api.ProbeKindTool, DisplayName: "GitHub CLI", Status: api.ProbeStatusNeedsAuth, Detail: "run gh auth login"},
	}}
	var gotItems string
	var gotFresh bool
	old := statusReadinessProbe
	statusReadinessProbe = func(_ context.Context, items string, fresh bool) (api.ReadinessResponse, error) {
		gotItems, gotFresh = items, fresh
		return want, nil
	}
	t.Cleanup(func() { statusReadinessProbe = old })

	var stdout, stderr bytes.Buffer
	if code := cmdStatusReadiness(context.Background(), "claude,github_cli", true, true, &stdout, &stderr); code != 0 {
		t.Fatalf("cmdStatusReadiness = %d, want 0; stderr=%s", code, stderr.String())
	}
	if gotItems != "claude,github_cli" || !gotFresh {
		t.Fatalf("probe called with items=%q fresh=%v", gotItems, gotFresh)
	}
	var got api.ReadinessResponse
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not the readiness route body: %v\n%s", err, stdout.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v, want %#v", got, want)
	}
}

func TestCmdStatusReadinessText(t *testing.T) {
	old := statusReadinessProbe
	statusReadinessProbe = func(context.Context, string, bool) (api.ReadinessResponse, error) {
		return api.ReadinessResponse{Items: map[string]api.ReadinessItem{
			"codex": {Name: "codex", Kind: api.ProbeKindProvider, DisplayName: "Codex", Status: api.ProbeStatusNotInstalled},
		}}, nil
	}
	t.Cleanup(func() { statusReadinessProbe = old })

	var stdout, stderr bytes.Buffer
	if code := cmdStatusReadiness(context.Background(), "", false, false, &stdout, &stderr); code != 0 {
		t.Fatalf("cmdStatusReadiness = %d, want 0; stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"ITEM", "Codex", "provider", "not_installed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestCmdStatusReadinessRejectsUnknownItems(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cmdStatusReadiness(context.Background(), "claude,unknown", false, true, &stdout, &stderr); code != 1 {
		t.Fatalf("cmdStatusReadiness = %d, want 1", code)
	}
	var got cliJSONErrorOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not a JSON error: %v\n%s", err, stdout.String())
	}
	if got.Error.Code != "invalid_readiness_items" || !strings.Contains(got.Error.Message, `"unknown"`) {
		t.Fatalf("error = %#v, want invalid_readiness_items naming the item", got.Error)
	}
}

func TestCmdStatusReadinessProbeFailure(t *testing.T) {
	old := statusReadinessProbe
	statusReadinessProbe = func(context.Context, string, bool) (api.ReadinessResponse, error) {
		return api.ReadinessResponse{}, errors.New("workspace home unavailable")
	}
	t.Cleanup(func() { statusReadinessProbe = old })

	var stdout, stderr bytes.Buffer
	if code := cmdStatusReadiness(context.Background(), "", false, false, &stdout, &stderr); code != 1 {
		t.Fatalf("cmdStatusReadiness = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "workspace home unavailable") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// inProcessAPIClient is a city-scoped API client whose transport serves h
// in-process, so a route test exercises the real client without a listener.
func inProcessAPIClient(cityName string, h http.Handler) *api.Client {
	return api.NewCityScopedClientWithHTTPClient("http://supervisor.in-process", cityName,
		&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			return rec.Result(), nil
		})})
}
