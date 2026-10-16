package session

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runtime"
)

const staleKeyTestSessionKey = "9cc6f616-c3ed-4f9e-8cdb-af3ff9deaccd"

// seedStaleKeyResumeSession is the shape a runtime-missing heal leaves on an
// on-demand named session: asleep, a resume key whose conversation is gone, and
// a pending continuation reset. Its runtime is named after the alias, not the
// bead id.
func seedStaleKeyResumeSession(t *testing.T, store beads.Store) string {
	t.Helper()
	b, err := store.Create(beads.Bead{
		Type:   BeadType,
		Labels: []string{LabelSession},
		Metadata: map[string]string{
			"state":                      string(StateAsleep),
			"sleep_reason":               string(SleepReasonRuntimeMissing),
			"wake_mode":                  "resume",
			"provider":                   "claude",
			"work_dir":                   "/tmp",
			"command":                    "claude --dangerously",
			"resume_flag":                "--resume",
			"resume_style":               "flag",
			"session_id_flag":            "--session-id",
			"session_key":                staleKeyTestSessionKey,
			"session_name":               "minion",
			"alias":                      "minion",
			"generation":                 "1",
			"instance_token":             "minion-token",
			"continuation_epoch":         "3",
			"continuation_reset_pending": "true",
		},
	})
	if err != nil {
		t.Fatalf("Create bead: %v", err)
	}
	return b.ID
}

const staleKeyTestResumeCmd = "claude --dangerously --resume " + staleKeyTestSessionKey

// lastStartEpoch is the GC_CONTINUATION_EPOCH the runtime named name was last
// started with.
func lastStartEpoch(t *testing.T, sp *runtime.Fake, name string) string {
	t.Helper()
	calls := sp.SnapshotCalls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Method == "Start" && calls[i].Name == name {
			return calls[i].Config.Env["GC_CONTINUATION_EPOCH"]
		}
	}
	t.Fatalf("no Start call for %q", name)
	return ""
}

// assertLiveWithoutResetPending fails unless id's runtime is up, its row holds
// no continuation reset, and the row's epoch is the one the runtime runs under.
func assertLiveWithoutResetPending(t *testing.T, store beads.Store, sp *runtime.Fake, id string) {
	t.Helper()
	if !sp.IsRunning("minion") {
		t.Fatal("runtime minion is not running")
	}
	b, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get bead: %v", err)
	}
	if got := b.Metadata["continuation_reset_pending"]; got != "" {
		t.Errorf("continuation_reset_pending = %q, want cleared: the fresh start consumed the reset", got)
	}
	if got := b.Metadata["session_key"]; got != "" {
		t.Errorf("session_key = %q, want the stale key cleared", got)
	}
	epoch, err := strconv.Atoi(b.Metadata["continuation_epoch"])
	if err != nil || epoch <= 3 {
		t.Errorf("continuation_epoch = %q, want rotated past 3", b.Metadata["continuation_epoch"])
	}
	if got, want := lastStartEpoch(t, sp, "minion"), b.Metadata["continuation_epoch"]; got != want {
		t.Errorf("runtime GC_CONTINUATION_EPOCH = %q, row continuation_epoch = %q; want equal", got, want)
	}
	if reason := LifecycleDisplayReasonWithLivenessInfo(infoFromPersistedBead(b), time.Now(), sp.IsRunning); reason == LifecycleReasonResetPending {
		t.Errorf("display reason = %q for a live, freshly started session", reason)
	}
}

