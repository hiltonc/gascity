package sourceworkflow

// A workflow launched from a source bead waits for that bead's blockers.
//
// A plain bead sling routes the bead itself, and Ready already hides a bead with
// an open blocker. A formula sling routes the workflow's steps instead, and they
// carry none of the source bead's dependencies, so a blocked bead's workflow was
// Ready the moment it launched and the blockers held nothing (bgc-acn). The
// launch paths read the blockers here and gate every step on them at create
// time (molecule.Options.Gates), so the workflow stays out of every ready query
// and pool demand count until they close, with no session spawned or held.
//
// The gates are copied once, at launch. A blocker added to the source bead
// afterwards does not reach a workflow already running; add it to the
// workflow's steps by hand, or re-sling with --force.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
)

// SourceBlockers are the unsatisfied ready-blocking dependencies of a source
// bead.
type SourceBlockers struct {
	// Gates are the blockers the workflow's store resolves, in the source
	// bead's dependency order, each keeping its dependency type.
	Gates []beads.Dep
	// Unprojected are open blockers the workflow's store cannot resolve. A
	// dependency row may only name an id its own store holds, so the workflow
	// runs without these constraints.
	Unprojected []string
}

// ReadSourceBlockers reads sourceBeadID's unsatisfied ready-blocking
// dependencies from sourceStore and sorts them by whether workflowStore, the
// store the workflow's beads are created in, can hold an edge to them. A
// satisfied blocker is dropped: it constrains nothing, and neither does a
// workflow root the source waits on. A blocker sourceStore
// itself cannot find is Unprojected, since nothing here can tell whether it is
// satisfied. Any other read failure is returned, so a launch never proceeds on
// a blocker set it could not read.
func ReadSourceBlockers(sourceStore, workflowStore beads.Store, sourceBeadID string) (SourceBlockers, error) {
	var out SourceBlockers
	sourceBeadID = strings.TrimSpace(sourceBeadID)
	if sourceStore == nil || sourceBeadID == "" {
		return out, nil
	}
	if workflowStore == nil {
		workflowStore = sourceStore
	}
	deps, err := sourceStore.DepList(sourceBeadID, "down")
	if err != nil {
		return out, fmt.Errorf("listing blockers of %s: %w", sourceBeadID, err)
	}
	seen := make(map[string]bool, len(deps))
	for _, dep := range deps {
		blockerID := strings.TrimSpace(dep.DependsOnID)
		if !beads.IsReadyBlockingDependencyType(dep.Type) || blockerID == "" || blockerID == sourceBeadID || seen[blockerID] {
			continue
		}
		seen[blockerID] = true
		blocker, err := sourceStore.Get(blockerID)
		if errors.Is(err, beads.ErrNotFound) {
			out.Unprojected = append(out.Unprojected, blockerID)
			continue
		}
		if err != nil {
			return SourceBlockers{}, fmt.Errorf("reading blocker %s of %s: %w", blockerID, sourceBeadID, err)
		}
		// A workflow root the source waits on is the source's own attached
		// work (gc formula cook --attach wires that edge), never a
		// precondition: gating a new workflow on it would chain each launch
		// behind the last.
		if IsWorkflowRoot(blocker) || beads.DependencySatisfied(blocker.Status, blocker.Metadata[beadmeta.WorkOutcomeMetadataKey]) {
			continue
		}
		if workflowStore != sourceStore {
			if _, err := workflowStore.Get(blockerID); errors.Is(err, beads.ErrNotFound) {
				out.Unprojected = append(out.Unprojected, blockerID)
				continue
			} else if err != nil {
				return SourceBlockers{}, fmt.Errorf("resolving blocker %s of %s in the workflow store: %w", blockerID, sourceBeadID, err)
			}
		}
		out.Gates = append(out.Gates, beads.Dep{IssueID: sourceBeadID, DependsOnID: blockerID, Type: dep.Type})
	}
	return out, nil
}
