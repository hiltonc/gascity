package config

import (
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/shellquote"
)

func routeAsFixture() (owner, borrower Agent) {
	owner = Agent{Dir: "gascity", Name: "reviewer", MaxActiveSessions: ptrInt(3)}
	borrower = Agent{Dir: "gascity", Name: "codex-reviewer", MaxActiveSessions: ptrInt(1), RouteAs: owner.QualifiedName()}
	return owner, borrower
}

func TestValidateAgentsAcceptsRouteAsToAgentInSameRig(t *testing.T) {
	owner, borrower := routeAsFixture()
	if err := ValidateAgents([]Agent{owner, borrower}); err != nil {
		t.Fatalf("ValidateAgents: %v", err)
	}
}

func TestValidateAgentsRejectsInvalidRouteAs(t *testing.T) {
	owner, borrower := routeAsFixture()
	otherRig := Agent{Dir: "other", Name: "reviewer"}
	chained := Agent{Dir: "gascity", Name: "third", RouteAs: borrower.QualifiedName()}
	self := Agent{Dir: "gascity", Name: "loop", RouteAs: "gascity/loop"}
	crossRig := borrower
	crossRig.RouteAs = otherRig.QualifiedName()
	unknown := borrower
	unknown.RouteAs = "gascity/missing"

	cases := []struct {
		name   string
		agents []Agent
		want   string
	}{
		{"unknown target", []Agent{owner, unknown}, `route_as references unknown agent "gascity/missing"`},
		{"self reference", []Agent{self}, "route_as references itself"},
		{"other rig", []Agent{owner, otherRig, crossRig}, `route_as target "other/reviewer" is not in the same rig`},
		{"chained", []Agent{owner, borrower, chained}, `route_as target "gascity/codex-reviewer" itself sets route_as`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAgents(tc.agents)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateAgents error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestRouteAsTargetsSharedQueueThenOwnRoute(t *testing.T) {
	owner, borrower := routeAsFixture()
	slot := borrower
	slot.Name = "codex-reviewer-1"
	slot.PoolName = borrower.QualifiedName()

	for _, a := range []Agent{borrower, slot} {
		if got, want := a.poolDemandTarget(), owner.QualifiedName(); got != want {
			t.Fatalf("%s poolDemandTarget() = %q, want %q", a.QualifiedName(), got, want)
		}
		probes := shellquote.Join([]string{"--", owner.QualifiedName(), borrower.QualifiedName()})
		for kind, cmd := range map[string]string{
			"work_query":  a.EffectiveWorkQuery(),
			"routed_pool": a.EffectiveRoutedPoolQuery(),
		} {
			if !strings.HasSuffix(cmd, " "+probes) {
				t.Errorf("%s %s does not probe the shared queue then its own route %q:\n%s", a.QualifiedName(), kind, probes, cmd)
			}
		}
		if cmd, want := a.EffectivePoolDemandQuery(), shellquote.Join([]string{"--", owner.QualifiedName()}); !strings.HasSuffix(cmd, " "+want) {
			t.Errorf("%s pool demand query does not count the shared queue %q:\n%s", a.QualifiedName(), want, cmd)
		}
	}
}

func TestRouteAsUnsetLeavesQueriesUnchanged(t *testing.T) {
	owner, _ := routeAsFixture()
	probes := shellquote.Join([]string{"--", owner.QualifiedName()})
	for kind, cmd := range map[string]string{
		"work_query":  owner.EffectiveWorkQuery(),
		"routed_pool": owner.EffectiveRoutedPoolQuery(),
		"demand":      owner.EffectivePoolDemandQuery(),
	} {
		if !strings.HasSuffix(cmd, " "+probes) {
			t.Errorf("owner %s = %q, want only its own route %q", kind, cmd, probes)
		}
	}
}
