package sling

import (
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
)

// readyWorkflowBeadIDs returns the Ready beads that belong to the workflow
// rootID: the root itself and every bead stamped with it as gc.root_bead_id.
func readyWorkflowBeadIDs(t *testing.T, store beads.Store, rootID string) []string {
	t.Helper()
	ready, err := store.Ready()
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	var ids []string
	for _, b := range ready {
		if b.ID == rootID || b.Metadata[beadmeta.RootBeadIDMetadataKey] == rootID {
			ids = append(ids, b.ID)
		}
	}
	return ids
}

func blockedSourceBead(t *testing.T, store beads.Store, blockerStatus string) (source, blocker beads.Bead) {
	t.Helper()
	blocker, err := store.Create(beads.Bead{Title: "blocker", Type: "task", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if blockerStatus == "closed" {
		if err := store.Close(blocker.ID); err != nil {
			t.Fatal(err)
		}
	}
	source, err = store.Create(beads.Bead{Title: "work", Type: "task", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DepAdd(source.ID, blocker.ID, "blocks"); err != nil {
		t.Fatal(err)
	}
	return source, blocker
}

// A formula slung onto a blocked bead must not be Ready work until the bead's
// blockers close: its steps are what a pool claims and counts as demand
// (bgc-acn).
func TestSlingGraphFormulaOnBlockedBeadWaitsForItsBlockers(t *testing.T) {
	deps := graphV2ConvoyFirstSlingTestConfig(t)
	source, blocker := blockedSourceBead(t, deps.Store, "open")
	a := config.Agent{Name: "mayor", MaxActiveSessions: intPtr(1)}

	result, err := DoSling(SlingOpts{Target: a, BeadOrFormula: source.ID, OnFormula: "graph-work"}, deps, deps.Store)
	if err != nil {
		t.Fatalf("DoSling: %v", err)
	}
	if result.WorkflowID == "" {
		t.Fatal("WorkflowID = empty, want a launched workflow")
	}
	if ready := readyWorkflowBeadIDs(t, deps.Store, result.WorkflowID); len(ready) != 0 {
		t.Fatalf("workflow beads Ready while %s is open: %v", blocker.ID, ready)
	}
	if !containsSubstring(result.BeadWarnings, blocker.ID) {
		t.Fatalf("BeadWarnings = %q, want a note naming blocker %s", result.BeadWarnings, blocker.ID)
	}

	if err := deps.Store.Close(blocker.ID); err != nil {
		t.Fatal(err)
	}
	if ready := readyWorkflowBeadIDs(t, deps.Store, result.WorkflowID); len(ready) == 0 {
		t.Fatalf("no workflow bead Ready after %s closed", blocker.ID)
	}
}

func TestSlingGraphFormulaOnBeadWithClosedBlockerIsReadyAtOnce(t *testing.T) {
	deps := graphV2ConvoyFirstSlingTestConfig(t)
	source, _ := blockedSourceBead(t, deps.Store, "closed")
	a := config.Agent{Name: "mayor", MaxActiveSessions: intPtr(1)}

	result, err := DoSling(SlingOpts{Target: a, BeadOrFormula: source.ID, OnFormula: "graph-work"}, deps, deps.Store)
	if err != nil {
		t.Fatalf("DoSling: %v", err)
	}
	if ready := readyWorkflowBeadIDs(t, deps.Store, result.WorkflowID); len(ready) == 0 {
		t.Fatal("no workflow bead Ready although the source's only blocker is closed")
	}
	if len(result.BeadWarnings) != 0 {
		t.Fatalf("BeadWarnings = %q, want none", result.BeadWarnings)
	}
}

func TestSlingDryRunNamesTheBlockersTheWorkflowWouldWaitFor(t *testing.T) {
	deps := graphV2ConvoyFirstSlingTestConfig(t)
	source, blocker := blockedSourceBead(t, deps.Store, "open")
	a := config.Agent{Name: "mayor", MaxActiveSessions: intPtr(1)}

	result, err := DoSling(SlingOpts{Target: a, BeadOrFormula: source.ID, OnFormula: "graph-work", DryRun: true}, deps, deps.Store)
	if err != nil {
		t.Fatalf("DoSling dry-run: %v", err)
	}
	if !containsSubstring(result.BeadWarnings, blocker.ID) {
		t.Fatalf("dry-run BeadWarnings = %q, want a note naming blocker %s", result.BeadWarnings, blocker.ID)
	}
	if live := liveGraphV2Roots(t, deps.Store); len(live) != 0 {
		t.Fatalf("dry-run launched %d workflow roots", len(live))
	}
}

// A plain bead sling routes the bead itself, which Ready already hides while
// it is blocked; it gains no note and no gate.
func TestSlingPlainBeadOnBlockedBeadIsUnchanged(t *testing.T) {
	deps := graphV2ConvoyFirstSlingTestConfig(t)
	source, _ := blockedSourceBead(t, deps.Store, "open")
	a := config.Agent{Name: "mayor", MaxActiveSessions: intPtr(1), DefaultSlingFormula: stringPtr("graph-work")}

	result, err := DoSling(SlingOpts{Target: a, BeadOrFormula: source.ID, NoFormula: true}, deps, deps.Store)
	if err != nil {
		t.Fatalf("DoSling --no-formula: %v", err)
	}
	if result.WorkflowID != "" {
		t.Fatalf("WorkflowID = %q, want a plain route", result.WorkflowID)
	}
	if len(result.BeadWarnings) != 0 {
		t.Fatalf("BeadWarnings = %q, want none", result.BeadWarnings)
	}
}

func containsSubstring(items []string, sub string) bool {
	for _, item := range items {
		if strings.Contains(item, sub) {
			return true
		}
	}
	return false
}
