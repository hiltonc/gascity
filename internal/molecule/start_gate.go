package molecule

import (
	"fmt"
	"strings"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/formula"
)

// StartGateStepSuffix is appended to the recipe root's step ID to name the
// start step GateRecipe adds.
const StartGateStepSuffix = ".start-gate"

// ApplyGates applies opts.Gates to recipe with GateRecipe and returns opts
// with the start bead's dependencies moved into ExternalDeps. With no gates
// it returns opts unchanged.
func ApplyGates(recipe *formula.Recipe, opts Options) (Options, error) {
	if len(opts.Gates) == 0 {
		return opts, nil
	}
	gated, err := GateRecipe(recipe, opts.Gates)
	if err != nil {
		return opts, err
	}
	opts.ExternalDeps = append(append([]ExternalDep(nil), opts.ExternalDeps...), gated...)
	opts.Gates = nil
	return opts, nil
}

// GateRecipe holds recipe back until gates, beads that already exist, are
// satisfied. It adds one start step, a start-gate control bead that waits on
// every gate itself, and makes the recipe's root and its entry steps (the
// steps that wait on no other step) wait on that start bead. Later steps wait
// through their needs and gain no edges. The control dispatcher closes the
// start bead once its dependencies are satisfied, so nothing the recipe
// creates reads Ready before then and no session is held for it. The root
// stays gated so a released root is never claimable demand either.
//
// A blocker found after launch is one dependency added to the start bead.
//
// The edges from the start bead to the gates are returned as ExternalDeps for
// Instantiate, since the gates are outside the recipe. GateRecipe edits recipe
// in place and must run before graph routing so the start bead is routed to
// the control dispatcher. Only a graph workflow (root gc.kind=workflow) can
// be gated: its steps reach the root by metadata, never parent-child, so a
// root waiting on its own start bead cannot block that bead. A recipe that
// already has its start step is left alone.
func GateRecipe(recipe *formula.Recipe, gates []beads.Dep) ([]ExternalDep, error) {
	if recipe == nil || len(recipe.Steps) == 0 {
		return nil, fmt.Errorf("gating a recipe: recipe has no steps")
	}
	root := recipe.Steps[0]
	rootID := strings.TrimSpace(root.ID)
	if rootID == "" || root.Metadata[beadmeta.KindMetadataKey] != beadmeta.KindWorkflow {
		return nil, fmt.Errorf("gating recipe %q: only a graph workflow can wait on blockers", recipe.Name)
	}
	startID := rootID + StartGateStepSuffix
	stepIDs := make(map[string]bool, len(recipe.Steps))
	for _, step := range recipe.Steps {
		if step.ID == startID {
			return nil, nil
		}
		stepIDs[step.ID] = true
	}

	var external []ExternalDep
	var gateIDs []string
	sourceIDs := map[string]bool{}
	var sources []string
	seen := map[string]bool{}
	for _, gate := range gates {
		gateID := strings.TrimSpace(gate.DependsOnID)
		if gateID == "" || seen[gateID] {
			continue
		}
		seen[gateID] = true
		gateIDs = append(gateIDs, gateID)
		external = append(external, ExternalDep{StepID: startID, DependsOnID: gateID, Type: externalDepType(gate.Type)})
		if source := strings.TrimSpace(gate.IssueID); source != "" && !sourceIDs[source] {
			sourceIDs[source] = true
			sources = append(sources, source)
		}
	}
	if len(external) == 0 {
		return nil, nil
	}

	waitsOnStep := make(map[string]bool, len(recipe.Steps))
	for _, dep := range recipe.Deps {
		if beads.IsReadyBlockingDependencyType(dep.Type) && stepIDs[dep.DependsOnID] && dep.DependsOnID != rootID {
			waitsOnStep[dep.StepID] = true
		}
	}
	gatedSteps := []string{rootID}
	for _, step := range recipe.Steps[1:] {
		if waitsOnStep[step.ID] || step.Metadata[beadmeta.KindMetadataKey] == beadmeta.KindSpec {
			continue
		}
		gatedSteps = append(gatedSteps, step.ID)
	}

	title := "Wait for " + strings.Join(gateIDs, ", ")
	if len(sources) > 0 {
		title += " (blockers of " + strings.Join(sources, ", ") + ")"
	}
	recipe.Steps = append(recipe.Steps, formula.RecipeStep{
		ID:    startID,
		Title: title,
		Description: fmt.Sprintf("This workflow starts once %s close. Its first steps and its root wait on this bead, "+
			"and the control dispatcher closes it when every bead it depends on is satisfied; nothing needs to claim it.\n\n"+
			"To hold the workflow on another bead, make this bead depend on it: bd dep add <this bead> <blocker>. "+
			"Once this bead has closed the workflow has started, and a new dependency here holds nothing.",
			strings.Join(gateIDs, ", ")),
		Type:     "task",
		Metadata: map[string]string{beadmeta.KindMetadataKey: beadmeta.KindStartGate},
	})
	for _, stepID := range gatedSteps {
		recipe.Deps = append(recipe.Deps, formula.RecipeDep{StepID: stepID, DependsOnID: startID, Type: "blocks"})
	}
	return external, nil
}

// externalDepType is an ExternalDep's effective type: "blocks" when unset.
func externalDepType(t string) string {
	if t == "" {
		return "blocks"
	}
	return t
}
