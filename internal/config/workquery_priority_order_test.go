package config

import (
	"fmt"
	"slices"
	"strings"
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
// within one band the executable step still precedes the root and equal
// rows keep the reader's FIFO order.
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
// slung after a P2 wisp is offered first, the earlier of two P1 wisps wins, and
// a wisp with no priority ranks as P2, between a P1 and a P3.
func TestEffectiveWorkQueryLegacyEphemeralTiersServePriorityFirst(t *testing.T) {
	wisp := func(id, assignee, priority string, day int) string {
		return fmt.Sprintf(`{"id":%q,"issue_type":"task","status":"open","ephemeral":true,"assignee":%q%s,"created_at":"2026-10-%02dT00:00:00Z","metadata":{"gc.routed_to":"hello-world/worker"}}`, id, assignee, priority, day)
	}
	const p1, p2, p3, noPriority = `,"priority":1`, `,"priority":2`, `,"priority":3`, ``
	for _, tier := range []struct {
		name     string
		assignee string
		env      map[string]string
	}{
		{name: "routed pool demand", env: map[string]string{"GC_SESSION_ORIGIN": "ephemeral"}},
		{name: "assigned ready", assignee: "worker-session", env: map[string]string{"GC_SESSION_ORIGIN": "ephemeral", "GC_SESSION_NAME": "worker-session"}},
	} {
		for _, tc := range []struct {
			name   string
			rows   func(assignee string) []string
			wantID string
		}{
			{
				name: "P1 slung after P2",
				rows: func(assignee string) []string {
					return []string{
						wisp("p2-slung-first", assignee, p2, 15),
						wisp("p1-slung-later", assignee, p1, 17),
						wisp("p1-slung-second", assignee, p1, 16),
					}
				},
				wantID: "p1-slung-second",
			},
			{
				name: "no priority outranks an older P3",
				rows: func(assignee string) []string {
					return []string{
						wisp("p3-slung-first", assignee, p3, 14),
						wisp("no-priority", assignee, noPriority, 15),
					}
				},
				wantID: "no-priority",
			},
			{
				name: "P1 outranks an older wisp with no priority",
				rows: func(assignee string) []string {
					return []string{
						wisp("no-priority-slung-first", assignee, noPriority, 14),
						wisp("p1-slung-later", assignee, p1, 15),
					}
				},
				wantID: "p1-slung-later",
			},
		} {
			t.Run(tier.name+"/"+tc.name, func(t *testing.T) {
				rows := "[" + strings.Join(tc.rows(tier.assignee), ",") + "]"
				out := runEffectiveWorkQuery(t, Agent{Name: "worker", Dir: "hello-world"}, tier.env, `#!/bin/sh
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
				if len(got) == 0 || got[0] != tc.wantID {
					t.Fatalf("EffectiveWorkQuery() candidate order = %v, want %s first", got, tc.wantID)
				}
			})
		}
	}
}

// TestEffectiveWorkQueryAssignedWispsWithDependenciesServePriorityFirst pins
// the bd 1.0.4 assigned tier's dependency path: of two assigned wisps whose
// dependencies have all closed, the P1 slung after the P2 is served.
func TestEffectiveWorkQueryAssignedWispsWithDependenciesServePriorityFirst(t *testing.T) {
	wisp := func(id string, priority, day int) string {
		return fmt.Sprintf(`{"id":%q,"issue_type":"task","status":"open","ephemeral":true,"assignee":"worker-session","priority":%d,"dependency_count":1,"created_at":"2026-10-%02dT00:00:00Z","metadata":{"gc.routed_to":"hello-world/worker"}}`, id, priority, day)
	}
	rows := "[" + wisp("p2-slung-first", 2, 15) + "," + wisp("p1-slung-later", 1, 17) + "]"
	out := runEffectiveWorkQuery(t, Agent{Name: "worker", Dir: "hello-world"}, map[string]string{
		"GC_SESSION_ORIGIN": "ephemeral",
		"GC_SESSION_NAME":   "worker-session",
	}, `#!/bin/sh
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
  show)
    printf '[{"id":"%s","dependencies":[{"id":"dep-1","status":"closed","dependency_type":"blocks"}]}]' "$2"
    ;;
  *)
    printf '[]'
    ;;
esac
`)
	got := workQueryOutputIDOrder(t, out)
	if !slices.Equal(got, []string{"p1-slung-later"}) {
		t.Fatalf("EffectiveWorkQuery() candidate order = %v, want [p1-slung-later]", got)
	}
}
