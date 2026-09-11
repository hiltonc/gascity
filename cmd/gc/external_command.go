package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/gastownhall/gascity/internal/config"
)

// externalCommandPrefix names the executable an unknown subcommand delegates
// to: `gc foo` runs `gc-foo`.
const externalCommandPrefix = "gc-"

// Seams for tests, which cannot install executables on the process PATH and
// must survive a runner that would otherwise replace the test binary.
var (
	lookExternalCommandPath = exec.LookPath
	runExternalCommand      = execExternalCommand
)

// delegateToExternalCommand hands `gc <name> <args...>` to a `gc-<name>`
// executable on PATH, and reports whether it took the invocation over. On the
// Unix handover it does not return at all; it returns an exit code only where
// the runner supervises a child or the handover itself failed.
//
// Resolution runs before Cobra sees the arguments, which is what keeps the
// extension's own flags out of gc's parser: `gc foo --city x` must reach the
// extension with `--city x` intact even though `--city` is one of gc's
// persistent flags, and `gc foo --help` must reach it rather than printing
// gc's help. It also runs before the telemetry provider and the product
// metrics lifecycle are opened, so replacing the process image strands no
// half-written state.
//
// Only the first argument is a candidate, and only when it is a bare name. gc
// never rewrites what it forwards, so an invocation that puts gc's own scope
// flags ahead of the subcommand keeps gc's existing unknown-command behavior
// rather than silently dropping them or reordering them around the name.
func delegateToExternalCommand(args []string, stderr io.Writer) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	name := args[0]
	if !delegatableExternalCommandName(name) {
		return 0, false
	}
	// A hit found through a relative PATH entry comes back with exec.ErrDot,
	// and honoring it would let the working directory decide what `gc foo`
	// means. Any lookup error, that one included, leaves gc's own diagnostics
	// to answer for the name.
	path, err := lookExternalCommandPath(externalCommandPrefix + name)
	if err != nil {
		return 0, false
	}
	if gcAnswersToCommandName(name) {
		return 0, false
	}
	argv := append([]string{externalCommandPrefix + name}, args[1:]...)
	code, err := runExternalCommand(path, argv, os.Environ())
	if err != nil {
		fmt.Fprintf(stderr, "gc: %s%s: %v\n", externalCommandPrefix, name, err) //nolint:errcheck // best-effort stderr
	}
	return code, true
}

// delegatableExternalCommandName rejects the argument shapes that are not a
// subcommand at all. A name carrying a path separator is refused rather than
// joined onto the prefix, which would turn `gc ../foo` into a lookup of the
// relative path `gc-../foo`.
func delegatableExternalCommandName(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}

// gcAnswersToCommandName reports whether gc resolves name itself, as a
// built-in or as a command a pack contributed to this city. Either shadows an
// extension on PATH: an extension is the last resort, so adding `gc foo`
// upstream later takes the name back from an installed `gc-foo` instead of
// being masked by it.
//
// The built-in tree is built without pack discovery and thrown away. Both
// checks are reached only once an extension is known to exist, so an ordinary
// invocation pays a PATH lookup and nothing else.
func gcAnswersToCommandName(name string) bool {
	builtins := coreCommandNames(newRootCmdWithOptions(io.Discard, io.Discard, rootCommandOptions{}))
	return builtins[name] || cityPackBindsCommandName(name)
}

func cityPackBindsCommandName(name string) bool {
	cityPath, err := resolveCity()
	if err != nil {
		return false
	}
	cfg, err := quietLoadCityConfig(cityPath)
	if err != nil {
		return false
	}
	return packCommandsBindName(cfg.PackCommands, name)
}

func packCommandsBindName(entries []config.DiscoveredCommand, name string) bool {
	for _, entry := range entries {
		if entry.BindingName == name {
			return true
		}
	}
	return false
}
