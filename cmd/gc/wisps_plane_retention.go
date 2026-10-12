package main

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/doctor"
)

const (
	// wispsPlaneRetentionWatchdogInterval is the minimum time between
	// controller-driven wisps-plane retention sweeps. Shorter than the
	// order-tracking watchdog (15m) because the per-tick budget is larger and the
	// creation rate demands faster drain.
	wispsPlaneRetentionWatchdogInterval = 5 * time.Minute

	// wispsPlaneRetentionDeleteBudget bounds the number of closed wisps-plane
	// beads deleted per watchdog invocation across all categories. At ~8.7k
	// order-tracking wisps/day plus nudge and session wisps, 1000 per 5min
	// (288k/day) keeps up with any observed burst rate.
	wispsPlaneRetentionDeleteBudget = 1000

	// mailBeadType is the bead type mail is stored as (cmd_mail.go). The
	// sweeps never delete it: closed mail addressed to a human is the archive.
	mailBeadType = "message"
)

// wispsPlaneRetentionPolicy holds the per-category TTL for wisps-plane
// retention. Each category's TTL comes from [beads.policies.<name>].delete_after_close.
type wispsPlaneRetentionPolicy struct {
	nudgeDeleteAfterClose   time.Duration
	sessionDeleteAfterClose time.Duration
}

// wispsPlaneRetentionPolicyForConfig reads the wisps-plane retention TTLs from
// the city config. Each category uses its own [beads.policies.<name>] section.
// ApplyBeadPolicyDefaults fills the defaults (24h) before this runs.
func wispsPlaneRetentionPolicyForConfig(cfg *config.City) wispsPlaneRetentionPolicy {
	policy := wispsPlaneRetentionPolicy{}
	if cfg == nil {
		return policy
	}
	if p, ok := cfg.Beads.Policies["nudge"]; ok {
		policy.nudgeDeleteAfterClose = p.DeleteAfterCloseDuration()
	}
	if p, ok := cfg.Beads.Policies["session"]; ok {
		policy.sessionDeleteAfterClose = p.DeleteAfterCloseDuration()
	}
	return policy
}

// sweepClosedNudgeRetention deletes closed nudge beads past their TTL.
// It lists closed beads with the gc:nudge label, filters by age, and deletes
// each individually. The limit bounds total delete attempts per call. A
// message bead is never deleted, even when it carries the nudge label.
func sweepClosedNudgeRetention(store beads.Store, now time.Time, deleteAfterClose time.Duration, limit int) (int, error) {
	if store == nil || deleteAfterClose <= 0 || limit <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-deleteAfterClose)
	candidates, err := store.List(beads.ListQuery{
		Status:   "closed",
		Label:    nudgeBeadLabel,
		TierMode: beads.TierBoth,
	})
	if err != nil {
		return 0, fmt.Errorf("listing closed nudge beads for retention: %w", err)
	}

	deleted := 0
	var deleteErr error
	for _, c := range candidates {
		if deleted >= limit {
			break
		}
		if c.Type == mailBeadType {
			// Never delete mail, whatever labels it carries: closed mail
			// addressed to a human is that human's archive.
			continue
		}
		if c.CreatedAt.IsZero() || !c.CreatedAt.Before(cutoff) {
			continue
		}
		if err := deleteWorkflowBead(store, c.ID); err != nil {
			if errors.Is(err, errWorkflowDeleteLiveDescendants) {
				continue
			}
			deleteErr = errors.Join(deleteErr, fmt.Errorf("deleting closed nudge bead %q: %w", c.ID, err))
			continue
		}
		deleted++
	}
	return deleted, deleteErr
}

