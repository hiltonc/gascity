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

// budgetRecordingProvider records the filter each read leg received so a test
// can assert the cheap leg was actually bounded and the expensive leg was
// actually taken.
type budgetRecordingProvider struct {
	*events.Fake
	tailFilters []events.Filter
	listCalls   int
	activeFloor uint64
}

func (p *budgetRecordingProvider) ListTail(filter events.Filter, limit int) ([]events.Event, error) {
	p.tailFilters = append(p.tailFilters, filter)
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

func (p *budgetRecordingProvider) List(filter events.Filter) ([]events.Event, error) {
	p.listCalls++
	return p.Fake.List(filter)
}

// TestEventListTailLegIsByteBounded pins the cheap leg's bound. The tail probe
// is a backward scan of the active events.jsonl, and a selective filter that
// never fills the probe used to walk the whole file — on high-gas-city a
// 153 MiB active log, decoded line by line, before the handler threw the work
// away and did a second full pass in the fallback. The probe must carry a
// byte budget so that wasted leg is bounded regardless of how selective the
// filter is.
func TestEventListTailLegIsByteBounded(t *testing.T) {
	state := newFakeState(t)
	fake := events.NewFake()
	prov := &budgetRecordingProvider{Fake: fake, activeFloor: 1}
	state.eventProv = prov
	h := newTestCityHandler(t, state)
	for i := 0; i < 5; i++ {
		fake.Record(events.Event{Type: "e.t", Actor: "a"})
	}

	getList(t, h, cityURL(state, "/events?limit=500&type=e.t"))

	if len(prov.tailFilters) == 0 {
		t.Fatal("handler did not attempt the tail fast path")
	}
	for i, f := range prov.tailFilters {
		if f.MaxScanBytes <= 0 {
			t.Fatalf("tail probe %d carried MaxScanBytes=%d; the backward walk must be byte-bounded", i, f.MaxScanBytes)
		}
		if f.MaxScanBytes != eventListTailScanBudgetBytes {
			t.Fatalf("tail probe %d budget = %d, want %d", i, f.MaxScanBytes, eventListTailScanBudgetBytes)
		}
	}
}

// TestEventListFullScanReportsItsCost pins that the expensive leg is never
// silent. A request whose bounded probe cannot answer it falls through to the
// archive-aware read, which gunzips every rotated segment; that has to name
// itself and its cost in the log so an operator can tie it back to the client
// that asked for it.
func TestEventListFullScanReportsItsCost(t *testing.T) {
	state := newFakeState(t)
	fake := events.NewFake()
	// activeFloor above every seed seq: the tail probe always comes up empty,
	// so the handler must fall through to the full read.
	prov := &budgetRecordingProvider{Fake: fake, activeFloor: 9999}
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
// expensive leg: a request the bounded probe answers must not log at all.
func TestEventListFastPathStaysSilent(t *testing.T) {
	state := newFakeState(t)
	fake := events.NewFake()
	prov := &budgetRecordingProvider{Fake: fake, activeFloor: 1}
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
