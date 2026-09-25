package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/api/dashboardbff"
	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
)

// The dashboard's runs/summary is a second view of the same event-log
// projection /runs reads, so a root whose close the log missed (gcd-thjlcv)
// must leave both once its store says it closed. This drives the production
// wiring: the supervisor mux, the dashboard plane as its run source, and the
// real handlers on both surfaces.
func TestRunsSummaryAndRunsListAgreeOnARootTheLogNeverSawClose(t *testing.T) {
	fs := newFakeState(t)
	old := time.Now().Add(-2 * time.Hour)
	started := map[string]string{"gc.step_id": "implement"}
	passChild := runChildBead("mr-pass.1", "mr-pass", "in_progress", started)
	passChild.UpdatedAt = old
	liveChild := runChildBead("mr-live.1", "mr-live", "in_progress", started)
	liveChild.UpdatedAt = old
	writeRunEventLog(t, fs.cityPath,
		beadCreatedEvent(1, staleRunRoot("mr-pass", "in_progress", "", old)),
		beadCreatedEvent(2, passChild),
		beadCreatedEvent(3, staleRunRoot("mr-live", "in_progress", "", old)),
		beadCreatedEvent(4, liveChild),
	)
	fs.stores["myrig"] = beads.NewMemStoreFrom(0, []beads.Bead{
		staleRunRoot("mr-pass", "closed", beadmeta.OutcomePass, old.Add(time.Minute)),
		staleRunRoot("mr-live", "in_progress", "", old),
	}, nil)

	mux := NewSupervisorMux(&singleStateResolver{state: fs}, nil, false, "test", "", time.Now())
	mux.WithAnyHostAllowed()
	plane := dashboardbff.New(dashboardbff.Deps{
		Resolver: cityPathResolver{fs.CityName(): fs.CityPath()},
	})
	plane.Start(t.Context())
	t.Cleanup(plane.Stop)
	mux.WithRunCensusSource(plane).WithAPIPlane(plane.Handler())
	handler := mux.Handler()

	var summary struct {
		TotalActive int `json:"totalActive"`
		Lanes       []struct {
			ID string `json:"id"`
		} `json:"lanes"`
		Census struct {
			Data struct {
				TotalInFlight int `json:"totalInFlight"`
			} `json:"data"`
		} `json:"census"`
	}
	getJSON(t, handler, "/api/city/test-city/runs/summary", &summary)
	if len(summary.Lanes) != 1 || summary.Lanes[0].ID != "mr-live" {
		t.Fatalf("runs/summary lanes = %+v, want only mr-live", summary.Lanes)
	}
	if summary.TotalActive != len(summary.Lanes) || summary.Census.Data.TotalInFlight != len(summary.Lanes) {
		t.Fatalf("runs/summary totalActive=%d census in-flight=%d lanes=%d; want all three to agree",
			summary.TotalActive, summary.Census.Data.TotalInFlight, len(summary.Lanes))
	}

	var runs struct {
		Runs []struct {
			RunID  string    `json:"run_id"`
			Status RunStatus `json:"status"`
		} `json:"runs"`
	}
	getJSON(t, handler, "/v0/city/test-city/runs", &runs)
	statuses := make(map[string]RunStatus, len(runs.Runs))
	for _, run := range runs.Runs {
		statuses[run.RunID] = run.Status
	}
	if statuses["mr-pass"] != RunStatusCompleted || statuses["mr-live"] != RunStatusActive {
		t.Fatalf("/runs statuses = %v, want mr-pass completed and mr-live active", statuses)
	}
}

func getJSON(t *testing.T, handler http.Handler, path string, into any) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d; body=%s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("decode GET %s: %v; body=%s", path, err, rec.Body.String())
	}
}

// cityPathResolver resolves registered city names to their roots for the
// dashboard plane.
type cityPathResolver map[string]string

func (r cityPathResolver) CityPath(name string) (string, bool) {
	path, ok := r[name]
	return path, ok
}
