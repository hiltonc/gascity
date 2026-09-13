package tmux

import (
	"testing"
	"time"
)

func TestApplyWindowListing_FoldsAttachAndLatestActivityOntoRunningSessions(t *testing.T) {
	sessions := map[string]sessionRuntimeState{
		"gc-a": {Running: true},
		"gc-b": {Running: true},
	}
	out := "gc-a\t1\t1700000000\ngc-a\t1\t1700000900\ngc-b\t0\t1700000100\nunknown\t1\t1700000500\nmalformed line\n"
	applyWindowListing(sessions, out)

	a := sessions["gc-a"]
	if !a.Attached {
		t.Errorf("gc-a attached = false, want true (a client is attached)")
	}
	if want := time.Unix(1700000900, 0); !a.Activity.Equal(want) {
		t.Errorf("gc-a activity = %v, want the most recent window %v", a.Activity, want)
	}
	b := sessions["gc-b"]
	if b.Attached {
		t.Errorf("gc-b attached = true, want false (no client)")
	}
	if want := time.Unix(1700000100, 0); !b.Activity.Equal(want) {
		t.Errorf("gc-b activity = %v, want %v", b.Activity, want)
	}
	if _, ok := sessions["unknown"]; ok {
		t.Errorf("a session the panes listing did not report was added from the window listing")
	}
	if len(sessions) != 2 {
		t.Errorf("sessions = %d, want 2", len(sessions))
	}
}

func TestStateCache_WindowListingAnswersAttachedAndActivity(t *testing.T) {
	at := time.Unix(1700000900, 0)
	fetcher := &mockFetcher{state: runtimeStateSnapshot{
		Sessions: map[string]sessionRuntimeState{
			"gc-a": {Running: true, Attached: true, Activity: at},
			"gc-c": {Running: true},
		},
		WindowsAvailable: true,
	}}
	cache := NewStateCache(fetcher, time.Second)

	if attached, known := cache.Attached("gc-a"); !known || !attached {
		t.Errorf("Attached(gc-a) = (%v, known=%v), want (true, true)", attached, known)
	}
	if got, known := cache.LastActivity("gc-a"); !known || !got.Equal(at) {
		t.Errorf("LastActivity(gc-a) = (%v, known=%v), want (%v, true)", got, known, at)
	}
	if _, known := cache.LastActivity("gc-c"); known {
		t.Errorf("LastActivity(gc-c) known = true, want false: no window reported a timestamp")
	}
	if _, known := cache.Attached("missing"); known {
		t.Errorf("Attached(missing) known = true, want false")
	}
	if calls := fetcher.getCalls(); calls != 1 {
		t.Errorf("fetches = %d, want 1: every answer came from one snapshot", calls)
	}
}

func TestStateCache_WindowListingUnavailableIsNotKnown(t *testing.T) {
	fetcher := &mockFetcher{state: runtimeStateSnapshot{
		Sessions: map[string]sessionRuntimeState{
			"gc-a": {Running: true, Attached: true, Activity: time.Unix(1700000900, 0)},
		},
		WindowsAvailable: false,
	}}
	cache := NewStateCache(fetcher, time.Second)

	if _, known := cache.Attached("gc-a"); known {
		t.Errorf("Attached known = true with the window listing unavailable; the caller must probe live")
	}
	if _, known := cache.LastActivity("gc-a"); known {
		t.Errorf("LastActivity known = true with the window listing unavailable; the caller must probe live")
	}
	if !cache.IsRunning("gc-a") {
		t.Errorf("IsRunning = false; liveness comes from list-panes and must survive a missing window listing")
	}
}
