package dispatch

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/formula"
	"github.com/gastownhall/gascity/internal/molecule"
)

func readyIDs(t *testing.T, store beads.Store) []string {
	t.Helper()
	ready, err := store.Ready()
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	ids := make([]string, 0, len(ready))
	for _, b := range ready {
		ids = append(ids, b.ID)
	}
	return ids
}

// gatedWorkflow instantiates a two-step graph workflow gated on blocker and
// returns the bead ids by step. The root waits on its finalizer, as the
// compiler wires it.
func gatedWorkflow(t *testing.T, store beads.Store, blockerID string) map[string]string {
	t.Helper()
	prev := molecule.IsGraphApplyEnabled()
	molecule.SetGraphApplyEnabled(false)
	t.Cleanup(func() { molecule.SetGraphApplyEnabled(prev) })
	recipe := &formula.Recipe{
		Name: "wf",
		Steps: []formula.RecipeStep{
			{ID: "wf", Title: "Workflow", Type: "task", IsRoot: true, Metadata: map[string]string{beadmeta.KindMetadataKey: beadmeta.KindWorkflow}},
			{ID: "wf.a", Title: "First", Type: "task"},
			{ID: "wf.b", Title: "Second", Type: "task"},
			{ID: "wf.workflow-finalize", Title: "Finalize workflow", Type: "task", Metadata: map[string]string{beadmeta.KindMetadataKey: beadmeta.KindWorkflowFinalize}},
		},
		Deps: []formula.RecipeDep{
			{StepID: "wf.b", DependsOnID: "wf.a", Type: "blocks"},
			{StepID: "wf.workflow-finalize", DependsOnID: "wf.b", Type: "blocks"},
			{StepID: "wf", DependsOnID: "wf.workflow-finalize", Type: "blocks"},
		},
	}
	result, err := molecule.Instantiate(context.Background(), store, recipe, molecule.Options{
		Gates: []beads.Dep{{IssueID: "src", DependsOnID: blockerID}},
	})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	return result.IDMapping
}

// The start bead is the only thing a gated workflow shows a work query, and
// only once its blockers close; the dispatcher closing it releases the entry
// step with no session involved (bgc-acn).
func TestStartGateReleasesTheWorkflowWhenItsBlockersClose(t *testing.T) {
	store := beads.NewMemStore()
	blocker, err := store.Create(beads.Bead{Title: "blocker", Type: "task"})
	if err != nil {
		t.Fatal(err)
	}
	ids := gatedWorkflow(t, store, blocker.ID)
	startID := ids["wf"+molecule.StartGateStepSuffix]

	if got := readyIDs(t, store); !slices.Equal(got, []string{blocker.ID}) {
		t.Fatalf("Ready with the blocker open = %v, want only the blocker %s", got, blocker.ID)
	}

	if err := store.Close(blocker.ID); err != nil {
		t.Fatal(err)
	}
	if got := readyIDs(t, store); !slices.Equal(got, []string{startID}) {
		t.Fatalf("Ready after the blocker closed = %v, want only the start bead %s", got, startID)
	}

	start, err := store.Get(startID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ProcessControl(store, start, ProcessOptions{})
	if err != nil {
		t.Fatalf("ProcessControl(start gate): %v", err)
	}
	if !result.Processed || result.Action != "start-gate-open" {
		t.Fatalf("ProcessControl result = %+v, want processed start-gate-open", result)
	}
	start, err = store.Get(startID)
	if err != nil {
		t.Fatal(err)
	}
	if start.Status != "closed" || start.Metadata[beadmeta.OutcomeMetadataKey] != beadmeta.OutcomePass {
		t.Fatalf("start bead status=%s outcome=%q, want closed pass", start.Status, start.Metadata[beadmeta.OutcomeMetadataKey])
	}
	if got := readyIDs(t, store); !slices.Equal(got, []string{ids["wf.a"]}) {
		t.Fatalf("Ready after the start gate opened = %v, want the entry step %s", got, ids["wf.a"])
	}
}

// A blocker found after launch is one dependency on the start bead, and holds
// the gate even if the dispatcher reads it before Ready catches up.
func TestStartGateHoldsForABlockerAddedAfterLaunch(t *testing.T) {
	store := beads.NewMemStore()
	blocker, err := store.Create(beads.Bead{Title: "blocker", Type: "task"})
	if err != nil {
		t.Fatal(err)
	}
	late, err := store.Create(beads.Bead{Title: "late blocker", Type: "task"})
	if err != nil {
		t.Fatal(err)
	}
	ids := gatedWorkflow(t, store, blocker.ID)
	startID := ids["wf"+molecule.StartGateStepSuffix]

	if err := store.DepAdd(startID, late.ID, "blocks"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(blocker.ID); err != nil {
		t.Fatal(err)
	}
	if got := readyIDs(t, store); slices.Contains(got, startID) || slices.Contains(got, ids["wf.a"]) {
		t.Fatalf("Ready = %v; the late blocker %s should still hold the workflow", got, late.ID)
	}

	start, err := store.Get(startID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessControl(store, start, ProcessOptions{}); !errors.Is(err, ErrControlPending) {
		t.Fatalf("ProcessControl with a late blocker open = %v, want ErrControlPending", err)
	}
	if start, _ = store.Get(startID); start.Status == "closed" {
		t.Fatal("start bead closed while its late blocker is open")
	}
}

// A blocker closed with gc.work_outcome=blocked does not satisfy, as Ready
// already reads it.
func TestStartGateStaysShutForABlockerClosedBlocked(t *testing.T) {
	store := beads.NewMemStore()
	blocker, err := store.Create(beads.Bead{Title: "blocker", Type: "task"})
	if err != nil {
		t.Fatal(err)
	}
	ids := gatedWorkflow(t, store, blocker.ID)
	startID := ids["wf"+molecule.StartGateStepSuffix]
	if err := store.Update(blocker.ID, beads.UpdateOpts{Metadata: map[string]string{beadmeta.WorkOutcomeMetadataKey: beadmeta.WorkOutcomeBlocked}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(blocker.ID); err != nil {
		t.Fatal(err)
	}
	start, err := store.Get(startID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessControl(store, start, ProcessOptions{}); !errors.Is(err, ErrControlPending) {
		t.Fatalf("ProcessControl with a blocked-outcome blocker = %v, want ErrControlPending", err)
	}
}
