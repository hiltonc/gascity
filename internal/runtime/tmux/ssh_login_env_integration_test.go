//go:build integration

package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sshAgentSocketCanary = "/gc-test/ssh-agent.sock"

// sshLoginCanaries maps each SSH login variable to a value no real login would
// produce, so a match can only have come from the test.
func sshLoginCanaries() map[string]string {
	canaries := make(map[string]string, len(sshLoginEnvKeys))
	for _, k := range sshLoginEnvKeys {
		canaries[k] = "gc-test-" + strings.ToLower(k)
	}
	return canaries
}

func newPrivateSocketTmux(t *testing.T, tag string) *Tmux {
	t.Helper()
	if !hasTmux() {
		t.Skip("tmux not installed")
	}
	cfg := DefaultConfig()
	cfg.SocketName = privateSocketName(tag)
	tm := NewTmuxWithConfig(cfg)
	t.Cleanup(func() { _ = tm.KillServer() })
	return tm
}

// envReportingPaneCommand writes the pane's environment to a file, then
// signals channel so the test can read it without polling.
func envReportingPaneCommand(t *testing.T, channel string) (command, envFile string) {
	t.Helper()
	envFile = filepath.Join(t.TempDir(), "pane-env")
	command = fmt.Sprintf("env > '%s'; tmux wait-for -S %s; exec sleep 600", envFile, channel)
	return command, envFile
}

func readPaneEnv(t *testing.T, tm *Tmux, channel, envFile string) string {
	t.Helper()
	if _, err := tm.run("wait-for", channel); err != nil {
		t.Fatalf("waiting for pane to report its env: %v", err)
	}
	body, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("reading pane env: %v", err)
	}
	return string(body)
}

func showEnvironment(t *testing.T, tm *Tmux, args ...string) string {
	t.Helper()
	out, err := tm.run(append([]string{"show-environment"}, args...)...)
	if err != nil {
		t.Fatalf("show-environment %v: %v", args, err)
	}
	return out
}

// assertNoSSHLoginEnv fails for every SSH login variable listed with a value in
// env, a newline-separated KEY=VALUE listing. tmux's "-KEY" removal markers do
// not count: they are what withholding looks like.
func assertNoSSHLoginEnv(t *testing.T, where, env string) {
	t.Helper()
	for _, line := range strings.Split(env, "\n") {
		for _, k := range sshLoginEnvKeys {
			if strings.HasPrefix(line, k+"=") {
				t.Errorf("%s carries %s", where, line)
			}
		}
	}
}

func assertHasEnvLine(t *testing.T, where, env, line string) {
	t.Helper()
	for _, l := range strings.Split(env, "\n") {
		if l == line {
			return
		}
	}
	t.Errorf("%s is missing %q, so this test cannot tell a scrub from an env that never arrived", where, line)
}

func TestServerStartedFromSSHLoginHasNoSSHLoginEnv(t *testing.T) {
	for k, v := range sshLoginCanaries() {
		t.Setenv(k, v)
	}
	t.Setenv("SSH_AUTH_SOCK", sshAgentSocketCanary)
	tm := newPrivateSocketTmux(t, "sshsrv")
	command, envFile := envReportingPaneCommand(t, "gcsshsrv-ready")

	if err := tm.NewSessionWithCommandAndEnv("gcsshsrv", "", command, nil); err != nil {
		t.Fatalf("NewSessionWithCommandAndEnv: %v", err)
	}

	pane := readPaneEnv(t, tm, "gcsshsrv-ready", envFile)
	global := showEnvironment(t, tm, "-g")
	assertNoSSHLoginEnv(t, "server global env", global)
	assertNoSSHLoginEnv(t, "session env", showEnvironment(t, tm, "-t", "gcsshsrv"))
	assertNoSSHLoginEnv(t, "pane env", pane)
	assertHasEnvLine(t, "server global env", global, "SSH_AUTH_SOCK="+sshAgentSocketCanary)
	assertHasEnvLine(t, "pane env", pane, "SSH_AUTH_SOCK="+sshAgentSocketCanary)
}

func TestNewSessionScrubsSSHLoginEnvFromExistingServer(t *testing.T) {
	tm := newPrivateSocketTmux(t, "sshold")
	if err := tm.NewSessionWithCommand("gcssholder", "", "exec sleep 600"); err != nil {
		t.Fatalf("starting server: %v", err)
	}
	for k, v := range sshLoginCanaries() {
		if _, err := tm.run("set-environment", "-g", k, v); err != nil {
			t.Fatalf("seeding global %s: %v", k, err)
		}
	}
	command, envFile := envReportingPaneCommand(t, "gcsshold-ready")

	if err := tm.NewSessionWithCommandAndEnv("gcsshold", "", command, nil); err != nil {
		t.Fatalf("NewSessionWithCommandAndEnv: %v", err)
	}

	pane := readPaneEnv(t, tm, "gcsshold-ready", envFile)
	assertNoSSHLoginEnv(t, "server global env", showEnvironment(t, tm, "-g"))
	assertNoSSHLoginEnv(t, "session env", showEnvironment(t, tm, "-t", "gcsshold"))
	assertNoSSHLoginEnv(t, "pane env", pane)
}

// TestRespawnScrubsSSHLoginEnv seeds the session env as well as the global
// one: an attach over SSH copies SSH_CONNECTION into the session env through
// tmux's update-environment, and respawn-pane inherits both.
func TestRespawnScrubsSSHLoginEnv(t *testing.T) {
	tm := newPrivateSocketTmux(t, "sshrsp")
	if err := tm.NewSessionWithCommand("gcsshrsp", "", "exec sleep 600"); err != nil {
		t.Fatalf("NewSessionWithCommand: %v", err)
	}
	for k, v := range sshLoginCanaries() {
		if _, err := tm.run("set-environment", "-g", k, v); err != nil {
			t.Fatalf("seeding global %s: %v", k, err)
		}
		if _, err := tm.run("set-environment", "-t", "gcsshrsp", k, v); err != nil {
			t.Fatalf("seeding session %s: %v", k, err)
		}
	}
	command, envFile := envReportingPaneCommand(t, "gcsshrsp-ready")

	if err := tm.RespawnPaneWithWorkDir("gcsshrsp", "", command); err != nil {
		t.Fatalf("RespawnPaneWithWorkDir: %v", err)
	}

	pane := readPaneEnv(t, tm, "gcsshrsp-ready", envFile)
	assertNoSSHLoginEnv(t, "server global env", showEnvironment(t, tm, "-g"))
	assertNoSSHLoginEnv(t, "session env", showEnvironment(t, tm, "-t", "gcsshrsp"))
	assertNoSSHLoginEnv(t, "pane env", pane)
}
