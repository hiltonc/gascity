package api

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/events"
)

// captureEventScanLog runs fn with the standard logger redirected and returns
// everything it wrote.
func captureEventScanLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	defer captureLog(t, &buf)()
	fn()
	return buf.String()
}

// legRecordingProvider serves the tail probe from events at or above
// activeFloor, standing in for an active log whose older history has rotated
// away, and counts full reads so a test can assert which leg the handler took.
type legRecordingProvider struct {
	*events.Fake
	listCalls   int
	activeFloor uint64
}

func (p *legRecordingProvider) ListTail(filter events.Filter, limit int) ([]events.Event, error) {
	all, err := p.Fake.List(filter)
	if err != nil {
		return nil, err
	}
	var active []events.Event
	for _, e := range all {
		if e.Seq >= p.activeFloor {
			active = append(active, e)
		}
	}
	if limit > 0 && len(active) > limit {
		active = active[len(active)-limit:]
	}
	return active, nil
}

func (p *legRecordingProvider) List(filter events.Filter) ([]events.Event, error) {
	p.listCalls++
	return p.Fake.List(filter)
}

// TestEventListFullScanReportsItsCost pins that the expensive leg is never
// silent. A request the tail probe cannot answer falls through to the
// archive-aware read, which gunzips every rotated segment; that has to name
// itself and its cost in the log so an operator can tie it back to the client
// that asked for it.
func TestEventListFullScanReportsItsCost(t *testing.T) {
	state := newFakeState(t)
	fake := events.NewFake()
	// activeFloor above every seed seq: the tail probe always comes up empty,
	// so the handler must fall through to the full read.
	prov := &legRecordingProvider{Fake: fake, activeFloor: 9999}
	state.eventProv = prov
	h := newTestCityHandler(t, state)
	for i := 0; i < 5; i++ {
		fake.Record(events.Event{Type: "e.t", Actor: "a"})
	}

	logged := captureEventScanLog(t, func() {
		getList(t, h, cityURL(state, "/events?limit=500&type=e.t&since=2m"))
	})

	if prov.listCalls == 0 {
		t.Fatal("expected the full archive-aware read to be taken")
	}
	if !strings.Contains(logged, eventListFullScanLogPrefix) {
		t.Fatalf("full-history scan was silent; log = %q", logged)
	}
	for _, want := range []string{"type=e.t", "since=", "limit=500", "scanned=", "took="} {
		if !strings.Contains(logged, want) {
			t.Errorf("cost log is missing %q; log = %q", want, logged)
		}
	}
}

// TestEventListFastPathStaysSilent pins that the cost log is reserved for the
// expensive leg: a request the tail probe answers must not log at all.
func TestEventListFastPathStaysSilent(t *testing.T) {
	state := newFakeState(t)
	fake := events.NewFake()
	prov := &legRecordingProvider{Fake: fake, activeFloor: 1}
	state.eventProv = prov
	h := newTestCityHandler(t, state)
	for i := 0; i < 20; i++ {
		fake.Record(events.Event{Type: "e.t", Actor: "a"})
	}

	logged := captureEventScanLog(t, func() {
		getList(t, h, cityURL(state, "/events?limit=5"))
	})

	if strings.Contains(logged, eventListFullScanLogPrefix) {
		t.Fatalf("a probe-answered request must not log a full-scan cost; log = %q", logged)
	}
}

// TestEventFilterSinceFieldMeasuresFromReadStart pins that since= reports the
// window the caller asked for, not that window plus the scan's own duration.
// Measuring at log time made the field drift by exactly took, so it was least
// accurate on the slow reads the line exists to diagnose.
func TestEventFilterSinceFieldMeasuresFromReadStart(t *testing.T) {
	start := time.Now()
	since := start.Add(-2 * time.Minute)

	if got, want := eventFilterSinceField(since, start), "2m0s"; got != want {
		t.Fatalf("since field = %q, want %q", got, want)
	}

	// A slow scan must not widen the reported window: the same read logged 90s
	// later still asked for 2m. If the field ignored its reference instant
	// this guard would be vacuous, so assert it actually moves.
	if got := eventFilterSinceField(since, start.Add(90*time.Second)); got == "2m0s" {
		t.Fatal("eventFilterSinceField ignores its reference instant; the drift guard is vacuous")
	}
}

// TestEventFilterSinceFieldZeroIsNone keeps the no-window case legible.
func TestEventFilterSinceFieldZeroIsNone(t *testing.T) {
	if got, want := eventFilterSinceField(time.Time{}, time.Now()), "none"; got != want {
		t.Fatalf("zero Since = %q, want %q", got, want)
	}
}
