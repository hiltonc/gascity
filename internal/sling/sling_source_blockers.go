package sling

import (
	"fmt"
	"strings"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/molecule"
	"github.com/gastownhall/gascity/internal/sourceworkflow"
)

// readSourceBlockers reads the blockers a workflow launched from sourceBeadID
// must wait on. See sourceworkflow.ReadSourceBlockers.
func readSourceBlockers(deps SlingDeps, sourceBeadID string) (sourceworkflow.SourceBlockers, error) {
	return sourceworkflow.ReadSourceBlockers(deps.Store, deps.graphStore(), sourceBeadID)
}

// noteSourceBlockers reports the blockers a just-created workflow waits on,
// and records on its root the open ones it cannot wait on. A launch that found
// an existing root created nothing, gated nothing, and is not reported.
func noteSourceBlockers(deps SlingDeps, mResult *molecule.Result, sourceBeadID string, blockers sourceworkflow.SourceBlockers, result *SlingResult) {
	if mResult == nil || mResult.Created == 0 {
		return
	}
	if len(blockers.Unprojected) > 0 {
		value := strings.Join(blockers.Unprojected, ",")
		if err := deps.graphStore().SetMetadata(mResult.RootID, beadmeta.SourceUnprojectedBlockersMetadataKey, value); err != nil {
			result.MetadataErrors = append(result.MetadataErrors,
				fmt.Sprintf("setting %s on %s: %v", beadmeta.SourceUnprojectedBlockersMetadataKey, mResult.RootID, err))
		}
	}
	result.BeadWarnings = append(result.BeadWarnings, sourceBlockerNotes("workflow "+mResult.RootID, "waits", sourceBeadID, blockers)...)
}

// sourceBlockerNotes renders what blockers do to subject, a workflow that
// waits or (in a dry run) would wait.
func sourceBlockerNotes(subject, waits, sourceBeadID string, blockers sourceworkflow.SourceBlockers) []string {
	var notes []string
	if len(blockers.Gates) > 0 {
		ids := make([]string, 0, len(blockers.Gates))
		for _, gate := range blockers.Gates {
			ids = append(ids, gate.DependsOnID)
		}
		notes = append(notes, fmt.Sprintf("note: %s %s for %s's open blockers (%s); no step is ready until they close", subject, waits, sourceBeadID, strings.Join(ids, ", ")))
	}
	if len(blockers.Unprojected) > 0 {
		notes = append(notes, fmt.Sprintf("warning: %s cannot wait for %s's blockers %s, which its store cannot resolve; it runs without them", subject, sourceBeadID, strings.Join(blockers.Unprojected, ", ")))
	}
	return notes
}
