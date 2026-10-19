package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
)

// These tests drive the generated default work query through a fake bd and
// into the real claim loop: what a pool worker claims first is the
// highest-priority ready bead, FIFO by sling time within a priority, and the
// assignee compare-and-swap still decides every claim.

const priorityClaimRoute = "rig/worker"

func priorityClaimAgent() config.Agent {
	return config.Agent{Name: "worker", Dir: "rig", MinActiveSessions: intPtr(0), MaxActiveSessions: intPtr(3)}
}

func priorityClaimRow(id string, priority, day int, extra string) string {
	return fmt.Sprintf(`{"id":%q,"issue_type":"task","status":"open","priority":%d,"created_at":"2026-10-%02dT00:00:00Z"%s,"metadata":{"gc.routed_to":%q}}`, id, priority, day, extra, priorityClaimRoute)
}

// priorityClaimCAS is an assignee compare-and-swap over a fixed set of beads:
// a claim lands only on an unassigned bead.
type priorityClaimCAS struct {
	mu       sync.Mutex
	assignee map[string]string
	attempts []string
}

func (c *priorityClaimCAS) claim(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.attempts = append(c.attempts, beadID)
	if current := c.assignee[beadID]; current != "" {
		return beads.Bead{ID: beadID, Assignee: current, Status: "in_progress"}, false, nil
	}
	c.assignee[beadID] = assignee
	return beads.Bead{ID: beadID, Assignee: assignee, Status: "in_progress"}, true, nil
}

func runPriorityClaim(t *testing.T, bdScript string, cas *priorityClaimCAS, worker string) string {
	t.Helper()
	env := append(priorityClaimQueryEnv(t, bdScript), "GC_SESSION_NAME="+worker)
	agent := priorityClaimAgent()
	ops := hookClaimOps{
		Runner:   func(command, dir string) (string, error) { return shellWorkQueryWithEnv(command, dir, env) },
		Claim:    cas.claim,
		DrainAck: func(io.Writer) error { return nil },
	}
	var stdout, stderr bytes.Buffer
	code := doHookClaim(agent.EffectiveWorkQuery(), t.TempDir(), hookClaimOptions{
		Assignee:           worker,
		IdentityCandidates: []string{worker},
		RouteTargets:       []string{priorityClaimRoute},
		JSON:               true,
	}, ops, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doHookClaim() = %d, want 0; stderr=%s", code, stderr.String())
	}
	for id, owner := range cas.assignee {
		if owner == worker {
			return id
		}
	}
	return ""
}

func priorityClaimFakeBD(readyRows, ephemeralRows string) string {
	return `#!/bin/sh
set -eu
case "$1" in
  ready)
    case "$*" in
      *"--metadata-field gc.routed_to=` + priorityClaimRoute + `"*"--unassigned"*) printf '%s' '` + readyRows + `' ;;
      *) printf '[]' ;;
    esac
    ;;
  query)
    case "$*" in
      *"ephemeral=true AND status=open"*) printf '%s' '` + ephemeralRows + `' ;;
      *) printf '[]' ;;
    esac
    ;;
  *) printf '[]' ;;
esac
`
}

func TestHookClaimTakesHigherPriorityWispSlungLater(t *testing.T) {
	// `bd query` returns wisps in store order; the work query must rank them.
	wisps := "[" + strings.Join([]string{
		priorityClaimRow("p2-slung-first", 2, 15, `,"ephemeral":true`),
		priorityClaimRow("p1-slung-later", 1, 17, `,"ephemeral":true`),
		priorityClaimRow("p1-slung-second", 1, 16, `,"ephemeral":true`),
	}, ",") + "]"
	cas := &priorityClaimCAS{assignee: map[string]string{}}

	if got := runPriorityClaim(t, priorityClaimFakeBD("[]", wisps), cas, "worker-1"); got != "p1-slung-second" {
		t.Fatalf("claimed %q, want p1-slung-second (P1 before P2, earlier sling within P1); attempts=%v", got, cas.attempts)
	}
}

func TestHookClaimTakesHigherPriorityWorkflowRootOverOlderLowerPriorityStep(t *testing.T) {
	// The reader serves the canonical (priority, created_at, id) window; the
	// executable-before-anchor preference must not reorder across priorities.
	ready := "[" + strings.Join([]string{
		priorityClaimRow("p1-root", 1, 16, ``),
		priorityClaimRow("p2-step", 2, 15, ``),
	}, ",") + "]"
	ready = strings.Replace(ready, `"metadata":{"gc.routed_to"`, `"metadata":{"gc.kind":"workflow","gc.formula_contract":"graph.v2","gc.routed_to"`, 1)

	agent := priorityClaimAgent()
	out, err := shellWorkQueryWithEnv(agent.EffectiveWorkQuery(), t.TempDir(), priorityClaimQueryEnv(t, priorityClaimFakeBD(ready, "[]")))
	if err != nil {
		t.Fatalf("work query: %v", err)
	}
	first := strings.Index(out, `"p1-root"`)
	second := strings.Index(out, `"p2-step"`)
	if first < 0 || second < 0 || first > second {
		t.Fatalf("work query served %s, want p1-root ahead of the older p2-step", out)
	}
}

func TestHookClaimPriorityOrderKeepsTheAssigneeCAS(t *testing.T) {
	ready := "[" + strings.Join([]string{
		priorityClaimRow("p1-slung-first", 1, 16, ``),
		priorityClaimRow("p1-slung-second", 1, 17, ``),
		priorityClaimRow("p2-slung-earliest", 2, 15, ``),
	}, ",") + "]"
	cas := &priorityClaimCAS{assignee: map[string]string{"p1-slung-first": "worker-other"}}

	got := runPriorityClaim(t, priorityClaimFakeBD(ready, "[]"), cas, "worker-1")
	if got != "p1-slung-second" {
		t.Fatalf("claimed %q, want p1-slung-second after losing the CAS on p1-slung-first; attempts=%v", got, cas.attempts)
	}
	if owner := cas.assignee["p1-slung-first"]; owner != "worker-other" {
		t.Fatalf("p1-slung-first assignee = %q, want the CAS winner worker-other kept", owner)
	}
	if owner := cas.assignee["p2-slung-earliest"]; owner != "" {
		t.Fatalf("p2-slung-earliest assignee = %q, want it left unclaimed", owner)
	}
}

func priorityClaimQueryEnv(t *testing.T, bdScript string) []string {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(bdScript), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
	return []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GC_SESSION_ORIGIN=ephemeral",
	}
}