// sweepClosedSessionRetention deletes closed session beads past their TTL.
// It lists closed beads of type "session", filters by age, and deletes each
// individually. The limit bounds total delete attempts per call. Sessions that
// still own children are skipped.
func sweepClosedSessionRetention(store beads.Store, now time.Time, deleteAfterClose time.Duration, limit int) (int, error) {
	if store == nil || deleteAfterClose <= 0 || limit <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-deleteAfterClose)
	candidates, err := store.List(beads.ListQuery{
		Status:   "closed",
		Type:     "session",
		TierMode: beads.TierBoth,
	})
	if err != nil {
		return 0, fmt.Errorf("listing closed session beads for retention: %w", err)
	}

	deleted := 0
	var deleteErr error
	for _, c := range candidates {
		if deleted >= limit {
			break
		}
		activity := c.UpdatedAt
		if activity.IsZero() {
			activity = c.CreatedAt
		}
		if activity.IsZero() || !activity.Before(cutoff) {
			continue
		}
		children, childErr := store.Children(c.ID, beads.IncludeClosed, beads.WithBothTiers)
		if childErr != nil {
			deleteErr = errors.Join(deleteErr, fmt.Errorf("listing children for session %q: %w", c.ID, childErr))
			continue
		}
		if len(children) > 0 {
			continue
		}
		if err := deleteWorkflowBead(store, c.ID); err != nil {
			if errors.Is(err, errWorkflowDeleteLiveDescendants) {
				continue
			}
			deleteErr = errors.Join(deleteErr, fmt.Errorf("deleting closed session bead %q: %w", c.ID, err))
			continue
		}
		deleted++
	}
	return deleted, deleteErr
}

// countClosedWispsPlaneRetentionEligible counts closed nudge and session beads
// that are past their TTL and would be deleted by the retention sweep.
func countClosedWispsPlaneRetentionEligible(store beads.Store, now time.Time, policy wispsPlaneRetentionPolicy) (nudgeEligible, sessionEligible int) {
	if store == nil {
		return 0, 0
	}
	if policy.nudgeDeleteAfterClose > 0 {
		nudgeCutoff := now.Add(-policy.nudgeDeleteAfterClose)
		candidates, err := store.List(beads.ListQuery{
			Status:   "closed",
			Label:    nudgeBeadLabel,
			TierMode: beads.TierBoth,
		})
		if err == nil {
			for _, c := range candidates {
				if c.Type == mailBeadType {
					continue
				}
				if !c.CreatedAt.IsZero() && c.CreatedAt.Before(nudgeCutoff) {
					nudgeEligible++
				}
			}
		}
	}
	if policy.sessionDeleteAfterClose > 0 {
		sessionCutoff := now.Add(-policy.sessionDeleteAfterClose)
		candidates, err := store.List(beads.ListQuery{
			Status:   "closed",
			Type:     "session",
			TierMode: beads.TierBoth,
		})
		if err == nil {
			for _, c := range candidates {
				activity := c.UpdatedAt
				if activity.IsZero() {
					activity = c.CreatedAt
				}
				if !activity.IsZero() && activity.Before(sessionCutoff) {
					sessionEligible++
				}
			}
		}
	}
	return nudgeEligible, sessionEligible
}

