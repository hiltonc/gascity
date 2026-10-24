package main

import (
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	sessionpkg "github.com/gastownhall/gascity/internal/session"
)

// seedLiveResetPendingSession is a committed awake row on a live runtime,
// named after its alias, with a continuation reset nothing will consume.
func seedLiveResetPendingSession(t *testing.T, env *reconcilerTestEnv, extra map[string]string) beads.Bead {
	t.Helper()
	env.addDesired("minion", "minion", true)
	session := env.createSessionBead("minion", "minion")
	if session.ID == "minion" {
		t.Fatal("fixture must name the runtime after an alias distinct from the bead id")
	}
	meta := map[string]string{
		"alias":                      "minion",
		"state":                      string(sessionpkg.StateAwake),
		"state_reason":               "creation_complete",
		"wake_mode":                  "resume",
		"continuation_epoch":         "4",
		"continuation_reset_pending": "true",
		"last_woke_at":               env.clk.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	}
	for k, v := range extra {
		meta[k] = v
	}
	env.setSessionMetadata(&session, meta)
	return session
}

func TestReconcileSessionBeads_LiveRuntimeClearsOrphanedContinuationReset(t *testing.T) {
	env := newReconcilerTestEnv(t)
	session := seedLiveResetPendingSession(t, env, nil)

	env.reconcile([]beads.Bead{session})

	got := env.sessionInfo(session.ID)
	if got.ContinuationResetPending != "" {
		t.Fatalf("continuation_reset_pending = %q, want cleared on a live runtime", got.ContinuationResetPending)
	}
	if got.ContinuationEpoch != "4" {
		t.Fatalf("continuation_epoch = %q, want 4 kept: the live runtime runs under it", got.ContinuationEpoch)
	}
	if !env.sp.IsRunning("minion") {
		t.Fatal("the repair must leave the live runtime running")
	}
	if n := env.sp.CountCalls("Stop", "minion"); n != 0 {
		t.Fatalf("Stop called %d times on the live runtime, want 0", n)
	}
	if reason := sessionpkg.LifecycleDisplayReasonWithLivenessInfo(got, env.clk.Now(), env.sp.IsRunning); reason == sessionpkg.LifecycleReasonResetPending {
		t.Fatalf("display reason = %q after the repair", reason)
	}
}

// A reset a restart committed is still owed to its fresh start: the repair
// leaves it to the reset-stall path.
func TestReconcileSessionBeads_LiveRuntimeKeepsCommittedRestartReset(t *testing.T) {
	env := newReconcilerTestEnv(t)
	session := seedLiveResetPendingSession(t, env, map[string]string{
		sessionpkg.ResetCommittedAtKey: env.clk.Now().UTC().Format(time.RFC3339),
	})

	env.reconcile([]beads.Bead{session})

	if got := env.sessionInfo(session.ID).ContinuationResetPending; got != "true" {
		t.Fatalf("continuation_reset_pending = %q, want the committed restart's marker kept", got)
	}
}

// The clear's premise is a live runtime: a row whose runtime is down, or whose
// agent died in it, still owes its reset to the next start.
func TestReconcileSessionBeads_DeadRuntimeKeepsOrphanedContinuationReset(t *testing.T) {
	cases := map[string]func(env *reconcilerTestEnv){
		"runtime down": func(env *reconcilerTestEnv) {
			if err := env.sp.Stop("minion"); err != nil {
				t.Fatalf("Stop: %v", err)
			}
		},
		"agent dead": func(env *reconcilerTestEnv) {
			tp := env.desiredState["minion"]
			tp.Hints.ProcessNames = []string{"claude"}
			env.desiredState["minion"] = tp
			env.sp.Zombies["minion"] = true
		},
	}
	for name, kill := range cases {
		t.Run(name, func(t *testing.T) {
			env := newReconcilerTestEnv(t)
			session := seedLiveResetPendingSession(t, env, nil)
			kill(env)

			env.reconcile([]beads.Bead{session})

			if got := env.sessionInfo(session.ID).ContinuationResetPending; got != "true" {
				t.Fatalf("continuation_reset_pending = %q, want the marker kept for the next start", got)
			}
		})
	}
}
