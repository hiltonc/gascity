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

	// wispsPlaneRetentionTimeBudget is the wall-clock budget for a steady-state
	// retention pass. The sweep stops when the budget expires, regardless of how
	// many beads remain. Subsequent ticks drain the remainder.
	wispsPlaneRetentionTimeBudget = 5 * time.Second

	// wispsPlaneRetentionStartupTimeBudget is the time budget for the first
	// retention pass after startup. Kept small so order dispatch is never gated
	// on a bulk delete.
	wispsPlaneRetentionStartupTimeBudget = 1 * time.Second

	// wispsPlaneRetentionBatchSize is the number of eligible beads collected
	// into a single batch-delete call. Batching replaces the per-bead
	// deleteWorkflowBead round trip with one descendants check for the page and
	// one batch delete.
	wispsPlaneRetentionBatchSize = 50

	// mailBeadType is the bead type mail is stored as (cmd_mail.go). The
	// sweeps never delete it: closed mail addressed to a human is the archive.
	mailBeadType = "message"
)

// retentionSweepOpts configures a single retention sweep pass.
type retentionSweepOpts struct {
	deadline  time.Time
	budget    int
	batchSize int
	clockFn   func() time.Time // nil → time.Now
}

func (o retentionSweepOpts) clock() time.Time {
	if o.clockFn != nil {
		return o.clockFn()
	}
	return time.Now()
}

func (o retentionSweepOpts) pastDeadline() bool {
	if o.deadline.IsZero() {
		return false
	}
	return !o.clock().Before(o.deadline)
}

func (o retentionSweepOpts) effectiveBatchSize() int {
	if o.batchSize <= 0 {
		return 1
	}
	return o.batchSize
}

// retentionSweepResult reports what a single retention sweep did.
type retentionSweepResult struct {
	deleted   int
	remaining int
	timedOut  bool
}

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
// in batches. The opts deadline and budget bound the pass by time and count;
// whichever triggers first stops the sweep. A message bead is never deleted,
// even when it carries the nudge label.
func sweepClosedNudgeRetention(store beads.Store, now time.Time, deleteAfterClose time.Duration, opts retentionSweepOpts) (retentionSweepResult, error) {
	if store == nil || deleteAfterClose <= 0 || opts.budget <= 0 {
		return retentionSweepResult{}, nil
	}
	cutoff := now.Add(-deleteAfterClose)
	candidates, err := store.List(beads.ListQuery{
		Status:   "closed",
		Label:    nudgeBeadLabel,
		TierMode: beads.TierBoth,
	})
	if err != nil {
		return retentionSweepResult{}, fmt.Errorf("listing closed nudge beads for retention: %w", err)
	}

	var eligible []string
	for _, c := range candidates {
		if c.Type == mailBeadType {
			continue
		}
		if c.CreatedAt.IsZero() || !c.CreatedAt.Before(cutoff) {
			continue
		}
		eligible = append(eligible, c.ID)
	}

	deleted, timedOut, deleteErr := deleteBatchedWithDeadline(store, eligible, opts)
	remaining := len(eligible) - deleted
	if remaining < 0 {
		remaining = 0
	}
	return retentionSweepResult{deleted: deleted, remaining: remaining, timedOut: timedOut}, deleteErr
}

// sweepClosedSessionRetention deletes closed session beads past their TTL.
// It lists closed beads of type "session", filters by age, pre-filters those
// with children, and deletes the remainder in batches. The opts deadline and
// budget bound the pass by time and count.
func sweepClosedSessionRetention(store beads.Store, now time.Time, deleteAfterClose time.Duration, opts retentionSweepOpts) (retentionSweepResult, error) {
	if store == nil || deleteAfterClose <= 0 || opts.budget <= 0 {
		return retentionSweepResult{}, nil
	}
	cutoff := now.Add(-deleteAfterClose)
	candidates, err := store.List(beads.ListQuery{
		Status:   "closed",
		Type:     "session",
		TierMode: beads.TierBoth,
	})
	if err != nil {
		return retentionSweepResult{}, fmt.Errorf("listing closed session beads for retention: %w", err)
	}

	var eligible []string
	var filterErr error
	for _, c := range candidates {
		activity := c.UpdatedAt
		if activity.IsZero() {
			activity = c.CreatedAt
		}
		if activity.IsZero() || !activity.Before(cutoff) {
			continue
		}
		children, childErr := store.Children(c.ID, beads.IncludeClosed, beads.WithBothTiers)
		if childErr != nil {
			filterErr = errors.Join(filterErr, fmt.Errorf("listing children for session %q: %w", c.ID, childErr))
			continue
		}
		if len(children) > 0 {
			continue
		}
		eligible = append(eligible, c.ID)
	}

	deleted, timedOut, deleteErr := deleteBatchedWithDeadline(store, eligible, opts)
	remaining := len(eligible) - deleted
	if remaining < 0 {
		remaining = 0
	}
	return retentionSweepResult{deleted: deleted, remaining: remaining, timedOut: timedOut}, errors.Join(filterErr, deleteErr)
}