// runWispsPlaneRetentionWatchdog deletes closed wisps-plane beads (nudge and
// session) past their configured TTL. Order-tracking beads are handled by
// runOrderTrackingRetentionWatchdog. This watchdog complements that one by
// covering the two categories that previously had no retention at all.
//
// It runs at most once per wispsPlaneRetentionWatchdogInterval and logs when
// the eligible backlog exceeds the per-tick budget so an operator can see the
// city is falling behind.
func (cr *CityRuntime) runWispsPlaneRetentionWatchdog(cfg *config.City, now time.Time) {
	if !cr.wispsPlaneRetentionWatchdogLast.IsZero() &&
		now.Sub(cr.wispsPlaneRetentionWatchdogLast) < wispsPlaneRetentionWatchdogInterval {
		return
	}
	cr.wispsPlaneRetentionWatchdogLast = now

	if cr.cityPath != "" {
		if safe, reason := doctor.BulkDeleteSafe(cr.cityPath, cfg, bulkDeleteMaxAge(cfg), now); !safe {
			if cr.stderr != nil {
				_, _ = fmt.Fprintf(cr.stderr, "%s: wisps-plane retention watchdog: skipping bulk delete — %s\n", cr.logPrefix, reason)
			}
			return
		}
	}

	cityStore := cr.cityBeadStore()
	if cityStore == nil {
		return
	}

	policy := wispsPlaneRetentionPolicyForConfig(cfg)
	budget := wispsPlaneRetentionDeleteBudget

	var totalDeleted int
	var sweepErr error

	if policy.nudgeDeleteAfterClose > 0 {
		nudgeDeleted, nudgeErr := sweepClosedNudgeRetention(cityStore, now, policy.nudgeDeleteAfterClose, budget)
		totalDeleted += nudgeDeleted
		budget -= nudgeDeleted
		if budget < 0 {
			budget = 0
		}
		sweepErr = errors.Join(sweepErr, nudgeErr)
		if nudgeDeleted > 0 && cr.stderr != nil {
			_, _ = fmt.Fprintf(cr.stderr, "%s: wisps-plane retention watchdog: deleted %d closed nudge bead(s)\n", cr.logPrefix, nudgeDeleted)
		}
	}

	if policy.sessionDeleteAfterClose > 0 && budget > 0 {
		sessionDeleted, sessionErr := sweepClosedSessionRetention(cityStore, now, policy.sessionDeleteAfterClose, budget)
		totalDeleted += sessionDeleted
		sweepErr = errors.Join(sweepErr, sessionErr)
		if sessionDeleted > 0 && cr.stderr != nil {
			_, _ = fmt.Fprintf(cr.stderr, "%s: wisps-plane retention watchdog: deleted %d closed session bead(s)\n", cr.logPrefix, sessionDeleted)
		}
	}

	if sweepErr != nil && cr.stderr != nil {
		_, _ = fmt.Fprintf(cr.stderr, "%s: wisps-plane retention watchdog: %v\n", cr.logPrefix, sweepErr)
	}

	if totalDeleted >= wispsPlaneRetentionDeleteBudget {
		nudgeBacklog, sessionBacklog := countClosedWispsPlaneRetentionEligible(cityStore, now, policy)
		remaining := nudgeBacklog + sessionBacklog
		if remaining > 0 && cr.stderr != nil {
			_, _ = fmt.Fprintf(cr.stderr, "%s: wisps-plane retention watchdog: %d eligible bead(s) remain after budget exhausted (nudge: %d, session: %d) — backlog will drain over subsequent ticks\n",
				cr.logPrefix, remaining, nudgeBacklog, sessionBacklog)
		}
	}
}

// warnIfClosedWispsPlaneBacklogLarge writes one advisory line to stderr at
// startup when the city holds a significant number of closed nudge or session
// beads. This mirrors warnIfClosedOrderTrackingBacklogLarge for the two new
// categories.
func warnIfClosedWispsPlaneBacklogLarge(store beads.Store, stderr io.Writer) {
	if store == nil || stderr == nil {
		return
	}
	nudgeCount := 0
	if nudges, err := store.List(beads.ListQuery{
		Status:   "closed",
		Label:    nudgeBeadLabel,
		TierMode: beads.TierBoth,
		Limit:    1001,
	}); err == nil {
		nudgeCount = len(nudges)
	}
	sessionCount := 0
	if sessions, err := store.List(beads.ListQuery{
		Status:   "closed",
		Type:     "session",
		TierMode: beads.TierBoth,
		Limit:    1001,
	}); err == nil {
		sessionCount = len(sessions)
	}

	total := nudgeCount + sessionCount
	if total <= 100 {
		return
	}
	truncated := nudgeCount >= 1001 || sessionCount >= 1001
	countStr := fmt.Sprintf("%d", total)
	if truncated {
		countStr = "≥" + countStr
	}
	_, _ = fmt.Fprintf(stderr, "gc start: %s closed nudge/session beads detected — wisps-plane retention watchdog will prune automatically (24h TTL default; configure: [beads.policies.nudge].delete_after_close and [beads.policies.session].delete_after_close)\n", countStr)
}
