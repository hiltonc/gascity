//go:build integration

package tmux

import (
	"strings"
	"testing"
)

// TestConfigureServerForwardsHyperlinksAcrossServerRestart drives a real tmux
// server: a session create leaves the server's terminal-features with tmux's
// defaults plus exactly one xterm*:hyperlinks, however often the server is
// configured, and a replacement server bound to the same socket gets it back
// from its first session create.
func TestConfigureServerForwardsHyperlinksAcrossServerRestart(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not installed")
	}

	cfg := DefaultConfig()
	cfg.SocketName = testSocketName + "-features"
	tm := NewTmuxWithConfig(cfg)
	t.Cleanup(func() { _ = tm.TeardownServer() })

	for server := 1; server <= 2; server++ {
		if err := tm.NewSessionWithCommand("gc-test-features", t.TempDir(), "sleep 300"); err != nil {
			t.Fatalf("server %d: NewSessionWithCommand: %v", server, err)
		}
		if err := tm.ConfigureServer(); err != nil {
			t.Fatalf("server %d: ConfigureServer: %v", server, err)
		}
		out, err := tm.run("show-options", "-sv", "terminal-features")
		if err != nil {
			t.Fatalf("server %d: show-options terminal-features: %v", server, err)
		}
		entries := strings.Split(strings.TrimSpace(out), "\n")
		hyperlinks := 0
		for _, entry := range entries {
			if strings.TrimSpace(entry) == hyperlinksTerminalFeature {
				hyperlinks++
			}
		}
		if hyperlinks != 1 {
			t.Fatalf("server %d: terminal-features has %d %s entries, want 1:\n%s", server, hyperlinks, hyperlinksTerminalFeature, out)
		}
		if !strings.Contains(out, "xterm*:clipboard") {
			t.Fatalf("server %d: terminal-features lost tmux's defaults:\n%s", server, out)
		}
		if err := tm.KillServer(); err != nil {
			t.Fatalf("server %d: KillServer: %v", server, err)
		}
	}
}
