package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gastownhall/gascity/internal/session"
)

func TestProbeReadinessRejectsUnknownItemsAsInvalid(t *testing.T) {
	_, err := ProbeReadiness(context.Background(), "claude,unknown", false)
	var invalid *InvalidReadinessItemsError
	if !errors.As(err, &invalid) {
		t.Fatalf("ProbeReadiness error = %v, want *InvalidReadinessItemsError", err)
	}
	if got, want := err.Error(), `unsupported items value "unknown"`; got != want {
		t.Fatalf("error = %q, want the route's original message %q", got, want)
	}
}

func TestProbeReadinessRejectsEmptyItemList(t *testing.T) {
	_, err := ProbeReadiness(context.Background(), " , ", false)
	var invalid *InvalidReadinessItemsError
	if !errors.As(err, &invalid) {
		t.Fatalf("ProbeReadiness error = %v, want *InvalidReadinessItemsError", err)
	}
}

func TestCityPendingBodyMatchesRouteEnvelope(t *testing.T) {
	body := CityPendingBody([]session.CityPendingEntry{{SessionID: "gc-1", RequestID: "req-1", Kind: "approval"}}, []string{"session gc-2: boom"})
	want := ListBody[CityPendingEntry]{
		Items:         []CityPendingEntry{{SessionID: "gc-1", RequestID: "req-1", Kind: "approval"}},
		Total:         1,
		Partial:       true,
		PartialErrors: []string{"session gc-2: boom"},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("CityPendingBody = %#v, want %#v", body, want)
	}
}

func TestCityPendingBodyNothingPendingEncodesEmptyItems(t *testing.T) {
	data, err := json.Marshal(CityPendingBody(nil, nil))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, want := string(data), `{"items":[],"total":0}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestClientCityPendingDecodesRouteBody(t *testing.T) {
	var gotPath string
	c := inProcessCityClient("alpha", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListBody[CityPendingEntry]{ //nolint:errcheck
			Items:         []CityPendingEntry{{SessionID: "gc-1", RequestID: "req-1", Kind: "approval"}},
			Total:         1,
			Partial:       true,
			PartialErrors: []string{"session gc-2: boom"},
		})
	}))

	cr, err := c.CityPending()
	if err != nil {
		t.Fatalf("CityPending: %v", err)
	}
	if gotPath != "/v0/city/alpha/pending" {
		t.Fatalf("path = %q, want /v0/city/alpha/pending", gotPath)
	}
	want := CityPendingBody([]session.CityPendingEntry{{SessionID: "gc-1", RequestID: "req-1", Kind: "approval"}}, []string{"session gc-2: boom"})
	if !reflect.DeepEqual(cr.Body, want) {
		t.Fatalf("body = %#v, want %#v", cr.Body, want)
	}
}

func TestClientStopSessionTurnReturnsResolvedID(t *testing.T) {
	var gotPath, gotMethod string
	c := inProcessCityClient("alpha", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": "gc-42"}) //nolint:errcheck
	}))

	id, err := c.StopSessionTurn("mayor-alias")
	if err != nil {
		t.Fatalf("StopSessionTurn: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v0/city/alpha/session/mayor-alias/stop" {
		t.Fatalf("request = %s %s, want POST /v0/city/alpha/session/mayor-alias/stop", gotMethod, gotPath)
	}
	if id != "gc-42" {
		t.Fatalf("id = %q, want server-resolved gc-42", id)
	}
}

func TestClientStopSessionTurnSurfacesNotFound(t *testing.T) {
	c := inProcessCityClient("alpha", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"status": 404, "title": "Not Found", "detail": "session not found"}) //nolint:errcheck
	}))

	if _, err := c.StopSessionTurn("nope"); err == nil {
		t.Fatal("StopSessionTurn error = nil, want not-found error")
	}
}

// inProcessCityClient is a city-scoped client whose transport serves h
// in-process, so the client's request and decode paths run without a listener.
func inProcessCityClient(cityName string, h http.Handler) *Client {
	return NewCityScopedClientWithHTTPClient("http://supervisor.in-process", cityName,
		&http.Client{Transport: handlerRoundTripper{h: h}})
}

// handlerRoundTripper answers each request by serving it on an http.Handler.
type handlerRoundTripper struct{ h http.Handler }

func (t handlerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}