// deleteBatchedWithDeadline deletes IDs in pages of opts.batchSize, checking
// the deadline between pages. Each page uses deleteWorkflowBeadsBatch for the
// collective descendants check and batch delete. Returns the number deleted,
// whether the deadline stopped the sweep, and any accumulated errors.
func deleteBatchedWithDeadline(store beads.Store, ids []string, opts retentionSweepOpts) (deleted int, timedOut bool, err error) {
	bs := opts.effectiveBatchSize()
	budget := opts.budget
	var deleteErr error
	for len(ids) > 0 && deleted < budget {
		if opts.pastDeadline() {
			return deleted, true, deleteErr
		}
		pageSize := bs
		if pageSize > len(ids) {
			pageSize = len(ids)
		}
		if pageSize > budget-deleted {
			pageSize = budget - deleted
		}
		page := ids[:pageSize]
		ids = ids[pageSize:]

		if batchErr := deleteWorkflowBeadsBatch(store, page); batchErr != nil {
			if errors.Is(batchErr, errWorkflowDeleteLiveDescendants) {
				for _, id := range page {
					if err := deleteWorkflowBead(store, id); err != nil {
						if !errors.Is(err, errWorkflowDeleteLiveDescendants) {
							deleteErr = errors.Join(deleteErr, err)
						}
						continue
					}
					deleted++
				}
				continue
			}
			deleteErr = errors.Join(deleteErr, batchErr)
			continue
		}
		deleted += pageSize
	}
	return deleted, false, deleteErr
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
// session) past their configured TTL. Each pass is time-bounded: the first
// pass after startup uses a small budget so order dispatch is never blocked
// by a bulk delete; subsequent passes use the steady-state budget.
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

	timeBudget := wispsPlaneRetentionTimeBudget
	if !cr.wispsPlaneRetentionStartupDone {
		timeBudget = wispsPlaneRetentionStartupTimeBudget
		cr.wispsPlaneRetentionStartupDone = true
	}

	policy := wispsPlaneRetentionPolicyForConfig(cfg)
	budget := wispsPlaneRetentionDeleteBudget
	clockFn := cr.wispsPlaneRetentionClockFn
	deadline := now.Add(timeBudget)

	opts := retentionSweepOpts{
		deadline:  deadline,
		budget:    budget,
		batchSize: wispsPlaneRetentionBatchSize,
		clockFn:   clockFn,
	}

	var totalDeleted int
	var totalRemaining int
	var anyTimedOut bool
	var sweepErr error

	if policy.nudgeDeleteAfterClose > 0 {
		result, nudgeErr := sweepClosedNudgeRetention(cityStore, now, policy.nudgeDeleteAfterClose, opts)
		totalDeleted += result.deleted
		totalRemaining += result.remaining
		anyTimedOut = anyTimedOut || result.timedOut
		opts.budget -= result.deleted
		if opts.budget < 0 {
			opts.budget = 0
		}
		sweepErr = errors.Join(sweepErr, nudgeErr)
		if result.deleted > 0 && cr.stderr != nil {
			_, _ = fmt.Fprintf(cr.stderr, "%s: wisps-plane retention watchdog: deleted %d closed nudge bead(s)\n", cr.logPrefix, result.deleted)
		}
	}

	if policy.sessionDeleteAfterClose > 0 && opts.budget > 0 && !opts.pastDeadline() {
		result, sessionErr := sweepClosedSessionRetention(cityStore, now, policy.sessionDeleteAfterClose, opts)
		totalDeleted += result.deleted
		totalRemaining += result.remaining
		anyTimedOut = anyTimedOut || result.timedOut
		sweepErr = errors.Join(sweepErr, sessionErr)
		if result.deleted > 0 && cr.stderr != nil {
			_, _ = fmt.Fprintf(cr.stderr, "%s: wisps-plane retention watchdog: deleted %d closed session bead(s)\n", cr.logPrefix, result.deleted)
		}
	}

	if sweepErr != nil && cr.stderr != nil {
		_, _ = fmt.Fprintf(cr.stderr, "%s: wisps-plane retention watchdog: %v\n", cr.logPrefix, sweepErr)
	}

	if anyTimedOut || totalRemaining > 0 {
		if cr.stderr != nil {
			_, _ = fmt.Fprintf(cr.stderr, "%s: wisps-plane retention watchdog: %d bead(s) deleted, %d remain — backlog will drain over subsequent ticks\n",
				cr.logPrefix, totalDeleted, totalRemaining)
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
