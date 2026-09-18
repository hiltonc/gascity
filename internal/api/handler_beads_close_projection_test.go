package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

var (
	closeProjectionCreatedAt = time.Date(2026, 9, 17, 16, 0, 0, 0, time.UTC)
	closeProjectionClosedAt  = time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC)
)

// closeProjectionFields is the set the served Bead schema has to carry for a
// reporting client to measure a lifespan. closed_at is the one that makes the
// measurement possible at all -- updated_at moves on any later write, and
// merge bookkeeping routinely touches a bead after it closes.
var closeProjectionFields = []string{"closed_at", "close_reason", "owner", "created_by"}

// newCloseProjectionState seeds one closed bead carrying all four properties
// and one open bead that carries only the attribution half, which is the shape
// the store reports for each.
func newCloseProjectionState(t *testing.T) *fakeState {
	t.Helper()
	state := newFakeState(t)
	closedAt := closeProjectionClosedAt
	state.stores["myrig"] = beads.NewMemStoreFrom(0, []beads.Bead{
		{
			ID:          "gc-closed",
			Title:       "Closed bead",
			Status:      "closed",
			Type:        "task",
			CreatedAt:   closeProjectionCreatedAt,
			UpdatedAt:   closeProjectionCreatedAt.Add(2 * time.Hour),
			ClosedAt:    &closedAt,
			CloseReason: "shipped in #118",
			Owner:       "me@heyhilton.com",
			CreatedBy:   "gc-mayor",
		},
		{
			ID:        "gc-open",
			Title:     "Open bead",
			Status:    "open",
			Type:      "task",
			CreatedAt: closeProjectionCreatedAt,
			Owner:     "me@heyhilton.com",
			CreatedBy: "gc-mayor",
		},
	}, nil)
	return state
}

// assertClosedBeadDocument checks the four properties on one served bead
// document, which is the unit both routes are asserted against.
func assertClosedBeadDocument(t *testing.T, got map[string]any) {
	t.Helper()
	want := map[string]any{
		"closed_at":    closeProjectionClosedAt.Format(time.RFC3339Nano),
		"close_reason": "shipped in #118",
		"owner":        "me@heyhilton.com",
		"created_by":   "gc-mayor",
	}
	for _, field := range closeProjectionFields {
		if got[field] != want[field] {
			t.Errorf("%s = %#v, want %#v", field, got[field], want[field])
		}
	}
}

// TestBeadGetServesCloseAndAttribution pins the single-bead route. The store
// has always held these four; the served projection dropped them, so a remote
// client -- the only way a Dispatch reporting screen can read this data -- had
// no completion time to compute a lifespan from.
func TestBeadGetServesCloseAndAttribution(t *testing.T) {
	state := newCloseProjectionState(t)
	h := newTestCityHandler(t, state)

	req := httptest.NewRequest("GET", cityURL(state, "/bead/gc-closed"), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	assertClosedBeadDocument(t, got)
}

// TestBeadGetOmitsClosedAtForAnOpenBead pins the negative half: an open bead
// must read as "not closed", not as closed at the zero time. A served
// "0001-01-01T00:00:00Z" is a real date to a client, and every lifespan
// computed against it would be wrong rather than skipped.
func TestBeadGetOmitsClosedAtForAnOpenBead(t *testing.T) {
	state := newCloseProjectionState(t)
	h := newTestCityHandler(t, state)

	req := httptest.NewRequest("GET", cityURL(state, "/bead/gc-open"), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if closedAt, ok := got["closed_at"]; ok {
		t.Errorf("closed_at = %#v, want omitted for an open bead", closedAt)
	}
	if reason, ok := got["close_reason"]; ok {
		t.Errorf("close_reason = %#v, want omitted for an open bead", reason)
	}
	if got["owner"] != "me@heyhilton.com" {
		t.Errorf("owner = %#v, want %q", got["owner"], "me@heyhilton.com")
	}
	if got["created_by"] != "gc-mayor" {
		t.Errorf("created_by = %#v, want %q", got["created_by"], "gc-mayor")
	}
}

// TestBeadListServesCloseAndAttribution covers the route reporting actually
// reads. A screen plotting created-vs-closed per day fetches ranges, so the
// four have to ride the list items and not only the single-bead route.
func TestBeadListServesCloseAndAttribution(t *testing.T) {
	state := newCloseProjectionState(t)
	h := newTestCityHandler(t, state)

	req := httptest.NewRequest("GET", cityURL(state, "/beads?status=closed"), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	var closed map[string]any
	for _, item := range body.Items {
		if item["id"] == "gc-closed" {
			closed = item
			break
		}
	}
	if closed == nil {
		t.Fatalf("gc-closed missing from list items: %v", body.Items)
	}
	assertClosedBeadDocument(t, closed)
}

// TestBeadSchemaDeclaresCloseAndAttribution is the guard against the
// projection silently narrowing again. A generated client can only decode a
// property the published schema declares, so serving it without declaring it
// would leave the four unreadable to exactly the consumer that needs them.
// Both published copies are checked because the docs copy is what a client
// generator is pointed at.
func TestBeadSchemaDeclaresCloseAndAttribution(t *testing.T) {
	for _, specPath := range []string{
		"openapi.json",
		filepath.Join("..", "..", "docs", "reference", "schema", "openapi.json"),
	} {
		data, err := os.ReadFile(specPath)
		if err != nil {
			t.Fatalf("read %s: %v", specPath, err)
		}
		var spec struct {
			Components struct {
				Schemas map[string]struct {
					Properties map[string]json.RawMessage `json:"properties"`
					Required   []string                   `json:"required"`
				} `json:"schemas"`
			} `json:"components"`
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			t.Fatalf("parse %s: %v", specPath, err)
		}
		bead, ok := spec.Components.Schemas["Bead"]
		if !ok {
			t.Fatalf("%s: Bead schema missing", specPath)
		}
		for _, field := range closeProjectionFields {
			if _, ok := bead.Properties[field]; !ok {
				t.Errorf("%s: Bead schema does not declare %q", specPath, field)
			}
		}
		// None of the four may be required: an open bead legitimately carries
		// no close time, and a store that records no attribution carries
		// neither name.
		for _, required := range bead.Required {
			for _, field := range closeProjectionFields {
				if required == field {
					t.Errorf("%s: Bead schema marks %q required; an open bead has no value for it", specPath, field)
				}
			}
		}
	}
}
