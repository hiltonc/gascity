package main

import (
	"io"
	"testing"
)

// TestResolveClientIdentityNamesTheSubcommand pins ask 1's client half: the
// name the CLI reports must be the subcommand actually being run, so the
// server log can attribute an expensive request to it.
func TestResolveClientIdentityNamesTheSubcommand(t *testing.T) {
	root := newRootCmdWithOptions(io.Discard, io.Discard, rootCommandOptions{})

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"leaf command", []string{"events"}, "gc events"},
		{"nested command", []string{"session", "nudge"}, "gc session nudge"},
		{"flags before args", []string{"--city", "/tmp/c", "events"}, "gc events"},
		{"flags after command", []string{"events", "--type", "bead.updated", "--since", "2m"}, "gc events"},
		{"bare root", nil, "gc"},
		{"unknown command", []string{"definitely-not-a-command"}, "gc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveClientIdentityName(root, tc.args); got != tc.want {
				t.Fatalf("resolveClientIdentityName(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestResolveClientIdentityExcludesFlagValues pins that nothing from the
// caller's arguments rides along into a header the server logs. Only the
// resolved command path is reported.
func TestResolveClientIdentityExcludesFlagValues(t *testing.T) {
	root := newRootCmdWithOptions(io.Discard, io.Discard, rootCommandOptions{})

	got := resolveClientIdentityName(root, []string{"events", "--type", "s3cr3t-value"})
	if got != "gc events" {
		t.Fatalf("resolveClientIdentityName = %q, want %q", got, "gc events")
	}
}
