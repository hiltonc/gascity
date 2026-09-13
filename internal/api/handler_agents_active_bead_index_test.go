package api

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
)

// The bead half of the agent-list build must not scale with the fleet.
//
// The lookup it replaced was cheap per call and ruinous in aggregate: one
// Limit=1 query per (agent, candidate identity, rig), which on a 30-agent,
// 7-rig city is several hundred queries to build one response — and the
// response cache exists precisely because that build is too slow to repeat.
// Growing the fleet here and asserting the read count does NOT move is what
// keeps the per-agent shape from creeping back in.
func TestAgentListBeadReadsDoNotScaleWithTheFleet(t *testing.T) {
	pinBuckets(t, time.Hour)

	readsFor := func(t *testing.T, agents int) int {
		t.Helper()
		state := newFakeState(t)
		state.cfg.Agents = nil
		for i := 0; i < agents; i++ {
			state.cfg.Agents = append(state.cfg.Agents, config.Agent{
				Name:              "worker-" + strconv.Itoa(i),
				Dir:               "myrig",
				Provider:          "test-agent",
				MaxActiveSessions: intPtr(1),
			})
		}
		store := &countingStore{Store: beads.NewMemStore()}
		state.stores["myrig"] = store
		srv := New(state)

		out := listAgents(t, srv)
		if len(out.Body.Items) != agents {
			t.Fatalf("items = %d, want %d: the fixture did not declare the fleet it meant to", len(out.Body.Items), agents)
		}
		// EVERY bead read the build made, whatever shape it took: counting
		// only the fleet-wide one would stay flat while a per-agent lookup
		// fanned out alongside it.
		return store.activeBeadListCalls + store.listByAssigneeCalls
	}

	small := readsFor(t, 1)
	if small == 0 {
		t.Fatal("a build made no bead reads; the fixture is not exercising the lookup")
	}
	large := readsFor(t, 40)
	if large != small {
		t.Errorf("bead reads = %d for 40 agents and %d for 1; the lookup is back to one query per agent", large, small)
	}
}

// Collapsing the fan-out must not change which bead an agent is reported to
// be working on. The index resolves identities outermost and rigs innermost,
// the order the per-assignee walk used, so the first candidate identity that
// holds work anywhere still wins over a later identity that holds work in an
// earlier rig.
func TestActiveBeadIndexResolvesIdentitiesBeforeRigs(t *testing.T) {
	rigA := beads.NewMemStore()
	rigB := beads.NewMemStore()
	claim := func(t *testing.T, store beads.Store, title, assignee string) string {
		t.Helper()
		b, err := store.Create(beads.Bead{Title: title})
		if err != nil {
			t.Fatalf("Create(%s): %v", title, err)
		}
		status := "in_progress"
		if err := store.Update(b.ID, beads.UpdateOpts{Status: &status, Assignee: &assignee}); err != nil {
			t.Fatalf("Update(%s): %v", title, err)
		}
		return b.ID
	}
	// The session id holds work in the LATER rig; the session name holds work
	// in the earlier one. Identity order must win.
	wantedByID := claim(t, rigB, "held by session id", "sess-1")
	claim(t, rigA, "held by session name", "myrig--worker")

	idx := newActiveBeadIndex(map[string]beads.Store{"a-rig": rigA, "b-rig": rigB})

	if got := idx.lookup("", "sess-1", "myrig--worker"); got != wantedByID {
		t.Errorf("lookup = %q, want %q: the first identity with work must win over an earlier rig", got, wantedByID)
	}
	if got := idx.lookup("b-rig", "sess-1", "myrig--worker"); got != wantedByID {
		t.Errorf("rig-scoped lookup = %q, want %q", got, wantedByID)
	}
	if got := idx.lookup("", "nobody"); got != "" {
		t.Errorf("lookup for an identity with no work = %q, want empty", got)
	}
}

// A rig whose store cannot answer must cost the build nothing more than that
// rig's rows: the remaining rigs still resolve, rather than the whole list
// losing its active-bead column or the build failing.
func TestActiveBeadIndexSkipsARigThatCannotAnswer(t *testing.T) {
	healthy := beads.NewMemStore()
	b, err := healthy.Create(beads.Bead{Title: "work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	status, assignee := "in_progress", "sess-1"
	if err := healthy.Update(b.ID, beads.UpdateOpts{Status: &status, Assignee: &assignee}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	idx := newActiveBeadIndex(map[string]beads.Store{
		"a-broken": &failingListStore{Store: beads.NewMemStore()},
		"b-ok":     healthy,
	})
	if got := idx.lookup("", "sess-1"); got != b.ID {
		t.Errorf("lookup = %q, want %q: a rig that could not answer took the others down with it", got, b.ID)
	}
}

type failingListStore struct {
	beads.Store
}

func (s *failingListStore) List(beads.ListQuery) ([]beads.Bead, error) {
	return nil, errors.New("bd list: store unavailable")
}
