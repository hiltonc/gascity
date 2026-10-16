package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
)

const (
	routeAsOwner    = "reviewer"
	routeAsBorrower = "codex-reviewer"
)

func routeAsAgents() (owner, borrower config.Agent) {
	owner = config.Agent{Name: routeAsOwner, StartCommand: "true", MaxActiveSessions: intPtr(3)}
	borrower = config.Agent{Name: routeAsBorrower, StartCommand: "true", MaxActiveSessions: intPtr(1), RouteAs: routeAsOwner}
	return owner, borrower
}

func TestHookClaimRouteTargetsAcceptForeignRouteOnlyWithRouteAs(t *testing.T) {
	_, borrower := routeAsAgents()
	shared := beads.Bead{ID: "ga-shared", Status: "open", Metadata: beads.StringMap{"gc.routed_to": routeAsOwner}}
	own := beads.Bead{ID: "ga-own", Status: "open", Metadata: beads.StringMap{"gc.routed_to": routeAsBorrower}}
	now := time.Now()

	plain := borrower
	plain.RouteAs = ""
	plainTargets := hookClaimAgentRouteTargets(&plain, plain.QualifiedName(), "")
	if hookCandidateClaimable(shared, plainTargets, now) {
		t.Fatalf("agent without route_as claimed a bead routed to %q: targets=%v", routeAsOwner, plainTargets)
	}

	targets := hookClaimAgentRouteTargets(&borrower, borrower.QualifiedName(), "")
	if !hookCandidateClaimable(shared, targets, now) {
		t.Fatalf("route_as agent refused its shared queue: targets=%v", targets)
	}
	if !hookCandidateClaimable(own, targets, now) {
		t.Fatalf("route_as agent refused work routed to itself: targets=%v", targets)
	}
}

// casClaimStore is the bd assignee compare-and-swap in miniature: a claim wins
// only while the bead is unassigned.
type casClaimStore struct {
	mu       sync.Mutex
	assignee string
	attempts []string
}

func (s *casClaimStore) claim(_ context.Context, _ string, _ []string, beadID, assignee string) (beads.Bead, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, assignee)
	if s.assignee != "" {
		return beads.Bead{ID: beadID, Status: "in_progress", Assignee: s.assignee}, false, nil
	}
	s.assignee = assignee
	return beads.Bead{ID: beadID, Status: "in_progress", Assignee: assignee, Metadata: beads.StringMap{"gc.routed_to": routeAsOwner}}, true, nil
}

func TestRouteAsClaimsStayAtomicAcrossPools(t *testing.T) {
	owner, borrower := routeAsAgents()
	store := &casClaimStore{}
	runner := func(string, string) (string, error) {
		return `[{"id":"ga-shared","status":"open","metadata":{"gc.routed_to":"` + routeAsOwner + `"}}]`, nil
	}
	sessions := []struct {
		assignee string
		agent    config.Agent
	}{
		{"reviewer-1", owner},
		{"codex-reviewer-1", borrower},
	}

	results := make([]hookClaimJSONResult, len(sessions))
	var wg sync.WaitGroup
	for i, s := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			opts := hookClaimOptions{
				Assignee:           s.assignee,
				IdentityCandidates: []string{s.assignee},
				RouteTargets:       hookClaimAgentRouteTargets(&s.agent, s.agent.QualifiedName(), ""),
				JSON:               true,
			}
			ops := hookClaimOps{
				Runner: runner,
				Claim:  store.claim,
				ListContinuation: func(context.Context, string, []string, string, string) ([]beads.Bead, error) {
					return nil, nil
				},
			}
			var stdout bytes.Buffer
			doHookClaim("bd ready --json", "/tmp/work", opts, ops, &stdout, io.Discard)
			if err := json.Unmarshal(stdout.Bytes(), &results[i]); err != nil {
				t.Errorf("%s: stdout is not JSON: %v\nraw: %s", s.assignee, err, stdout.String())
			}
		}()
	}
	wg.Wait()

	var winners []string
	for i, r := range results {
		if r.Reason == "claimed" {
			winners = append(winners, sessions[i].assignee)
			if r.BeadID != "ga-shared" || r.Assignee != store.assignee {
				t.Errorf("%s claim result = %+v, want ga-shared assigned to %q", sessions[i].assignee, r, store.assignee)
			}
		}
	}
	if len(store.attempts) != len(sessions) {
		t.Fatalf("claim attempts = %v, want one per pool: both must see the shared bead as claimable", store.attempts)
	}
	if len(winners) != 1 {
		t.Fatalf("claim winners = %v, want exactly one (results=%+v)", winners, results)
	}
}

// TestBuildDesiredState_RouteAsBeadWakesOnlyItsRoutedPool pins the demand cost
// of route_as: default demand is counted per route, so one ready bead starts a
// seat in the pool it is routed to and never one in each pool.
func TestBuildDesiredState_RouteAsBeadWakesOnlyItsRoutedPool(t *testing.T) {
	for _, routedTo := range []string{routeAsOwner, routeAsBorrower} {
		t.Run(routedTo, func(t *testing.T) {
			store := beads.NewMemStore()
			if _, err := store.Create(beads.Bead{
				Title:    "review",
				Type:     "task",
				Status:   "open",
				Metadata: map[string]string{"gc.routed_to": routedTo},
			}); err != nil {
				t.Fatalf("create routed bead: %v", err)
			}
			owner, borrower := routeAsAgents()
			cfg := &config.City{
				Workspace: config.Workspace{Name: "test-city"},
				Agents:    []config.Agent{owner, borrower},
			}

			ds := buildDesiredState("test-city", t.TempDir(), time.Now().UTC(), cfg, runtime.NewFake(), store, io.Discard)

			seats := map[string]int{}
			for _, tp := range ds.State {
				seats[tp.TemplateName]++
			}
			for _, template := range []string{routeAsOwner, routeAsBorrower} {
				want := 0
				if template == routedTo {
					want = 1
				}
				if seats[template] != want || ds.ScaleCheckCounts[template] != want {
					t.Errorf("template %q: seats=%d demand=%d, want %d each (seats=%v counts=%v)",
						template, seats[template], ds.ScaleCheckCounts[template], want, seats, ds.ScaleCheckCounts)
				}
			}
		})
	}
}
