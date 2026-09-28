package session

import (
	"fmt"
	"strings"

	"github.com/gastownhall/gascity/internal/runtime"
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

// CityPendingProbe is one probed session and its pending-probe outcome.
type CityPendingProbe struct {
	Info      Info
	Pending   *runtime.PendingInteraction
	Supported bool
	Err       error
}

// ProbeCityPending probes every session in infos that could be holding a
// pending decision, through PendingByName. It is the single probe behind the
// city-wide pending snapshot, shared by GET /v0/city/{cityName}/pending and
// `gc session pending`.
//
// The probe set is active sessions plus legacy empty-state ("none") beads,
// which the codebase treats as active for upgrade/bootstrap cities; a live
// runtime predating the state-metadata field can still hold a pending
// decision and must not be dropped. Asleep, draining, creating, and closed
// sessions have no live runtime that could hold one and are skipped. Rows keep
// the order of infos after the state filter, regardless of probe completion
// order.
func (m *Manager) ProbeCityPending(infos []Info) []CityPendingProbe {
	// ListFromInfos takes a comma-separated state filter; StateNone is the
	// empty string, so this resolves to "active," — both states, with closed
	// beads still excluded by the status guard.
	stateFilter := strings.Join([]string{string(StateActive), string(StateNone)}, ",")
	sessions := m.ListFromInfos(infos, stateFilter, "")

	// Probe concurrently with bounded fan-out: a probe can be expensive (a
	// tmux pane capture), so a sequential sweep adds latency, and an unbounded
	// one floods the provider. PendingByName reuses each session's resolved
	// runtime name, skipping the per-session store lookup Pending(id) makes.
	// Each goroutine writes its own slot, so the result stays in session order.
	rows := make([]CityPendingProbe, len(sessions))
	group := new(errgroup.Group)
	group.SetLimit(cityPendingProbeConcurrency)
	for i, sess := range sessions {
		group.Go(func() error {
			pending, supported, err := m.PendingByName(sess.SessionName)
			rows[i] = CityPendingProbe{Info: sess, Pending: pending, Supported: supported, Err: err}
			return nil
		})
	}
	_ = group.Wait()
	return rows
}

// CityPendingEntries reduces probe rows to the sessions awaiting a decision.
// Per-session probe failures are returned as human-readable probeErrors
// rather than failing the aggregate, so one gone runtime session does not
// blind the caller to the rest.
func CityPendingEntries(rows []CityPendingProbe) (entries []CityPendingEntry, probeErrors []string) {
	entries = make([]CityPendingEntry, 0, len(rows))
	for _, row := range rows {
		if row.Err != nil {
			probeErrors = append(probeErrors, fmt.Sprintf("session %s: %v", row.Info.ID, row.Err))
			continue
		}
		if !row.Supported || row.Pending == nil {
			continue
		}
		entries = append(entries, CityPendingEntry{
			SessionID: row.Info.ID,
			RequestID: row.Pending.RequestID,
			Kind:      row.Pending.Kind,
		})
	}
	return entries, probeErrors
}

// CityPending returns the sessions in infos that are currently awaiting a
// human decision: ProbeCityPending reduced by CityPendingEntries.
func (m *Manager) CityPending(infos []Info) (entries []CityPendingEntry, probeErrors []string) {
	return CityPendingEntries(m.ProbeCityPending(infos))
}
