package sourceworkflow

import (
	"reflect"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

func TestReadSourceBlockersKeepsOnlyUnsatisfiedBlockingDeps(t *testing.T) {
	store := beads.NewMemStoreFrom(0, []beads.Bead{
		{ID: "src", Title: "source", Type: "task", Status: "open"},
		{ID: "open", Title: "open", Type: "task", Status: "open"},
		{ID: "closed", Title: "closed", Type: "task", Status: "closed"},
		{ID: "outcome-blocked", Title: "closed blocked", Type: "task", Status: "closed", Metadata: map[string]string{"gc.work_outcome": "blocked"}},
		{ID: "waits", Title: "waits", Type: "task", Status: "open"},
		{ID: "related", Title: "related", Type: "task", Status: "open"},
		{ID: "attached-root", Title: "attached workflow", Type: "task", Status: "in_progress", Metadata: map[string]string{"gc.kind": "workflow"}},
	}, []beads.Dep{
		{IssueID: "src", DependsOnID: "open", Type: "blocks"},
		{IssueID: "src", DependsOnID: "closed", Type: "blocks"},
		{IssueID: "src", DependsOnID: "outcome-blocked", Type: "blocks"},
		{IssueID: "src", DependsOnID: "waits", Type: "waits-for"},
		{IssueID: "src", DependsOnID: "related", Type: "relates-to"},
		{IssueID: "src", DependsOnID: "attached-root", Type: "blocks"},
	})

	got, err := ReadSourceBlockers(store, store, "src")
	if err != nil {
		t.Fatalf("ReadSourceBlockers: %v", err)
	}
	gates := map[string]string{}
	for _, gate := range got.Gates {
		gates[gate.DependsOnID] = gate.Type
	}
	want := map[string]string{"open": "blocks", "outcome-blocked": "blocks", "waits": "waits-for"}
	if !reflect.DeepEqual(gates, want) {
		t.Fatalf("gates = %v, want %v", gates, want)
	}
	if len(got.Unprojected) != 0 {
		t.Fatalf("Unprojected = %v, want none", got.Unprojected)
	}
}

func TestReadSourceBlockersSetsAsideBlockersTheWorkflowStoreCannotResolve(t *testing.T) {
	work := beads.NewMemStoreFrom(0, []beads.Bead{
		{ID: "src", Title: "source", Type: "task", Status: "open"},
		{ID: "work-only", Title: "work only", Type: "task", Status: "open"},
		{ID: "shared", Title: "shared", Type: "task", Status: "open"},
	}, []beads.Dep{
		{IssueID: "src", DependsOnID: "work-only", Type: "blocks"},
		{IssueID: "src", DependsOnID: "shared", Type: "blocks"},
		{IssueID: "src", DependsOnID: "missing", Type: "blocks"},
	})
	graph := beads.NewMemStoreFrom(0, []beads.Bead{
		{ID: "shared", Title: "shared", Type: "task", Status: "open"},
	}, nil)

	got, err := ReadSourceBlockers(work, graph, "src")
	if err != nil {
		t.Fatalf("ReadSourceBlockers: %v", err)
	}
	if len(got.Gates) != 1 || got.Gates[0].DependsOnID != "shared" {
		t.Fatalf("Gates = %+v, want only shared", got.Gates)
	}
	if !reflect.DeepEqual(got.Unprojected, []string{"work-only", "missing"}) {
		t.Fatalf("Unprojected = %v, want [work-only missing]", got.Unprojected)
	}
}
