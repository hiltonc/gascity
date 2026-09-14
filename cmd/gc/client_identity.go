package main

import (
	"github.com/gastownhall/gascity/internal/api"
	"github.com/spf13/cobra"
)

// announceClientIdentity records which subcommand this invocation is running
// so every API request it makes names its own source on the server's request
// log. Called once per invocation, after the command tree exists (including
// materialized pack commands) and before any handler can issue a request.
func announceClientIdentity(root *cobra.Command, args []string) {
	api.SetClientIdentity(resolveClientIdentityName(root, args), version)
}

// resolveClientIdentityName returns the command path cobra would dispatch args
// to, e.g. "gc events" or "gc session nudge". Only the resolved path is
// reported — never flag values or positional arguments, which routinely carry
// paths, bead ids, and message text that has no business in a server log or on
// the wire. An unresolvable command reports the bare root, which is what cobra
// will report the failure against anyway.
func resolveClientIdentityName(root *cobra.Command, args []string) string {
	if root == nil {
		return "gc"
	}
	cmd, _, err := root.Find(args)
	if err != nil || cmd == nil {
		return root.CommandPath()
	}
	return cmd.CommandPath()
}
