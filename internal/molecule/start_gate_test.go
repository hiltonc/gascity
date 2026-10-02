package molecule

import (
	"context"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/formula"
)

// startGateTestRecipe is a graph workflow: wf.a -> wf.b -> finalize, and the
// root waits on the finalizer as the compiler wires it.
func startGateTestRecipe() *formula.Recipe {
	return &formula.Recipe{
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
}

func storeDepTargets(t *testing.T, store beads.Store, issueID string) map[string]string {
	t.Helper()
	deps, err := store.DepList(issueID, "down")
	if err != nil {
		t.Fatalf("DepList(%s): %v", issueID, err)
	}
	out := make(map[string]string, len(deps))
	for _, dep := range deps {
		out[dep.DependsOnID] = dep.Type
	}
	return out
}

// A gated workflow gets one start bead carrying the gates. Only the root and
// the entry step wait on it; later steps wait through their needs (bgc-acn).
func TestInstantiateGatesTheWorkflowOnOneStartBead(t *testing.T) {
	store := beads.NewMemStore()
	prev := IsGraphApplyEnabled()
	SetGraphApplyEnabled(false)
	t.Cleanup(func() { SetGraphApplyEnabled(prev) })

	blocker, err := store.Create(beads.Bead{Title: "Blocker", Type: "task"})
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	waiter, err := store.Create(beads.Bead{Title: "Waited on", Type: "task"})
	if err != nil {
		t.Fatalf("create waiter: %v", err)
	}

	result, err := Instantiate(context.Background(), store, startGateTestRecipe(), Options{
		Gates: []beads.Dep{
			{IssueID: "src-1", DependsOnID: blocker.ID},
			{IssueID: "src-1", DependsOnID: waiter.ID, Type: "waits-for"},
		},
	})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}

	startID := result.IDMapping["wf"+StartGateStepSuffix]
	if startID == "" {
		t.Fatalf("no start bead in IDMapping %v", result.IDMapping)
	}
	start, err := store.Get(startID)
	if err != nil {
		t.Fatalf("Get start: %v", err)
	}
	if got := start.Metadata[beadmeta.KindMetadataKey]; got != beadmeta.KindStartGate {
		t.Errorf("start gc.kind = %q, want %q", got, beadmeta.KindStartGate)
	}
	for _, want := range []string{blocker.ID, waiter.ID, "src-1"} {
		if !strings.Contains(start.Title, want) {
			t.Errorf("start title %q does not name %s", start.Title, want)
		}
	}
	if got, want := storeDepTargets(t, store, startID), map[string]string{blocker.ID: "blocks", waiter.ID: "waits-for"}; !sameDeps(got, want) {
		t.Errorf("start deps = %v, want %v", got, want)
	}

	for _, step := range []string{"wf", "wf.a"} {
		deps := storeDepTargets(t, store, result.IDMapping[step])
		if deps[startID] != "blocks" {
			t.Errorf("%s deps = %v, want it to wait on the start bead %s", step, deps, startID)
		}
	}
	for _, step := range []string{"wf.b", "wf.workflow-finalize"} {
		if deps := storeDepTargets(t, store, result.IDMapping[step]); deps[startID] != "" {
			t.Errorf("%s deps = %v, want no edge to the start bead (it waits through its needs)", step, deps)
		}
	}
	for step, id := range result.IDMapping {
		if id == startID {
			continue
		}
		deps := storeDepTargets(t, store, id)
		if deps[blocker.ID] != "" || deps[waiter.ID] != "" {
			t.Errorf("%s carries a copied gate edge %v; only the start bead may", step, deps)
		}
	}
}

func TestInstantiateGraphApplyGatesTheWorkflowOnOneStartBead(t *testing.T) {
	store := &graphApplySpyStore{MemStore: beads.NewMemStore()}
	blocker, err := store.Create(beads.Bead{Title: "Blocker", Type: "task"})
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	prev := IsGraphApplyEnabled()
	SetGraphApplyEnabled(true)
	t.Cleanup(func() { SetGraphApplyEnabled(prev) })

	// A gate named twice adds one edge.
	if _, err := Instantiate(context.Background(), store, startGateTestRecipe(), Options{
		Gates: []beads.Dep{{DependsOnID: blocker.ID}, {DependsOnID: blocker.ID, Type: "blocks"}},
	}); err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if store.plan == nil {
		t.Fatal("ApplyGraphPlan was not called")
	}
	startKey := "wf" + StartGateStepSuffix
	assertGraphPlanEdgeCount(t, store.plan, startKey, "", blocker.ID, "blocks", 1)
	assertGraphPlanEdgeCount(t, store.plan, "wf", startKey, "", "blocks", 1)
	assertGraphPlanEdgeCount(t, store.plan, "wf.a", startKey, "", "blocks", 1)
	assertGraphPlanEdgeCount(t, store.plan, "wf.b", startKey, "", "blocks", 0)
	for _, step := range []string{"wf", "wf.a", "wf.b", "wf.workflow-finalize"} {
		assertGraphPlanEdgeCount(t, store.plan, step, "", blocker.ID, "blocks", 0)
	}
}

func TestGateRecipeIsIdempotent(t *testing.T) {
	recipe := startGateTestRecipe()
	gates := []beads.Dep{{DependsOnID: "blocker-1"}}
	first, err := GateRecipe(recipe, gates)
	if err != nil || len(first) != 1 {
		t.Fatalf("first GateRecipe = %v, %v; want one external dep", first, err)
	}
	steps, deps := len(recipe.Steps), len(recipe.Deps)
	second, err := GateRecipe(recipe, gates)
	if err != nil || len(second) != 0 {
		t.Fatalf("second GateRecipe = %v, %v; want nothing", second, err)
	}
	if len(recipe.Steps) != steps || len(recipe.Deps) != deps {
		t.Fatalf("second GateRecipe changed the recipe: steps %d -> %d, deps %d -> %d", steps, len(recipe.Steps), deps, len(recipe.Deps))
	}
}

// A legacy molecule links steps to the root by parent-child, so a root that
// waited on its own start bead would block it; refuse rather than deadlock.
func TestGateRecipeRefusesARecipeThatIsNotAGraphWorkflow(t *testing.T) {
	recipe := startGateTestRecipe()
	delete(recipe.Steps[0].Metadata, beadmeta.KindMetadataKey)
	if _, err := GateRecipe(recipe, []beads.Dep{{DependsOnID: "blocker-1"}}); err == nil {
		t.Fatal("GateRecipe on a non-workflow recipe = nil error, want refusal")
	}
}

func sameDeps(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
