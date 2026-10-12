package main

import (
	"bytes"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
)

func defaultSweepOpts(budget int) retentionSweepOpts {
	return retentionSweepOpts{budget: budget, batchSize: 50}
}

func TestSweepClosedNudgeRetention_DeletesOldClosedNudgeBeads(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour

	seed := []beads.Bead{
		{
			ID:        "nudge-old-1",
			Status:    "closed",
			Type:      "chore",
			CreatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		},
		{
			ID:        "nudge-old-2",
			Status:    "closed",
			Type:      "chore",
			CreatedAt: now.Add(-36 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		},
		{
			ID:        "nudge-recent",
			Status:    "closed",
			Type:      "chore",
			CreatedAt: now.Add(-12 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		},
		{
			ID:        "nudge-open",
			Status:    "open",
			Type:      "chore",
			CreatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		},
	}
	store := beads.NewMemStoreFrom(100, seed, nil)

	result, err := sweepClosedNudgeRetention(store, now, ttl, defaultSweepOpts(1000))
	if err != nil {
		t.Fatalf("sweepClosedNudgeRetention: %v", err)
	}
	if result.deleted != 2 {
		t.Fatalf("deleted = %d, want 2 (the two old closed nudges)", result.deleted)
	}
	for _, id := range []string{"nudge-old-1", "nudge-old-2"} {
		if _, err := store.Get(id); !errors.Is(err, beads.ErrNotFound) {
			t.Errorf("Get(%s) = %v, want ErrNotFound", id, err)
		}
	}
	if _, err := store.Get("nudge-recent"); err != nil {
		t.Errorf("nudge-recent should survive: %v", err)
	}
	if _, err := store.Get("nudge-open"); err != nil {
		t.Errorf("nudge-open should survive (not closed): %v", err)
	}
}

func TestSweepClosedNudgeRetention_HonorsBudget(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour

	seed := make([]beads.Bead, 0, 10)
	for i := 0; i < 10; i++ {
		seed = append(seed, beads.Bead{
			ID:        fmt.Sprintf("nudge-%02d", i),
			Status:    "closed",
			Type:      "chore",
			CreatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		})
	}
	store := beads.NewMemStoreFrom(100, seed, nil)

	result, err := sweepClosedNudgeRetention(store, now, ttl, defaultSweepOpts(3))
	if err != nil {
		t.Fatalf("sweepClosedNudgeRetention: %v", err)
	}
	if result.deleted != 3 {
		t.Fatalf("deleted = %d, want 3 (budget limit)", result.deleted)
	}
	if result.remaining != 7 {
		t.Errorf("remaining = %d, want 7", result.remaining)
	}
}

func TestSweepClosedSessionRetention_DeletesOldClosedSessionBeads(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour

	seed := []beads.Bead{
		{
			ID:        "session-old-1",
			Status:    "closed",
			Type:      "session",
			CreatedAt: now.Add(-48 * time.Hour),
			UpdatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{"gc:session", "agent:test"},
			NoHistory: true,
		},
		{
			ID:        "session-recent",
			Status:    "closed",
			Type:      "session",
			CreatedAt: now.Add(-12 * time.Hour),
			UpdatedAt: now.Add(-12 * time.Hour),
			Labels:    []string{"gc:session", "agent:test"},
			NoHistory: true,
		},
		{
			ID:        "session-open",
			Status:    "open",
			Type:      "session",
			CreatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{"gc:session", "agent:test"},
			NoHistory: true,
		},
	}
	store := beads.NewMemStoreFrom(100, seed, nil)

	result, err := sweepClosedSessionRetention(store, now, ttl, defaultSweepOpts(1000))
	if err != nil {
		t.Fatalf("sweepClosedSessionRetention: %v", err)
	}
	if result.deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (the old closed session)", result.deleted)
	}
	if _, err := store.Get("session-old-1"); !errors.Is(err, beads.ErrNotFound) {
		t.Errorf("Get(session-old-1) = %v, want ErrNotFound", err)
	}
	if _, err := store.Get("session-recent"); err != nil {
		t.Errorf("session-recent should survive: %v", err)
	}
	if _, err := store.Get("session-open"); err != nil {
		t.Errorf("session-open should survive: %v", err)
	}
}

func TestSweepClosedSessionRetention_SkipsSessionsWithChildren(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour

	seed := []beads.Bead{
		{
			ID:        "session-parent",
			Status:    "closed",
			Type:      "session",
			CreatedAt: now.Add(-48 * time.Hour),
			UpdatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{"gc:session"},
			NoHistory: true,
		},
		{
			ID:        "child-bead",
			Status:    "open",
			Type:      "task",
			CreatedAt: now.Add(-24 * time.Hour),
			ParentID:  "session-parent",
		},
	}
	store := beads.NewMemStoreFrom(100, seed, nil)

	result, err := sweepClosedSessionRetention(store, now, ttl, defaultSweepOpts(1000))
	if err != nil {
		t.Fatalf("sweepClosedSessionRetention: %v", err)
	}
	if result.deleted != 0 {
		t.Fatalf("deleted = %d, want 0 (session with children must be skipped)", result.deleted)
	}
	if _, err := store.Get("session-parent"); err != nil {
		t.Fatalf("session-parent should survive: %v", err)
	}
}

// TestSweepRetention_NeverDeletesArchivedMail verifies the hard constraint:
// an archived message bead older than any retention TTL survives both sweeps.
func TestSweepRetention_NeverDeletesArchivedMail(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour

	archivedMail := beads.Bead{
		ID:        "mail-archived",
		Status:    "closed",
		Type:      "message",
		CreatedAt: now.Add(-720 * time.Hour),
		UpdatedAt: now.Add(-720 * time.Hour),
		Labels:    []string{"read", "to:alice", "from:bob", nudgeBeadLabel},
		Ephemeral: true,
	}
	nudgeBead := beads.Bead{
		ID:        "nudge-old",
		Status:    "closed",
		Type:      "chore",
		CreatedAt: now.Add(-48 * time.Hour),
		Labels:    []string{nudgeBeadLabel},
		NoHistory: true,
	}
	sessionBead := beads.Bead{
		ID:        "session-old",
		Status:    "closed",
		Type:      "session",
		CreatedAt: now.Add(-48 * time.Hour),
		UpdatedAt: now.Add(-48 * time.Hour),
		Labels:    []string{"gc:session"},
		NoHistory: true,
	}

	store := beads.NewMemStoreFrom(100, []beads.Bead{archivedMail, nudgeBead, sessionBead}, nil)

	nudgeResult, err := sweepClosedNudgeRetention(store, now, ttl, defaultSweepOpts(1000))
	if err != nil {
		t.Fatalf("sweepClosedNudgeRetention: %v", err)
	}
	if nudgeResult.deleted != 1 {
		t.Fatalf("nudge deleted = %d, want 1", nudgeResult.deleted)
	}

	sessionResult, err := sweepClosedSessionRetention(store, now, ttl, defaultSweepOpts(1000))
	if err != nil {
		t.Fatalf("sweepClosedSessionRetention: %v", err)
	}
	if sessionResult.deleted != 1 {
		t.Fatalf("session deleted = %d, want 1", sessionResult.deleted)
	}

	if _, err := store.Get("mail-archived"); err != nil {
		t.Fatalf("archived mail bead must survive both retention sweeps: %v", err)
	}
}

func TestSweepClosedNudgeRetention_NilStoreIsNoop(t *testing.T) {
	result, err := sweepClosedNudgeRetention(nil, time.Now(), 24*time.Hour, defaultSweepOpts(100))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.deleted != 0 {
		t.Fatalf("deleted = %d, want 0", result.deleted)
	}
}

func TestSweepClosedSessionRetention_NilStoreIsNoop(t *testing.T) {
	result, err := sweepClosedSessionRetention(nil, time.Now(), 24*time.Hour, defaultSweepOpts(100))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.deleted != 0 {
		t.Fatalf("deleted = %d, want 0", result.deleted)
	}
}

func TestCountClosedWispsPlaneRetentionEligible(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	seed := []beads.Bead{
		{
			ID:        "nudge-old",
			Status:    "closed",
			Type:      "chore",
			CreatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		},
		{
			ID:        "nudge-recent",
			Status:    "closed",
			Type:      "chore",
			CreatedAt: now.Add(-12 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		},
		{
			ID:        "session-old",
			Status:    "closed",
			Type:      "session",
			CreatedAt: now.Add(-48 * time.Hour),
			UpdatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{"gc:session"},
			NoHistory: true,
		},
		{
			ID:        "mail-archived",
			Status:    "closed",
			Type:      "message",
			CreatedAt: now.Add(-720 * time.Hour),
			Ephemeral: true,
		},
	}
	store := beads.NewMemStoreFrom(100, seed, nil)

	policy := wispsPlaneRetentionPolicy{
		nudgeDeleteAfterClose:   24 * time.Hour,
		sessionDeleteAfterClose: 24 * time.Hour,
	}
	nudge, session := countClosedWispsPlaneRetentionEligible(store, now, policy)
	if nudge != 1 {
		t.Errorf("nudge eligible = %d, want 1", nudge)
	}
	if session != 1 {
		t.Errorf("session eligible = %d, want 1", session)
	}
}

func TestWispsPlaneRetentionPolicyForConfig(t *testing.T) {
	cfg := &config.City{
		Beads: config.BeadsConfig{
			Policies: map[string]config.BeadPolicyConfig{
				"nudge":   {DeleteAfterClose: "48h"},
				"session": {DeleteAfterClose: "72h"},
			},
		},
	}
	policy := wispsPlaneRetentionPolicyForConfig(cfg)
	if policy.nudgeDeleteAfterClose != 48*time.Hour {
		t.Errorf("nudge TTL = %v, want 48h", policy.nudgeDeleteAfterClose)
	}
	if policy.sessionDeleteAfterClose != 72*time.Hour {
		t.Errorf("session TTL = %v, want 72h", policy.sessionDeleteAfterClose)
	}
}

func TestWispsPlaneRetentionPolicyForConfig_NilConfig(t *testing.T) {
	policy := wispsPlaneRetentionPolicyForConfig(nil)
	if policy.nudgeDeleteAfterClose != 0 {
		t.Errorf("nudge TTL = %v, want 0 for nil config", policy.nudgeDeleteAfterClose)
	}
	if policy.sessionDeleteAfterClose != 0 {
		t.Errorf("session TTL = %v, want 0 for nil config", policy.sessionDeleteAfterClose)
	}
}

// --- New tests for time-bounding and batching ---

// TestSweepStopsAtDeadline verifies that a retention sweep stops when the
// clock reports a time past the deadline, even if eligible beads remain.
// The test uses a fake clock that advances by 200ms per call, so a 500ms
// deadline allows ~2 deletes before the sweep stops.
func TestSweepStopsAtDeadline(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour

	seed := make([]beads.Bead, 0, 20)
	for i := 0; i < 20; i++ {
		seed = append(seed, beads.Bead{
			ID:        fmt.Sprintf("nudge-%02d", i),
			Status:    "closed",
			Type:      "chore",
			CreatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		})
	}
	store := beads.NewMemStoreFrom(100, seed, nil)

	var calls atomic.Int64
	fakeClock := func() time.Time {
		n := calls.Add(1)
		return now.Add(time.Duration(n) * 200 * time.Millisecond)
	}

	opts := retentionSweepOpts{
		deadline:  now.Add(500 * time.Millisecond),
		budget:    1000,
		batchSize: 1,
		clockFn:   fakeClock,
	}

	result, err := sweepClosedNudgeRetention(store, now, ttl, opts)
	if err != nil {
		t.Fatalf("sweepClosedNudgeRetention: %v", err)
	}
	if result.deleted >= 20 {
		t.Fatalf("deleted = %d, want fewer than 20 (deadline should stop early)", result.deleted)
	}
	if !result.timedOut {
		t.Fatal("timedOut = false, want true")
	}
	if result.remaining <= 0 {
		t.Fatal("remaining should be > 0 after deadline stop")
	}
}

// TestSweepBatchesDeletions verifies that deletions are batched rather
// than done one at a time by checking that all eligible beads are removed
// when using a batch size larger than 1.
func TestSweepBatchesDeletions(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour

	seed := make([]beads.Bead, 0, 10)
	for i := 0; i < 10; i++ {
		seed = append(seed, beads.Bead{
			ID:        fmt.Sprintf("nudge-%02d", i),
			Status:    "closed",
			Type:      "chore",
			CreatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		})
	}
	store := beads.NewMemStoreFrom(100, seed, nil)

	opts := retentionSweepOpts{
		budget:    1000,
		batchSize: 5,
	}

	result, err := sweepClosedNudgeRetention(store, now, ttl, opts)
	if err != nil {
		t.Fatalf("sweepClosedNudgeRetention: %v", err)
	}
	if result.deleted != 10 {
		t.Fatalf("deleted = %d, want 10 (all eligible)", result.deleted)
	}
	if result.remaining != 0 {
		t.Errorf("remaining = %d, want 0", result.remaining)
	}
}

// TestWatchdogStartupUsesSmallBudget verifies the first watchdog pass after
// startup uses the startup time budget, not the steady-state budget.
func TestWatchdogStartupUsesSmallBudget(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour

	seed := make([]beads.Bead, 0, 500)
	for i := 0; i < 500; i++ {
		seed = append(seed, beads.Bead{
			ID:        fmt.Sprintf("nudge-%04d", i),
			Status:    "closed",
			Type:      "chore",
			CreatedAt: now.Add(-48 * time.Hour),
			Labels:    []string{nudgeBeadLabel},
			NoHistory: true,
		})
	}
	store := beads.NewMemStoreFrom(1000, seed, nil)

	var deleteCalls atomic.Int64
	fakeClock := func() time.Time {
		n := deleteCalls.Add(1)
		return now.Add(time.Duration(n) * 100 * time.Millisecond)
	}

	cfg := &config.City{
		Beads: config.BeadsConfig{
			Policies: map[string]config.BeadPolicyConfig{
				"nudge":   {DeleteAfterClose: ttl.String()},
				"session": {DeleteAfterClose: ttl.String()},
			},
		},
	}
	config.ApplyBeadPolicyDefaults(cfg)

	var stderr bytes.Buffer
	cr := &CityRuntime{
		cfg:                            cfg,
		logPrefix:                      "test",
		stderr:                         &stderr,
		standaloneCityStore:            store,
		wispsPlaneRetentionClockFn:     fakeClock,
		wispsPlaneRetentionStartupDone: false,
	}

	cr.runWispsPlaneRetentionWatchdog(cfg, now)

	remaining := 0
	for i := 0; i < 500; i++ {
		if _, err := store.Get(fmt.Sprintf("nudge-%04d", i)); err == nil {
			remaining++
		}
	}
	if remaining == 0 {
		t.Fatal("startup pass deleted all 500 beads; should have stopped early due to time budget")
	}

	if !cr.wispsPlaneRetentionStartupDone {
		t.Fatal("wispsPlaneRetentionStartupDone should be true after first pass")
	}

	// Second pass should use the steady-state (larger) budget.
	deleteCalls.Store(0)
	cr.wispsPlaneRetentionWatchdogLast = time.Time{}
	cr.runWispsPlaneRetentionWatchdog(cfg, now.Add(10*time.Minute))

	remaining2 := 0
	for i := 0; i < 500; i++ {
		if _, err := store.Get(fmt.Sprintf("nudge-%04d", i)); err == nil {
			remaining2++
		}
	}
	if remaining2 >= remaining {
		t.Fatalf("second pass deleted nothing (remaining: %d → %d), steady-state budget should have deleted more", remaining, remaining2)
	}
}

// TestDeleteBatchedWithDeadline_DeadlineStopsBeforeBudget verifies that the
// batched delete stops when the deadline expires even when count budget has
// room.
func TestDeleteBatchedWithDeadline_DeadlineStopsBeforeBudget(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	seed := make([]beads.Bead, 0, 100)
	for i := 0; i < 100; i++ {
		seed = append(seed, beads.Bead{
			ID:        fmt.Sprintf("b-%03d", i),
			Status:    "closed",
			Type:      "chore",
			NoHistory: true,
		})
	}
	store := beads.NewMemStoreFrom(200, seed, nil)

	var calls atomic.Int64
	fakeClock := func() time.Time {
		n := calls.Add(1)
		return now.Add(time.Duration(n) * 300 * time.Millisecond)
	}

	ids := make([]string, 100)
	for i := range ids {
		ids[i] = fmt.Sprintf("b-%03d", i)
	}

	opts := retentionSweepOpts{
		deadline:  now.Add(1 * time.Second),
		budget:    100,
		batchSize: 1,
		clockFn:   fakeClock,
	}

	deleted, timedOut, err := deleteBatchedWithDeadline(store, ids, opts)
	if err != nil {
		t.Fatalf("deleteBatchedWithDeadline: %v", err)
	}
	if !timedOut {
		t.Fatal("timedOut = false, want true")
	}
	if deleted >= 100 {
		t.Fatalf("deleted = %d, want < 100", deleted)
	}
	if deleted == 0 {
		t.Fatal("deleted = 0, want > 0")
	}
}

// TestRetentionSweepOpts_PastDeadline checks the deadline helper.
func TestRetentionSweepOpts_PastDeadline(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	t.Run("zero deadline never expires", func(t *testing.T) {
		opts := retentionSweepOpts{clockFn: func() time.Time { return now.Add(time.Hour) }}
		if opts.pastDeadline() {
			t.Fatal("zero deadline should never expire")
		}
	})

	t.Run("before deadline", func(t *testing.T) {
		opts := retentionSweepOpts{
			deadline: now.Add(time.Second),
			clockFn:  func() time.Time { return now },
		}
		if opts.pastDeadline() {
			t.Fatal("should not be past deadline")
		}
	})

	t.Run("at deadline", func(t *testing.T) {
		opts := retentionSweepOpts{
			deadline: now,
			clockFn:  func() time.Time { return now },
		}
		if !opts.pastDeadline() {
			t.Fatal("should be past deadline (at == past)")
		}
	})

	t.Run("after deadline", func(t *testing.T) {
		opts := retentionSweepOpts{
			deadline: now,
			clockFn:  func() time.Time { return now.Add(time.Millisecond) },
		}
		if !opts.pastDeadline() {
			t.Fatal("should be past deadline")
		}
	})
}
