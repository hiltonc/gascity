package api

import (
	"strings"

	"github.com/gastownhall/gascity/internal/beads"
)

// activeBeadIndex answers "which bead is this identity working on?" for a
// whole agent-list build from one query per rig, instead of one query per
// (agent, identity, rig) triple.
//
// findActiveBeadForAssignees is cheap per call — Limit=1 on an indexed
// (assignee, status) pair — but GET /agents calls it once per pool-expanded
// agent, with three or four candidate identities each, against every rig
// store. On a 30-agent, 7-rig city that is several hundred bead queries to
// build one response, which dominates the build the response cache exists to
// avoid rebuilding. The identities are not known until the agents are walked,
// but the ANSWER set is: the in_progress beads of each rig, which is bounded
// by how much work the fleet has claimed rather than by how many beads exist.
// One filtered query per rig therefore collects every answer the build can
// need, and the per-agent lookups become map reads.
//
// The index is built once per response build and thrown away with it; it is
// never cached beyond the body it produced, so it cannot serve a stale
// assignment to a later request.
type activeBeadIndex struct {
	// byRig maps rig name to assignee to the bead that assignee is working
	// on, keeping the most recently created when an assignee holds several.
	byRig map[string]map[string]string
	// rigOrder is the deduplicated rig order lookups fall through, matching
	// the order findActiveBeadForAssigneesWithFreshness walks.
	rigOrder []string
}

// newActiveBeadIndex reads each rig's in_progress beads once and folds them
// into assignee -> bead. Stores that expose the read-model cache are asked
// for it first, exactly as the per-agent path does; a rig whose read fails is
// left absent, and lookups against it return "" rather than failing the build.
func newActiveBeadIndex(stores map[string]beads.Store) *activeBeadIndex {
	idx := &activeBeadIndex{
		byRig:    make(map[string]map[string]string, len(stores)),
		rigOrder: sortedRigNames(stores),
	}
	query := beads.ListQuery{
		Status: "in_progress",
		Sort:   beads.SortCreatedDesc,
	}
	for _, rn := range idx.rigOrder {
		store := stores[rn]
		if store == nil {
			continue
		}
		matches, ok := cachedOrListed(store, query)
		if !ok {
			continue
		}
		byAssignee := make(map[string]string, len(matches))
		for _, bead := range matches {
			assignee := strings.TrimSpace(bead.Assignee)
			if assignee == "" {
				continue
			}
			// SortCreatedDesc means the first row for an assignee is the one
			// a Limit=1 query would have returned, so first wins.
			if _, seen := byAssignee[assignee]; !seen {
				byAssignee[assignee] = bead.ID
			}
		}
		idx.byRig[rn] = byAssignee
	}
	return idx
}

// cachedOrListed serves query from the store's read-model cache when it has
// one and it is clean, falling back to the backing store. It mirrors the
// cache-first order findActiveBeadForAssigneesWithFreshness uses for its
// non-live reads.
func cachedOrListed(store beads.Store, query beads.ListQuery) ([]beads.Bead, bool) {
	if cached, ok := store.(cachedListStore); ok {
		if matches, cacheOK := cached.CachedList(query); cacheOK {
			return matches, true
		}
	}
	matches, err := store.List(query)
	if err != nil {
		return nil, false
	}
	return matches, true
}

// lookup returns the first in_progress bead held by any of assignees, walking
// identities outermost and rigs innermost so it resolves to the same bead
// findActiveBeadForAssignees would have returned. rig scopes the walk to a
// single rig when it names one the index knows.
func (idx *activeBeadIndex) lookup(rig string, assignees ...string) string {
	if idx == nil {
		return ""
	}
	rigNames := idx.rigOrder
	if rig != "" {
		if _, ok := idx.byRig[rig]; ok {
			rigNames = []string{rig}
		}
	}
	seen := make(map[string]bool, len(assignees))
	for _, assignee := range assignees {
		assignee = strings.TrimSpace(assignee)
		if assignee == "" || seen[assignee] {
			continue
		}
		seen[assignee] = true
		for _, rn := range rigNames {
			if id := idx.byRig[rn][assignee]; id != "" {
				return id
			}
		}
	}
	return ""
}
