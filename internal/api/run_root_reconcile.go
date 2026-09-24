package api

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runproj"
	"github.com/gastownhall/gascity/internal/sourceworkflow"
)

// The Run resource folds the event log, so a workflow root whose close never
// reached the log reads active for as long as the log is the only witness.
// That has happened: gcd-thjlcv, gcd-q4s2g4 and gcd-jj98s closed with
// gc.outcome=pass while their last logged root event still said open. The event
// log stays the hot path; this reconcile only asks the owning store about a
// non-terminal workflow root the log has been silent on for runRootReconcileAge,
// and lets the store's terminal row stand in for the one the log lost.
const (
	// runRootReconcileAge is how long a non-terminal root may go without a
	// logged update before its status is confirmed against the store. A close
	// the event stream does deliver lands well inside it.
	runRootReconcileAge = 5 * time.Minute
	// runRootRecheckInterval bounds how often a stale root the store still
	// reports non-terminal is asked again.
	runRootRecheckInterval = time.Minute
)

// runRootReconciler memoizes store answers for stale run roots. A terminal
// answer is kept until the projection carries a newer row for the root; a
// non-terminal answer only defers the next check.
type runRootReconciler struct {
	mu sync.Mutex
	// now is the clock; nil means time.Now.
	now      func() time.Time
	terminal map[string]terminalRunRoot
	checked  map[string]time.Time
}

// terminalRunRoot is a store-confirmed terminal root and the update time of the
// projected row it replaced. A projected row touched after projectedAt is news
// the log did deliver (the real close, or a reopen), so it supersedes the memo.
type terminalRunRoot struct {
	row         beads.Bead
	projectedAt time.Time
}

func (r *runRootReconciler) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// reconcileRunRoots returns projected with the store's terminal row in place of
// every stale non-terminal workflow root the store reports closed. projected is
// a published, shared snapshot, so it is copied before the first replacement
// and never written. failed reports that a store read failed, leaving at least
// one root's projected row unconfirmed.
func (s *Server) reconcileRunRoots(projected []beads.Bead) (reconciled []beads.Bead, failed bool) {
	r := &s.runRoots
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminal == nil {
		r.terminal = make(map[string]terminalRunRoot)
		r.checked = make(map[string]time.Time)
	}
	now := r.clock()

	reconciled = projected
	copied := false
	replace := func(i int, root beads.Bead) {
		if !copied {
			reconciled = append([]beads.Bead(nil), projected...)
			copied = true
		}
		reconciled[i] = root
	}
	for i := range projected {
		b := projected[i]
		if b.Status == "closed" || !sourceworkflow.IsWorkflowRoot(b) {
			continue
		}
		touched := lastTouched(b)
		if memo, ok := r.terminal[b.ID]; ok {
			if touched.After(memo.projectedAt) {
				delete(r.terminal, b.ID)
			} else {
				replace(i, memo.row)
			}
			continue
		}
		if now.Sub(touched) < runRootReconcileAge {
			continue
		}
		if at, ok := r.checked[b.ID]; ok && now.Sub(at) < runRootRecheckInterval {
			continue
		}
		store := s.runRootStore(b)
		if store == nil {
			continue
		}
		current, err := store.Get(b.ID)
		if err != nil {
			if !errors.Is(err, beads.ErrNotFound) {
				log.Printf("api: runs: confirming status of run root %s against its store: %v", b.ID, err)
				failed = true
			}
			continue
		}
		if current.Status != "closed" {
			r.checked[b.ID] = now
			continue
		}
		delete(r.checked, b.ID)
		r.terminal[b.ID] = terminalRunRoot{row: current, projectedAt: touched}
		replace(i, current)
	}
	return reconciled, failed
}

// runRootStore returns the store that owns a workflow root: the one its
// gc.root_store_ref names, else the one its id prefix routes to. Nil when
// neither resolves.
func (s *Server) runRootStore(root beads.Bead) beads.Store {
	if kind, ref, ok := runproj.ScopeFromRootStoreRef(root.Metadata[beadmeta.RootStoreRefMetadataKey]); ok {
		if kind == "city" {
			return s.state.CityBeadStore()
		}
		return s.state.BeadStore(ref)
	}
	return s.resolveStoreByPrefix(beadPrefix(root.ID))
}

// lastTouched is a bead's last update time, falling back to its creation time
// for a row that never carried one.
func lastTouched(b beads.Bead) time.Time {
	if !b.UpdatedAt.IsZero() {
		return b.UpdatedAt
	}
	return b.CreatedAt
}
