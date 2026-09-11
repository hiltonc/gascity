package main

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
)

type externalCommandCall struct {
	path string
	argv []string
}

// stubExternalCommandRunner records the handover instead of performing it, so
// a test can assert on argv without replacing the test binary.
func stubExternalCommandRunner(t *testing.T, code int, err error) *externalCommandCall {
	t.Helper()
	var call externalCommandCall
	prev := runExternalCommand
	runExternalCommand = func(path string, argv, _ []string) (int, error) {
		call.path = path
		call.argv = append([]string(nil), argv...)
		return code, err
	}
	t.Cleanup(func() { runExternalCommand = prev })
	return &call
}

func stubExternalCommandLookup(t *testing.T, resolved map[string]string) {
	t.Helper()
	prev := lookExternalCommandPath
	lookExternalCommandPath = func(file string) (string, error) {
		if path, ok := resolved[file]; ok {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookExternalCommandPath = prev })
}

func TestDelegateToExternalCommandForwardsArgumentsUnchanged(t *testing.T) {
	stubExternalCommandLookup(t, map[string]string{"gc-spike": "/opt/bin/gc-spike"})
	call := stubExternalCommandRunner(t, 0, nil)

	var stderr bytes.Buffer
	code, delegated := delegateToExternalCommand([]string{"spike", "--city", "x", "-n", "--", "tail"}, &stderr)

	if !delegated || code != 0 {
		t.Fatalf("delegateToExternalCommand() = (%d, %v), want (0, true)", code, delegated)
	}
	if call.path != "/opt/bin/gc-spike" {
		t.Errorf("ran %q, want the path PATH resolved", call.path)
	}
	want := []string{"gc-spike", "--city", "x", "-n", "--", "tail"}
	if !reflect.DeepEqual(call.argv, want) {
		t.Errorf("argv = %q, want %q; gc must not parse, reorder, or consume the extension's flags", call.argv, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want silence on a successful handover", stderr.String())
	}
}

func TestDelegateToExternalCommandOnlyConsultsTheFirstSubcommand(t *testing.T) {
	stubExternalCommandLookup(t, map[string]string{"gc-spike-inner": "/opt/bin/gc-spike-inner"})
	stubExternalCommandRunner(t, 0, nil)

	var stderr bytes.Buffer
	if _, delegated := delegateToExternalCommand([]string{"spike", "inner"}, &stderr); delegated {
		t.Error("delegated to gc-spike-inner; only the first unknown subcommand participates in lookup")
	}
}

func TestDelegateToExternalCommandRefusesBuiltinNames(t *testing.T) {
	builtins := coreCommandNames(newRootCmdWithOptions(nil, nil, rootCommandOptions{}))
	for _, name := range []string{"status", "start", "help", "version"} {
		if !builtins[name] {
			t.Fatalf("%q is not a built-in; the precedence fixture is stale", name)
		}
	}

	stubExternalCommandLookup(t, map[string]string{
		"gc-status":  "/opt/bin/gc-status",
		"gc-version": "/opt/bin/gc-version",
	})
	stubExternalCommandRunner(t, 0, nil)

	for _, name := range []string{"status", "version"} {
		var stderr bytes.Buffer
		if _, delegated := delegateToExternalCommand([]string{name, "arg"}, &stderr); delegated {
			t.Errorf("gc %s delegated to an extension; built-in commands always win", name)
		}
	}
}

func TestDelegateToExternalCommandLeavesUnknownCommandsWithoutExtensionsAlone(t *testing.T) {
	stubExternalCommandLookup(t, nil)
	stubExternalCommandRunner(t, 0, nil)

	var stderr bytes.Buffer
	if _, delegated := delegateToExternalCommand([]string{"blorp"}, &stderr); delegated {
		t.Error("delegated with no gc-blorp on PATH; gc must keep its own unknown-command diagnostics")
	}
}

func TestDelegateToExternalCommandIgnoresArgumentsThatAreNotSubcommands(t *testing.T) {
	stubExternalCommandLookup(t, map[string]string{
		"gc---help":   "/opt/bin/gc---help",
		"gc-../spike": "/opt/bin/gc-dotdot",
		"gc-":         "/opt/bin/gc-empty",
	})
	stubExternalCommandRunner(t, 0, nil)

	for _, args := range [][]string{nil, {}, {"--help"}, {"-n"}, {"../spike"}, {""}} {
		var stderr bytes.Buffer
		if _, delegated := delegateToExternalCommand(args, &stderr); delegated {
			t.Errorf("delegated for args %q, want no lookup for a non-subcommand argument", args)
		}
	}
}

func TestDelegateToExternalCommandRefusesRelativePathHits(t *testing.T) {
	prev := lookExternalCommandPath
	lookExternalCommandPath = func(string) (string, error) {
		return "gc-spike", exec.ErrDot
	}
	t.Cleanup(func() { lookExternalCommandPath = prev })
	stubExternalCommandRunner(t, 0, nil)

	var stderr bytes.Buffer
	if _, delegated := delegateToExternalCommand([]string{"spike"}, &stderr); delegated {
		t.Error("delegated to a hit from a relative PATH entry; the working directory must not decide what gc spike means")
	}
}

func TestDelegateToExternalCommandReportsHandoverFailure(t *testing.T) {
	stubExternalCommandLookup(t, map[string]string{"gc-spike": "/opt/bin/gc-spike"})
	stubExternalCommandRunner(t, 1, errors.New("exec format error"))

	var stderr bytes.Buffer
	code, delegated := delegateToExternalCommand([]string{"spike"}, &stderr)

	if !delegated || code != 1 {
		t.Fatalf("delegateToExternalCommand() = (%d, %v), want (1, true)", code, delegated)
	}
	if !strings.Contains(stderr.String(), "gc-spike") || !strings.Contains(stderr.String(), "exec format error") {
		t.Errorf("stderr = %q, want the extension name and the underlying failure", stderr.String())
	}
}

func TestDelegateToExternalCommandMirrorsASupervisedExitCode(t *testing.T) {
	stubExternalCommandLookup(t, map[string]string{"gc-spike": "/opt/bin/gc-spike"})
	stubExternalCommandRunner(t, 3, nil)

	var stderr bytes.Buffer
	code, delegated := delegateToExternalCommand([]string{"spike"}, &stderr)

	if !delegated || code != 3 {
		t.Errorf("delegateToExternalCommand() = (%d, %v), want (3, true)", code, delegated)
	}
}

func TestPackCommandsBindNameMatchesTheBindingCobraRegisters(t *testing.T) {
	entries := []config.DiscoveredCommand{{Name: "hello", BindingName: "backstage"}}

	if !packCommandsBindName(entries, "backstage") {
		t.Error("a pack binding must shadow an extension of the same name")
	}
	if packCommandsBindName(entries, "hello") {
		t.Error("matched the leaf name; only the binding becomes a root command")
	}
}

func TestDelegatableExternalCommandName(t *testing.T) {
	for _, name := range []string{"spike", "dispatch", "a", "with-dash", "with_underscore", "v2"} {
		if !delegatableExternalCommandName(name) {
			t.Errorf("delegatableExternalCommandName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "-n", "--help", "../spike", "sub/spike", `sub\spike`, filepath.Join("a", "b")} {
		if delegatableExternalCommandName(name) {
			t.Errorf("delegatableExternalCommandName(%q) = true, want false", name)
		}
	}
}
