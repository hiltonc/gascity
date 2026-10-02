package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
)

func TestConfigLoadMemoAllowlist(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"mail", "inbox"}, true},
		{[]string{"--city", "/c", "mail", "check", "--inject"}, true},
		{[]string{"--rig=r", "nudge", "drain", "s"}, true},
		{[]string{"hook", "--claim", "--json"}, true},
		{[]string{"hook", "run", "--", "mail", "check"}, true},
		{[]string{"events", "--since", "1m"}, true},
		{[]string{"session", "list"}, true},
		{[]string{"sling", "claude", "bd-1", "--dry-run"}, true},

		{[]string{"mail", "send", "alice"}, false},
		{[]string{"mail"}, false},
		{[]string{"nudge", "poll"}, false},
		{[]string{"events", "--follow"}, false},
		{[]string{"events", "--watch=true"}, false},
		{[]string{"session", "kill", "s"}, false},
		{[]string{"supervisor", "run"}, false},
		{[]string{"convoy", "control", "--serve"}, false},
		{[]string{"rig", "add", "x"}, false},
		{[]string{"config", "set", "k", "v"}, false},
		{[]string{"--unknown", "mail", "inbox"}, false},
		{[]string{"--city", "mail", "inbox"}, false},
		{[]string{}, false},
	} {
		if got := configLoadMemoAllowed(tc.args); got != tc.want {
			t.Errorf("configLoadMemoAllowed(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// TestConfigLoadMemoConfirmsTheResolvedCommand covers what the argv pre-scan
// cannot see: a subcommand of an allowlisted command that writes.
func TestConfigLoadMemoConfirmsTheResolvedCommand(t *testing.T) {
	t.Cleanup(config.DisableLoadMemo)
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"events", "--since", "1m"}, true},
		{[]string{"--city", "/c", "mail", "inbox"}, true},
		{[]string{"hook", "run", "--", "mail", "check"}, true},
		{[]string{"events", "reemit-execution", "--run", "r", "--apply"}, false},
		{[]string{"hook", "no-such-subcommand"}, true}, // cobra hands hook the arg
	} {
		if !configLoadMemoAllowed(tc.args) {
			t.Fatalf("pre-scan rejected %q; this test is for the confirmation step", tc.args)
		}
		var stdout, stderr bytes.Buffer
		config.EnableLoadMemo()
		confirmConfigLoadMemoCommand(newRootCmd(&stdout, &stderr), tc.args)
		if got := config.LoadMemoEnabled(); got != tc.want {
			t.Errorf("after confirming %q the memo is enabled=%v, want %v", tc.args, got, tc.want)
		}
		config.DisableLoadMemo()
	}
}

// TestRunEnablesConfigLoadMemoOnlyForTheInvocation pins that run() leaves the
// memo off for whatever runs next in the process.
func TestRunEnablesConfigLoadMemoOnlyForTheInvocation(t *testing.T) {
	configureIsolatedRuntimeEnv(t)
	var sawEnabled bool
	orig := configLoadMemoAllowedForArgs
	t.Cleanup(func() { configLoadMemoAllowedForArgs = orig })
	configLoadMemoAllowedForArgs = func(args []string) bool {
		allowed := orig(args)
		sawEnabled = allowed
		return allowed
	}
	var stdout, stderr bytes.Buffer
	run([]string{"--city", writeConfigLoadMemoCity(t), "session", "list"}, &stdout, &stderr)
	if !sawEnabled {
		t.Fatal("session list did not enable the memo")
	}
	if config.LoadMemoEnabled() {
		t.Fatal("the memo is still on after run() returned")
	}
}

func writeConfigLoadMemoCity(t *testing.T) string {
	t.Helper()
	cityPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(cityPath, "city.toml"), []byte(`[workspace]
name = "mc-city"

[beads]
provider = "file"

[[agent]]
name = "worker"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureScopedFileStoreLayout(cityPath); err != nil {
		t.Fatal(err)
	}
	if err := ensurePersistedScopeLocalFileStore(cityPath); err != nil {
		t.Fatal(err)
	}
	return cityPath
}

// TestHotCommandsComposeTheCityConfigOnce is the bgc-58tk regression seam.
// Each hot one-shot command runs twice in this process, memo off then on, and
// must compose the city config exactly once with it on, many times without
// it, and print the same thing either way.
func TestHotCommandsComposeTheCityConfigOnce(t *testing.T) {
	configureIsolatedRuntimeEnv(t)
	t.Setenv("GC_EVENTS", "")
	t.Setenv("GC_AGENT", "worker")
	cityPath := writeConfigLoadMemoCity(t)
	t.Chdir(cityPath)
	server := newEventsTestServer(t, testEventRoutes{
		cityEvents: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-GC-Index", "0")
			writeJSONResponse(t, w, cityEventsListResponse(t, nil))
		},
	})
	t.Cleanup(server.Close)

	for _, command := range [][]string{
		{"mail", "inbox", "worker"},
		{"mail", "check", "worker"},
		{"nudge", "drain", "worker"},
		{"hook"},
		{"events", "--api", server.URL, "--since", "1m"},
		{"session", "list"},
		{"sling", "worker", "memo-1", "--dry-run"},
	} {
		args := append([]string{"--city", cityPath}, command...)
		t.Run(strings.Join(command[:min(2, len(command))], "_"), func(t *testing.T) {
			off := runCountingCompositions(t, args, false)
			on := runCountingCompositions(t, args, true)
			t.Logf("%q: exit %d, %d compositions without the memo, %d with it", args, on.code, off.compositions, on.compositions)
			if on.compositions != 1 {
				t.Errorf("%q composed the city config %d times with the memo, want 1\nstderr: %s", args, on.compositions, on.stderr)
			}
			if off.compositions <= 1 {
				t.Errorf("%q composed %d times without the memo; the seam is not measuring repeated loads", args, off.compositions)
			}
			if on.code != off.code || on.stdout != off.stdout || on.stderr != off.stderr {
				t.Errorf("%q output differs with the memo\noff: exit %d\n%s%s\non: exit %d\n%s%s",
					args, off.code, off.stdout, off.stderr, on.code, on.stdout, on.stderr)
			}
		})
	}
}

type countedRun struct {
	code           int
	stdout, stderr string
	compositions   int64
}

func runCountingCompositions(t *testing.T, args []string, memo bool) countedRun {
	t.Helper()
	orig := configLoadMemoAllowedForArgs
	t.Cleanup(func() { configLoadMemoAllowedForArgs = orig })
	if !memo {
		configLoadMemoAllowedForArgs = func([]string) bool { return false }
	}
	// A warning printed once per process must print in both runs.
	builtinImportWarningCache.Range(func(key, _ any) bool {
		builtinImportWarningCache.Delete(key)
		return true
	})
	var stdout, stderr bytes.Buffer
	before := config.LoadWithIncludesCalls()
	code := run(args, &stdout, &stderr)
	configLoadMemoAllowedForArgs = orig
	return countedRun{
		code:         code,
		stdout:       stdout.String(),
		stderr:       stderr.String(),
		compositions: config.LoadWithIncludesCalls() - before,
	}
}
