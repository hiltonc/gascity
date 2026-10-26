package tmux

import (
	"errors"
	"strings"
	"testing"
)

// TestConfigureServerSendsSetOptionExitEmptyOff verifies that ConfigureServer
// issues set-option -g exit-empty off through the executor.
func TestConfigureServerSendsSetOptionExitEmptyOff(t *testing.T) {
	fe := &fakeExecutor{}
	tm := &Tmux{cfg: DefaultConfig(), exec: fe}

	if err := tm.ConfigureServer(); err != nil {
		t.Fatalf("ConfigureServer() error = %v", err)
	}

	for _, call := range fe.calls {
		if containsSetOptionExitEmptyWithValue(call, "off") {
			return
		}
	}
	t.Fatalf("ConfigureServer did not issue set-option -g exit-empty off; calls = %v", fe.calls)
}

// TestConfigureServerReappliesExitEmptyForReplacementServer verifies that
// server configuration is applied on every call. A Tmux wrapper can outlive
// the server bound to its socket; per-instance sync.Once would leave a
// replacement server at tmux's unsafe exit-empty=on default.
func TestConfigureServerReappliesExitEmptyForReplacementServer(t *testing.T) {
	fe := &fakeExecutor{}
	tm := &Tmux{cfg: DefaultConfig(), exec: fe}

	for i := range 2 {
		if err := tm.ConfigureServer(); err != nil {
			t.Fatalf("ConfigureServer() call %d error = %v", i, err)
		}
	}

	count := 0
	for _, call := range fe.calls {
		if containsSetOptionExitEmptyWithValue(call, "off") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("set-option -g exit-empty off issued %d times across 2 ConfigureServer calls, want 2", count)
	}
}

// TestTeardownServerCallsKillServer verifies that TeardownServer delegates to
// tmux kill-server via the executor.
func TestTeardownServerCallsKillServer(t *testing.T) {
	fe := &fakeExecutor{}
	tm := &Tmux{cfg: DefaultConfig(), exec: fe}

	if err := tm.TeardownServer(); err != nil {
		t.Fatalf("TeardownServer() error = %v", err)
	}

	for _, call := range fe.calls {
		for _, arg := range call {
			if arg == "kill-server" {
				return
			}
		}
	}
	t.Fatalf("TeardownServer did not call kill-server; calls = %v", fe.calls)
}

// TestTeardownServerTreatsAlreadyGoneServerAsSuccess verifies that TeardownServer
// returns nil when the tmux server is already gone (ErrNoServer), consistent with
// KillServer's existing semantics.
func TestTeardownServerTreatsAlreadyGoneServerAsSuccess(t *testing.T) {
	fe := &fakeExecutor{err: ErrNoServer}
	tm := &Tmux{cfg: DefaultConfig(), exec: fe}

	if err := tm.TeardownServer(); err != nil {
		t.Fatalf("TeardownServer() = %v, want nil for already-gone server", err)
	}
}

// containsSetOptionExitEmptyWithValue returns true if args contains the
// sequence "set-option -g exit-empty <value>", possibly preceded by socket flags.
func containsSetOptionExitEmptyWithValue(args []string, value string) bool {
	for i, arg := range args {
		if arg == "set-option" && i+3 < len(args) &&
			args[i+1] == "-g" && args[i+2] == "exit-empty" && args[i+3] == value {
			return true
		}
	}
	return false
}

// TestConfigureServerAppendsHyperlinksTerminalFeature verifies that
// ConfigureServer appends xterm*:hyperlinks to the server's terminal-features
// rather than replacing them. tmux turns hyperlinks on only for terminals it
// recognizes by XTVERSION, so a client that attaches as plain xterm-256color
// (Ghostty) otherwise has every OSC 8 link stripped.
func TestConfigureServerAppendsHyperlinksTerminalFeature(t *testing.T) {
	fe := &fakeExecutor{outs: []string{
		"",
		"xterm*:clipboard:ccolour:cstyle:focus:title\nscreen*:title\nrxvt*:ignorefkeys\n",
		"",
	}}
	tm := &Tmux{cfg: DefaultConfig(), exec: fe}

	if err := tm.ConfigureServer(); err != nil {
		t.Fatalf("ConfigureServer() error = %v", err)
	}

	if got := countTerminalFeatureAppends(fe.calls, "xterm*:hyperlinks"); got != 1 {
		t.Fatalf("set-option -as terminal-features xterm*:hyperlinks issued %d times, want 1; calls = %v", got, fe.calls)
	}
	for _, call := range fe.calls {
		for i, arg := range call {
			if arg == "set-option" && i+2 < len(call) && call[i+1] != "-as" && call[i+2] == "terminal-features" {
				t.Fatalf("ConfigureServer replaced terminal-features instead of appending: %v", call)
			}
		}
	}
}

// TestConfigureServerKeepsOneHyperlinksTerminalFeature verifies that a server
// already carrying xterm*:hyperlinks is left alone: ConfigureServer runs after
// every session create, and tmux appends a duplicate array entry on each -a.
func TestConfigureServerKeepsOneHyperlinksTerminalFeature(t *testing.T) {
	fe := &fakeExecutor{outs: []string{
		"",
		"xterm*:clipboard:ccolour:cstyle:focus:title\nxterm*:hyperlinks\n",
	}}
	tm := &Tmux{cfg: DefaultConfig(), exec: fe}

	if err := tm.ConfigureServer(); err != nil {
		t.Fatalf("ConfigureServer() error = %v", err)
	}

	if got := countTerminalFeatureAppends(fe.calls, "xterm*:hyperlinks"); got != 0 {
		t.Fatalf("set-option -as terminal-features issued %d times on a server that has the feature, want 0; calls = %v", got, fe.calls)
	}
}

// TestConfigureServerReportsTerminalFeaturesReadFailure verifies that a
// server that cannot report its terminal-features is an error, not a silent
// skip, and that exit-empty is still configured first.
func TestConfigureServerReportsTerminalFeaturesReadFailure(t *testing.T) {
	fe := &fakeExecutor{
		outs: []string{"", ""},
		errs: []error{nil, errors.New("invalid option: terminal-features")},
	}
	tm := &Tmux{cfg: DefaultConfig(), exec: fe}

	err := tm.ConfigureServer()
	if err == nil || !strings.Contains(err.Error(), "terminal-features") {
		t.Fatalf("ConfigureServer() error = %v, want the terminal-features read failure", err)
	}
	if len(fe.calls) == 0 || !containsSetOptionExitEmptyWithValue(fe.calls[0], "off") {
		t.Fatalf("exit-empty off was not configured before terminal-features; calls = %v", fe.calls)
	}
}

// countTerminalFeatureAppends counts "set-option -as terminal-features
// <feature>" calls, possibly preceded by socket flags.
func countTerminalFeatureAppends(calls [][]string, feature string) int {
	count := 0
	for _, args := range calls {
		for i, arg := range args {
			if arg == "set-option" && i+3 < len(args) &&
				args[i+1] == "-as" && args[i+2] == "terminal-features" && args[i+3] == feature {
				count++
			}
		}
	}
	return count
}
