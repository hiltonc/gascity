package session

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

func orphanedResetMeta() map[string]string {
	return map[string]string{
		"state":                      string(StateAwake),
		"session_name":               "minion",
		"alias":                      "minion",
		"continuation_epoch":         "4",
		"continuation_reset_pending": "true",
	}
}

func TestOrphanedContinuationReset(t *testing.T) {
	cases := map[string]struct {
		move map[string]string
		want bool
	}{
		"awake row":                       {want: true},
		"active row":                      {move: map[string]string{"state": string(StateActive)}, want: true},
		"no marker":                       {move: map[string]string{"continuation_reset_pending": ""}},
		"restart requested":               {move: map[string]string{"restart_requested": "true"}},
		"reset committed by a restart":    {move: map[string]string{ResetCommittedAtKey: "2026-10-16T20:16:00Z"}},
		"wake request served":             {move: map[string]string{"wake_request": "explicit"}, want: true},
		"create claim held":               {move: map[string]string{"pending_create_claim": "true"}},
		"asleep row":                      {move: map[string]string{"state": string(StateAsleep)}},
		"drained row":                     {move: map[string]string{"state": string(StateDrained)}},
		"creating row":                    {move: map[string]string{"state": string(StateCreating)}},
		"draining row":                    {move: map[string]string{"state": string(StateDraining)}},
		"marker spelled other than true":  {move: map[string]string{"continuation_reset_pending": "yes"}},
		"restart requested on asleep row": {move: map[string]string{"state": string(StateAsleep), "restart_requested": "true"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			meta := orphanedResetMeta()
			for k, v := range c.move {
				meta[k] = v
			}
			info := infoFromPersistedBead(beads.Bead{ID: "s-1", Type: BeadType, Status: "open", Metadata: meta})
			if got := OrphanedContinuationReset(info); got != c.want {
				t.Fatalf("OrphanedContinuationReset = %v, want %v", got, c.want)
			}
		})
	}
}

// The clear decides again on the row it writes: a reset requested since the
// caller's read keeps its marker.
func TestStoreClearOrphanedContinuationReset(t *testing.T) {
	mem := beads.NewMemStore()
	b, err := mem.Create(beads.Bead{Type: BeadType, Labels: []string{LabelSession}, Metadata: orphanedResetMeta()})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	front := NewStore(beads.SessionStore{Store: mem})

	cleared, err := front.ClearOrphanedContinuationReset(b.ID)
	if err != nil || !cleared {
		t.Fatalf("ClearOrphanedContinuationReset = %v, %v; want cleared", cleared, err)
	}
	got, err := mem.Get(b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v := got.Metadata["continuation_reset_pending"]; v != "" {
		t.Fatalf("continuation_reset_pending = %q, want cleared", v)
	}
	if v := got.Metadata["continuation_epoch"]; v != "4" {
		t.Fatalf("continuation_epoch = %q, want 4 kept: the live runtime runs under it", v)
	}

	if err := mem.SetMetadataBatch(b.ID, map[string]string{"continuation_reset_pending": "true", "restart_requested": "true"}); err != nil {
		t.Fatalf("SetMetadataBatch: %v", err)
	}
	cleared, err = front.ClearOrphanedContinuationReset(b.ID)
	if err != nil || cleared {
		t.Fatalf("ClearOrphanedContinuationReset over a requested restart = %v, %v; want nothing written", cleared, err)
	}
	got, _ = mem.Get(b.ID)
	if v := got.Metadata["continuation_reset_pending"]; v != "true" {
		t.Fatalf("continuation_reset_pending = %q, want the requested restart's marker kept", v)
	}
}
