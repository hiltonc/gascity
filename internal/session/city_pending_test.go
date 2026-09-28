package session

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runtime"
)

func createCityPendingTestSession(t *testing.T, mgr *Manager, title string) Info {
	t.Helper()
	info, err := mgr.CreateSession(context.Background(), CreateOptions{Template: "helper", Title: title, Command: "claude", WorkDir: t.TempDir(), Provider: "claude", ExtraMeta: map[string]string{"session_origin": "manual"}})
	if err != nil {
		t.Fatalf("CreateSession(%q): %v", title, err)
	}
	return info
}

func listCityPendingTestInfos(t *testing.T, mgr *Manager) []Info {
	t.Helper()
	infos, err := mgr.List("", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return infos
}

func TestCityPendingReturnsOnlySessionsAwaitingADecision(t *testing.T) {
	store := beads.NewMemStore()
	sp := runtime.NewFake()
	mgr := NewManagerWithOptions(store, sp)

	waiting := createCityPendingTestSession(t, mgr, "Waiting")
	createCityPendingTestSession(t, mgr, "Idle")
	sp.SetPendingInteraction(waiting.SessionName, &runtime.PendingInteraction{RequestID: "req-1", Kind: "approval", Prompt: "approve?"})

	entries, probeErrors := mgr.CityPending(listCityPendingTestInfos(t, mgr))
	if len(probeErrors) != 0 {
		t.Fatalf("probeErrors = %#v, want none", probeErrors)
	}
	want := []CityPendingEntry{{SessionID: waiting.ID, RequestID: "req-1", Kind: "approval"}}
	if fmt.Sprint(entries) != fmt.Sprint(want) {
		t.Fatalf("entries = %#v, want %#v", entries, want)
	}
}

func TestCityPendingNothingPendingIsEmptyNotNil(t *testing.T) {
	store := beads.NewMemStore()
	sp := runtime.NewFake()
	mgr := NewManagerWithOptions(store, sp)
	createCityPendingTestSession(t, mgr, "Idle")

	entries, probeErrors := mgr.CityPending(listCityPendingTestInfos(t, mgr))
	if entries == nil || len(entries) != 0 {
		t.Fatalf("entries = %#v, want empty non-nil slice", entries)
	}
	if probeErrors != nil {
		t.Fatalf("probeErrors = %#v, want nil", probeErrors)
	}
}

func TestCityPendingSkipsSuspendedSessions(t *testing.T) {
	store := beads.NewMemStore()
	sp := runtime.NewFake()
	mgr := NewManagerWithOptions(store, sp)

	suspended := createCityPendingTestSession(t, mgr, "Suspended")
	sp.SetPendingInteraction(suspended.SessionName, &runtime.PendingInteraction{RequestID: "req-stale", Kind: "approval"})
	if err := mgr.Suspend(suspended.ID); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	entries, _ := mgr.CityPending(listCityPendingTestInfos(t, mgr))
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want suspended session excluded", entries)
	}
}

type cityPendingPerSessionErrorProvider struct {
	*runtime.Fake
	failName string
}

func (p *cityPendingPerSessionErrorProvider) Pending(name string) (*runtime.PendingInteraction, error) {
	if name == p.failName {
		return nil, fmt.Errorf("capturing pane: probe blew up")
	}
	return p.Fake.Pending(name)
}

func TestCityPendingReportsProbeFailuresWithoutDroppingOthers(t *testing.T) {
	store := beads.NewMemStore()
	fake := runtime.NewFake()
	sp := &cityPendingPerSessionErrorProvider{Fake: fake}
	mgr := NewManagerWithOptions(store, sp)

	healthy := createCityPendingTestSession(t, mgr, "Healthy")
	failing := createCityPendingTestSession(t, mgr, "Failing")
	sp.failName = failing.SessionName
	fake.SetPendingInteraction(healthy.SessionName, &runtime.PendingInteraction{RequestID: "req-healthy", Kind: "approval"})

	entries, probeErrors := mgr.CityPending(listCityPendingTestInfos(t, mgr))
	if len(probeErrors) != 1 || !strings.Contains(probeErrors[0], failing.ID) {
		t.Fatalf("probeErrors = %#v, want one entry naming %s", probeErrors, failing.ID)
	}
	if len(entries) != 1 || entries[0].SessionID != healthy.ID {
		t.Fatalf("entries = %#v, want only healthy session %s", entries, healthy.ID)
	}
}

type cityPendingNoInteractionProvider struct {
	runtime.Provider
}

func TestCityPendingUnsupportedProviderReportsNothing(t *testing.T) {
	store := beads.NewMemStore()
	fake := runtime.NewFake()
	mgr := NewManagerWithOptions(store, fake)
	waiting := createCityPendingTestSession(t, mgr, "Waiting")
	fake.SetPendingInteraction(waiting.SessionName, &runtime.PendingInteraction{RequestID: "req-1", Kind: "approval"})
	infos := listCityPendingTestInfos(t, mgr)

	unsupported := NewManagerWithOptions(store, cityPendingNoInteractionProvider{Provider: fake})
	entries, probeErrors := unsupported.CityPending(infos)
	if len(entries) != 0 || len(probeErrors) != 0 {
		t.Fatalf("entries=%#v probeErrors=%#v, want nothing from a provider without interaction support", entries, probeErrors)
	}
}
