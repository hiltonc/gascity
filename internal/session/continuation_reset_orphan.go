package session

import "strings"

// orphanedResetClearAttempts bounds ClearOrphanedContinuationReset's fenced
// write against a row other writers keep moving.
const orphanedResetClearAttempts = 3

// OrphanedContinuationReset reports that info's continuation_reset_pending is
// left over from a start that already happened, given that the caller observed
// info's runtime live. The marker means "the next start begins a fresh
// conversation"; once a committed row runs a runtime, every start that would
// consume it is past, and a wake request on it is served, not pending. A marker
// some pending request still owns is not orphaned: a restart request or its
// committed reset (the controller stops the runtime and starts fresh), a create
// claim (a start is on its way), and any row that is not committed awake or
// active.
func OrphanedContinuationReset(info Info) bool {
	if info.Closed || strings.TrimSpace(info.ContinuationResetPending) != "true" {
		return false
	}
	switch State(strings.TrimSpace(info.MetadataState)) {
	case StateActive, StateAwake:
	default:
		return false
	}
	return strings.TrimSpace(info.RestartRequested) != "true" &&
		strings.TrimSpace(info.ResetCommittedAt) == "" &&
		!info.PendingCreateClaim
}

// ClearOrphanedContinuationReset clears row id's continuation_reset_pending
// when OrphanedContinuationReset holds on a fresh read of the row, so a reset
// requested since the caller's read keeps its marker. The epoch is kept: the
// live runtime runs under it. It reports whether the clear was written. The
// caller owns the liveness half of the premise and must have observed the
// row's runtime live.
func (s *Store) ClearOrphanedContinuationReset(id string) (bool, error) {
	return s.UpdateMetadataFenced(id, orphanedResetClearAttempts, func(info Info, _ PersistedResponse) MetadataPatch {
		if !OrphanedContinuationReset(info) {
			return nil
		}
		return MetadataPatch{"continuation_reset_pending": ""}
	})
}
