package main

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
)

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

	deleted, err := sweepClosedNudgeRetention(store, now, ttl, 1000)
	if err != nil {
		t.Fatalf("sweepClosedNudgeRetention: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2 (the two old closed nudges)", deleted)
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

	deleted, err := sweepClosedNudgeRetention(store, now, ttl, 3)
	if err != nil {
		t.Fatalf("sweepClosedNudgeRetention: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3 (budget limit)", deleted)
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

	deleted, err := sweepClosedSessionRetention(store, now, ttl, 1000)
	if err != nil {
		t.Fatalf("sweepClosedSessionRetention: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (the old closed session)", deleted)
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

	deleted, err := sweepClosedSessionRetention(store, now, ttl, 1000)
	if err != nil {
		t.Fatalf("sweepClosedSessionRetention: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0 (session with children must be skipped)", deleted)
	}
	if _, err := store.Get("session-parent"); err != nil {
		t.Fatalf("session-parent should survive: %v", err)
	}
}

// TestSweepRetention_NeverDeletesArchivedMail is the hard-constraint test from
// the bead description: seed an archived message bead older than any retention
// TTL and assert it survives both nudge and session retention sweeps.
func TestSweepRetention_NeverDeletesArchivedMail(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour

	archivedMail := beads.Bead{
		ID:        "mail-archived",
		Status:    "closed",
		Type:      "message",
		CreatedAt: now.Add(-720 * time.Hour),
		UpdatedAt: now.Add(-720 * time.Hour),
		Labels:    []string{"read", "to:alice", "from:bob"},
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

	nudgeDeleted, err := sweepClosedNudgeRetention(store, now, ttl, 1000)
	if err != nil {
		t.Fatalf("sweepClosedNudgeRetention: %v", err)
	}
	if nudgeDeleted != 1 {
		t.Fatalf("nudge deleted = %d, want 1", nudgeDeleted)
	}

	sessionDeleted, err := sweepClosedSessionRetention(store, now, ttl, 1000)
	if err != nil {
		t.Fatalf("sweepClosedSessionRetention: %v", err)
	}
	if sessionDeleted != 1 {
		t.Fatalf("session deleted = %d, want 1", sessionDeleted)
	}

	// The archived mail must still exist — the sweeps select only by label
	// (gc:nudge) and type (session), so type=message beads are never matched.
	if _, err := store.Get("mail-archived"); err != nil {
		t.Fatalf("archived mail bead must survive both retention sweeps: %v", err)
	}
}

func TestSweepClosedNudgeRetention_NilStoreIsNoop(t *testing.T) {
	deleted, err := sweepClosedNudgeRetention(nil, time.Now(), 24*time.Hour, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
}

func TestSweepClosedSessionRetention_NilStoreIsNoop(t *testing.T) {
	deleted, err := sweepClosedSessionRetention(nil, time.Now(), 24*time.Hour, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
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
