package main

import (
	"strings"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/spf13/cobra"
)

// enableConfigLoadMemoForArgs turns on the process-scoped city-config memo
// (config.EnableLoadMemo) for the duration of one invocation when args name a
// one-shot command that never writes config (bgc-58tk). It returns the func
// that turns it off again; production exits right after, while package tests
// run many invocations in one process and must not inherit it.
//
// Long-lived commands (supervisor, controller, nudge poll, events --follow or
// --watch, convoy control --serve) and anything that edits config are not on
// the list. Argv the pre-scan cannot classify gets no memo, and
// confirmConfigLoadMemoCommand checks cobra's own resolution before the
// command runs.
func enableConfigLoadMemoForArgs(args []string) func() {
	if !configLoadMemoAllowedForArgs(args) {
		return func() {}
	}
	config.EnableLoadMemo()
	return config.DisableLoadMemo
}

// configLoadMemoAllowedForArgs is a seam: tests swap it to run an
// allowlisted command with the memo off and compare.
var configLoadMemoAllowedForArgs = configLoadMemoAllowed

func configLoadMemoAllowed(args []string) bool {
	command, rest, ok := rootCommandAndRest(args)
	if !ok {
		return false
	}
	switch command {
	case "hook", "sling":
		return true
	case "events":
		for _, arg := range rest {
			name, _, _ := strings.Cut(arg, "=")
			if name == "--follow" || name == "--watch" {
				return false
			}
		}
		return true
	case "mail":
		sub, ok := firstRootCommand(rest)
		return ok && (sub == "inbox" || sub == "check")
	case "nudge":
		sub, ok := firstRootCommand(rest)
		return ok && sub == "drain"
	case "session":
		sub, ok := firstRootCommand(rest)
		return ok && sub == "list"
	default:
		return false
	}
}

// configLoadMemoCommands are the resolved command paths the memo may serve.
// Bare events only: events reemit-execution --apply writes.
var configLoadMemoCommands = map[string]bool{
	"gc mail inbox":   true,
	"gc mail check":   true,
	"gc nudge drain":  true,
	"gc hook":         true,
	"gc hook run":     true,
	"gc events":       true,
	"gc session list": true,
	"gc sling":        true,
}

// confirmConfigLoadMemoCommand turns the memo off again when cobra resolves
// argv to a command the pre-scan let through but the allowlist does not hold.
// The pre-scan runs before the root command exists, so it cannot see
// subcommands the way cobra does.
func confirmConfigLoadMemoCommand(root *cobra.Command, args []string) {
	if !config.LoadMemoEnabled() {
		return
	}
	cmd, _, err := root.Find(args)
	if err != nil || !configLoadMemoCommands[cmd.CommandPath()] {
		config.DisableLoadMemo()
	}
}

// rootCommandAndRest is firstRootCommand that also returns the argv after the
// command word.
func rootCommandAndRest(args []string) (string, []string, bool) {
	command, ok := firstRootCommand(args)
	if !ok {
		return "", nil, false
	}
	for index, arg := range args {
		if arg == command && (index == 0 || !isRootPersistentValueFlag(args[index-1])) {
			return command, args[index+1:], true
		}
	}
	return "", nil, false
}