// A resume whose key is stale dies at startup and is relaunched fresh. That
// fresh start is the reset: it must not leave continuation_reset_pending armed
// behind a runtime it already started (ga-h56n).
func TestEnsureRunning_StaleKeyFreshRetryConsumesContinuationReset(t *testing.T) {
	store := beads.NewMemStore()
	sp := &startupDeathProvider{Fake: runtime.NewFake(), armed: true}
	mgr := newTestManager(t, store, sp, WithStaleKeyDetectionWaiter(immediateStaleKeyDetectionWaiter))
	id := seedStaleKeyResumeSession(t, store)

	if err := mgr.Start(context.Background(), testActor(mgr, ActorOperator), id, staleKeyTestResumeCmd, runtime.Config{WorkDir: "/tmp"}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	assertLiveWithoutResetPending(t, store, sp.Fake, id)
}

// The same holds when the resumed runtime comes up and then dies inside the
// stale-key detection window, the second recovery site.
func TestEnsureRunning_StaleKeyDetectedAfterStartConsumesContinuationReset(t *testing.T) {
	store := beads.NewMemStore()
	sp := &diesAfterFirstStartProvider{Fake: runtime.NewFake()}
	mgr := newTestManager(t, store, sp, WithStaleKeyDetectionWaiter(sp.killFirstRuntime))
	id := seedStaleKeyResumeSession(t, store)

	if err := mgr.Start(context.Background(), testActor(mgr, ActorOperator), id, staleKeyTestResumeCmd, runtime.Config{WorkDir: "/tmp"}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := sp.CountCalls("Start", "minion"); got != 2 {
		t.Fatalf("Start called %d times, want the resume plus one fresh relaunch", got)
	}
	assertLiveWithoutResetPending(t, store, sp.Fake, id)
}

// A fresh relaunch that fails leaves the reset pending, so the next start
// rotates the continuation again rather than resuming nothing.
func TestEnsureRunning_StaleKeyFreshRetryFailureKeepsResetPending(t *testing.T) {
	store := beads.NewMemStore()
	sp := &startupDeathProvider{Fake: runtime.NewFake(), armed: true, failRetry: true}
	mgr := newTestManager(t, store, sp, WithStaleKeyDetectionWaiter(immediateStaleKeyDetectionWaiter))
	id := seedStaleKeyResumeSession(t, store)

	if err := mgr.Start(context.Background(), testActor(mgr, ActorOperator), id, staleKeyTestResumeCmd, runtime.Config{WorkDir: "/tmp"}); err == nil {
		t.Fatal("Start succeeded, want the fresh relaunch's failure")
	}

	b, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get bead: %v", err)
	}
	if got := b.Metadata["continuation_reset_pending"]; got != "true" {
		t.Errorf("continuation_reset_pending = %q, want true after a failed fresh relaunch", got)
	}
	if got := b.Metadata["session_key"]; got != "" {
		t.Errorf("session_key = %q, want the stale key cleared", got)
	}
}

// A wake whose post-start state write fails once reports the sync error and
// leaves the runtime up; once the store recovers, the next start converges on
// the live runtime with no reset left pending.
func TestEnsureRunning_PostStartStateWriteFailureConvergesOnRetry(t *testing.T) {
	mem := beads.NewMemStore()
	store := &failActiveStateOnceStore{Store: mem}
	sp := &startupDeathProvider{Fake: runtime.NewFake(), armed: true}
	mgr := newTestManager(t, store, sp, WithStaleKeyDetectionWaiter(immediateStaleKeyDetectionWaiter))
	id := seedStaleKeyResumeSession(t, mem)
	store.armed = true

	err := mgr.Start(context.Background(), testActor(mgr, ActorOperator), id, staleKeyTestResumeCmd, runtime.Config{WorkDir: "/tmp"})
	if !errors.Is(err, ErrStateSync) {
		t.Fatalf("Start error = %v, want ErrStateSync from the failed state write", err)
	}
	if !sp.IsRunning("minion") {
		t.Fatal("a failed state write must not stop the runtime it started")
	}

	if err := mgr.Start(context.Background(), testActor(mgr, ActorOperator), id, staleKeyTestResumeCmd, runtime.Config{WorkDir: "/tmp"}); err != nil {
		t.Fatalf("Start after the store recovered: %v", err)
	}

	b, err := mem.Get(id)
	if err != nil {
		t.Fatalf("Get bead: %v", err)
	}
	if got := State(b.Metadata["state"]); got != StateActive {
		t.Errorf("state = %q, want active", got)
	}
	assertLiveWithoutResetPending(t, mem, sp.Fake, id)
}

// diesAfterFirstStartProvider starts every runtime, and its stale-key waiter
// kills the first one, as a resume of a missing conversation exits right after
// it launches.
type diesAfterFirstStartProvider struct {
	*runtime.Fake
	killed bool
}

func (p *diesAfterFirstStartProvider) killFirstRuntime(_ context.Context, name string) error {
	if !p.killed {
		p.killed = true
		return p.Stop(name)
	}
	return nil
}

// failActiveStateOnceStore fails the first metadata batch that commits a row
// active, as a Dolt write lost under load does, then recovers.
type failActiveStateOnceStore struct {
	beads.Store
	armed bool
}

func (s *failActiveStateOnceStore) SetMetadataBatch(id string, kvs map[string]string) error {
	if s.armed && kvs["state"] == string(StateActive) {
		s.armed = false
		return errors.New("dolt: circuit breaker open")
	}
	return s.Store.SetMetadataBatch(id, kvs)
}
