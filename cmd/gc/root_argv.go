package main

import "strings"

// rootCommandOptions controls side effects performed while constructing the
// Cobra tree. invocationArgs is always the injected run(args) slice and never
// includes argv[0].
type rootCommandOptions struct {
	invocationArgs            []string
	discoverPackCommands      bool
	eagerPackCommandDiscovery bool
}

func rootCommandOptionsForArgs(args []string) rootCommandOptions {
	command, ok := firstRootCommand(args)
	discoverPackCommands := !ok || !rootCommandSkipsPackDiscovery(command)
	return rootCommandOptions{
		invocationArgs:            append([]string(nil), args...),
		discoverPackCommands:      discoverPackCommands,
		eagerPackCommandDiscovery: discoverPackCommands,
	}
}

// rootCommandSkipsPackDiscovery identifies built-in commands that cannot
// resolve to a pack binding. Pack discovery only adds city-config and pack
// loading work; each command still performs its normal scope and config
// resolution when it runs.
//
// version, completion, login, logout, and whoami read no city config at all:
// version prints build metadata, completion emits a static script that calls
// back into `gc __complete` for every candidate, and the login trio talks only
// to the hosted service. help is deliberately absent, as are a bare `gc` and
// --help: the root usage lists every mounted pack binding, and `gc help
// <binding>` describes one, so their output depends on the city.
func rootCommandSkipsPackDiscovery(command string) bool {
	switch command {
	case "metrics", "bd", "git-credential", "dolt-state", "dolt-config", "bd-store-bridge",
		"version", "completion", "login", "logout", "whoami":
		return true
	default:
		return false
	}
}

// firstRootCommand returns the first command word under the root's narrow
// persistent-scope grammar. Unknown flags fail closed because this pre-scan
// cannot know whether a later token is their value. A separate known value
// flag consumes exactly one following token, including "--", matching pflag.
func firstRootCommand(args []string) (string, bool) {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--":
			return "", false
		case isRootPersistentValueFlag(arg):
			if index+1 >= len(args) {
				return "", false
			}
			index++
		case isRootPersistentValueAssignment(arg):
			continue
		case strings.HasPrefix(arg, "-"):
			return "", false
		default:
			return arg, true
		}
	}
	return "", false
}

func isRootPersistentValueFlag(arg string) bool {
	switch arg {
	case "--city", "--rig", "--context", "--city-url", "--city-name":
		return true
	default:
		return false
	}
}

func isRootPersistentValueAssignment(arg string) bool {
	name, _, hasValue := strings.Cut(arg, "=")
	return hasValue && isRootPersistentValueFlag(name)
}
