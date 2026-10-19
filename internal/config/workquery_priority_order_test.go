package config

import (
	"fmt"
	"slices"
	"testing"
)

// The claim contract every worker-side ordering site must honor: the bead a
// pool worker is offered first is the highest-priority ready one (P0 before P1
// before P2), and within one priority the earliest-slung one. The routed
// reader already serves that order (TestEffectiveWorkQueryRoutedQueueRidesReaderPriorityOrder);
// these tests pin the reorderings layered on top of a reader.

// TestEffectiveWorkQueryAnchorDemotionStaysWithinPriorityBand pins that
// preferring executable rows over graph.v2 workflow roots never outranks
// priority: a P1 workflow root slung after a P2 step still comes first, while
// within one band the executable step still precedes the root (#5474) and
// equal rows keep the reader's FIFO order.
func TestEffectiveWorkQueryAnchorDemotionStaysWithinPriorityBand(t *testing.T) {
	a := Agent{Name: "worker", Dir: "hello-world"}
	root := func(id string, priority, day int) string {
		return fmt.Sprintf(`{"id":%q,"status":"open","priority":%d,"created_at":"2026-10-%02dT00:00:00Z","metadata":{"gc.kind":"workflow","gc.formula_contract":"graph.v2","gc.routed_to":"hello-world/worker"}}`, id, priority, day)
	}
	step := func(id string, priority, day int) string {
		return fmt.Sprintf(`{"id":%q,"status":"open","priority":%d,"created_at":"2026-10-%02dT00:00:00Z","metadata":{"gc.kind":"task","gc.routed_to":"hello-world/worker"}}`, id, priority, day)
	}
	// The reader's canonical (priority, created_at, id) window.
	window := `[` +
		root("p1-root", 1, 16) + `,` +
		step("p1-step-older", 1, 16) + `,` +
		step("p1-step-newer", 1, 17) + `,` +
		root("p2-root-older", 2, 14) + `,` +
		step("p2-step", 2, 15) + `]`
	out := runEffectiveWorkQuery(t, a, map[string]string{
		"GC_SESSION_ORIGIN": "ephemeral",
	}, `#!/bin/sh
set -eu
case "$1" in
  ready)
    case "$*" in
      *"--metadata-field gc.routed_to=hello-world/worker"*)
        printf '%s' '`+window+`'
        ;;
      *)
        printf '[]'
        ;;
    esac
    ;;
  *)
    printf '[]'
    ;;
esac
`)

	got := workQueryOutputIDOrder(t, out)
	want := []string{"p1-step-older", "p1-step-newer", "p1-root", "p2-step", "p2-root-older"}
	if !slices.Equal(got, want) {
		t.Fatalf("EffectiveWorkQuery() candidate order = %v, want %v", got, want)
	}
}

// TestEffectiveWorkQueryLegacyEphemeralTiersServePriorityFirst pins the bd
// 1.0.4 wisp tiers, which read `bd query` (unordered) and sort in jq: a P1 wisp
// slung after a P2 wisp is offered first, and the earlier of two P1 wisps wins.
func TestEffectiveWorkQueryLegacyEphemeralTiersServePriorityFirst(t *testing.T) {
	wisp := func(id, assignee string, priority, day int) string {
		return fmt.Sprintf(`{"id":%q,"issue_type":"task","status":"open","ephemeral":true,"assignee":%q,"priority":%d,"created_at":"2026-10-%02dT00:00:00Z","metadata":{"gc.routed_to":"hello-world/worker"}}`, id, assignee, priority, day)
	}
	for _, tc := range []struct {
		name     string
		assignee string
		env      map[string]string
	}{
		{name: "routed pool demand", env: map[string]string{"GC_SESSION_ORIGIN": "ephemeral"}},
		{name: "assigned ready", assignee: "worker-session", env: map[string]string{"GC_SESSION_ORIGIN": "ephemeral", "GC_SESSION_NAME": "worker-session"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := `[` +
				wisp("p2-slung-first", tc.assignee, 2, 15) + `,` +
				wisp("p1-slung-later", tc.assignee, 1, 17) + `,` +
				wisp("p1-slung-second", tc.assignee, 1, 16) + `]`
			out := runEffectiveWorkQuery(t, Agent{Name: "worker", Dir: "hello-world"}, tc.env, `#!/bin/sh
set -eu
case "$1" in
  query)
    case "$*" in
      *"ephemeral=true AND status=open"*)
        printf '%s' '`+rows+`'
        ;;
      *)
        printf '[]'
        ;;
    esac
    ;;
  *)
    printf '[]'
    ;;
esac
`)
			got := workQueryOutputIDOrder(t, out)
			if len(got) == 0 || got[0] != "p1-slung-second" {
				t.Fatalf("EffectiveWorkQuery() candidate order = %v, want the earlier-slung P1 wisp first", got)
			}
		})
	}
}
