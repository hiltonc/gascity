package tmux

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// scriptedExecutor answers each tmux subcommand by name rather than by call
// order, so a test states which listing produces which output and does not
// silently keep passing when FetchState stops making one of the calls.
type scriptedExecutor struct {
	panes     string
	windows   string
	windowErr error

	paneCalls   int
	windowCalls int
}

func (e *scriptedExecutor) execute(args []string) (string, error) {
	return e.executeCtx(context.Background(), args)
}

func (e *scriptedExecutor) executeCtx(_ context.Context, args []string) (string, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "list-panes"):
		e.paneCalls++
		return e.panes, nil
	case strings.Contains(joined, "list-windows"):
		e.windowCalls++
		if e.windowErr != nil {
			return "", e.windowErr
		}
		return e.windows, nil
	}
	return "", nil
}

// This is the wiring the whole change exists for, and the one place it is
// owned end to end: FetchState must call the window listing and fold it onto
// the sessions the panes walk found, then mark the result available. Every
// ingredient has its own test — listWindows under its sub-budget, and
// applyWindowListing over a literal map — but a test that calls them in
// sequence itself cannot fail when FetchState stops calling them. Delete the
// fold from FetchState and this test is what goes red; without it, GET
// /agents silently returns to a tmux fork per agent with nothing failing and
// no log line, because the degraded path logs only when listWindows ERRORS,
// never when it is not called at all.
func TestFetchStateFoldsTheWindowListingOntoTheSessionsItFound(t *testing.T) {
	exec := &scriptedExecutor{
		panes:   "gc-a\t0\tclaude\t111\ngc-b\t0\tclaude\t222\n",
		windows: "gc-a\t1\t1700000900\ngc-b\t0\t1700000100\n",
	}
	tm := NewTmux()
	tm.exec = exec
	f := &tmuxFetcher{tm: tm}

	state, err := f.FetchState(context.Background())
	if err != nil {
		t.Fatalf("FetchState: %v", err)
	}

	if exec.windowCalls != 1 {
		t.Fatalf("list-windows calls = %d, want 1: FetchState must run the fleet-wide window listing", exec.windowCalls)
	}
	if !state.WindowsAvailable {
		t.Fatal("WindowsAvailable = false after a window listing that answered; StateCache.Attached/LastActivity report known=false and every caller falls back to a per-session fork")
	}
	a := state.Sessions["gc-a"]
	if !a.Running {
		t.Fatalf("gc-a running = false, want true from the panes walk")
	}
	if !a.Attached {
		t.Errorf("gc-a attached = false, want true: the listing reports an attached client")
	}
	if want := time.Unix(1700000900, 0); !a.Activity.Equal(want) {
		t.Errorf("gc-a activity = %v, want %v folded from the window listing", a.Activity, want)
	}
	b := state.Sessions["gc-b"]
	if b.Attached {
		t.Errorf("gc-b attached = true, want false: the listing reports no client")
	}
	if want := time.Unix(1700000100, 0); !b.Activity.Equal(want) {
		t.Errorf("gc-b activity = %v, want %v folded from the window listing", b.Activity, want)
	}
}

// The fold is a refinement over the panes walk, never a gate on it: a window
// listing that fails must leave liveness intact and mark itself unavailable
// so callers probe live rather than read a snapshot that silently says every
// session is detached.
func TestFetchStateDegradesWhenTheWindowListingFails(t *testing.T) {
	exec := &scriptedExecutor{
		panes:     "gc-a\t0\tclaude\t111\n",
		windowErr: errors.New("list-windows: server exited unexpectedly"),
	}
	tm := NewTmux()
	tm.exec = exec
	f := &tmuxFetcher{tm: tm}

	state, err := f.FetchState(context.Background())
	if err != nil {
		t.Fatalf("FetchState returned an error for a failed window listing; it must degrade, not fail: %v", err)
	}
	if state.WindowsAvailable {
		t.Error("WindowsAvailable = true after the window listing failed; callers would trust an unfolded snapshot")
	}
	if !state.Sessions["gc-a"].Running {
		t.Error("gc-a running = false; liveness comes from list-panes and must survive a failed window listing")
	}
	if state.Sessions["gc-a"].Attached {
		t.Error("gc-a attached = true with no listing to fold; the value must stay zero so known=false is the only signal")
	}
}
