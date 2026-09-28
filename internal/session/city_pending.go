package session

import (
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"
)

// cityPendingProbeConcurrency bounds the city pending aggregate's per-session
// probe fan-out so a many-session city neither serializes expensive runtime
// probes nor floods the provider with unbounded concurrent captures.
const cityPendingProbeConcurrency = 8

// CityPendingEntry is one session currently awaiting a human decision.
type CityPendingEntry struct {
	SessionID string
	RequestID string
	Kind      string
}

// cityPendingProbe is one session's pending-probe outcome, collected by index
// so the concurrent aggregate can be reassembled in deterministic order.
type cityPendingProbe struct {
	entry     CityPendingEntry
	supported bool
	pending   bool
	err       error
}

// CityPending returns the sessions in infos that are currently awaiting a
// human decision, probing each candidate's runtime through PendingByName. It
// is the single implementation behind the city-wide pending snapshot, shared
// by GET /v0/city/{cityName}/pending and `gc session pending`.
//
// The probe set is active sessions plus legacy empty-state ("none") beads,
// which the codebase treats as active for upgrade/bootstrap cities; a live
// runtime predating the state-metadata field can still hold a pending
// decision and must not be dropped. Asleep, draining, creating, and closed
// sessions have no live runtime that could hold one and are skipped.
//
// Per-session probe failures are returned as human-readable probeErrors
// rather than failing the aggregate, so one gone runtime session does not
// blind the caller to the rest. Entries keep the order of infos after the
// state filter, regardless of probe completion order.
func (m *Manager) CityPending(infos []Info) (entries []CityPendingEntry, probeErrors []string) {
	// ListFromInfos takes a comma-separated state filter; StateNone is the
	// empty string, so this resolves to "active," — both states, with closed
	// beads still excluded by the status guard.
	stateFilter := strings.Join([]string{string(StateActive), string(StateNone)}, ",")
	sessions := m.ListFromInfos(infos, stateFilter, "")

	// Probe concurrently with bounded fan-out: a probe can be expensive (a
	// tmux pane capture), so a sequential sweep adds latency, and an unbounded
	// one floods the provider. PendingByName reuses each session's resolved
	// runtime name, skipping the per-session store lookup Pending(id) makes.
	probes := make([]cityPendingProbe, len(sessions))
	group := new(errgroup.Group)
	group.SetLimit(cityPendingProbeConcurrency)
	for i, sess := range sessions {
		i, sess := i, sess
		group.Go(func() error {
			pending, supported, err := m.PendingByName(sess.SessionName)
			probe := cityPendingProbe{supported: supported, pending: pending != nil, err: err}
			if pending != nil {
				probe.entry = CityPendingEntry{
					SessionID: sess.ID,
					RequestID: pending.RequestID,
					Kind:      pending.Kind,
				}
			}
			probes[i] = probe
			return nil
		})
	}
	_ = group.Wait()

	entries = make([]CityPendingEntry, 0, len(sessions))
	for i, sess := range sessions {
		probe := probes[i]
		if probe.err != nil {
			probeErrors = append(probeErrors, fmt.Sprintf("session %s: %v", sess.ID, probe.err))
			continue
		}
		if !probe.supported || !probe.pending {
			continue
		}
		entries = append(entries, probe.entry)
	}
	return entries, probeErrors
}
