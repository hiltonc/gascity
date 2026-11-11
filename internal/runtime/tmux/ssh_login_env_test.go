package tmux

import (
	"slices"
	"testing"
)

func TestWithoutSSHLoginEnvKeepsAgentSocketAndLookalikes(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin",
		"SSH_CLIENT=10.0.0.1 50000 22",
		"SSH_CONNECTION=10.0.0.1 50000 10.0.0.2 22",
		"SSH_TTY=/dev/pts/3",
		"SSH_AUTH_SOCK=/tmp/agent.sock",
		"SSH_CLIENT_EXTRA=kept",
	}

	got := withoutSSHLoginEnv(environ)

	want := []string{"PATH=/usr/bin", "SSH_AUTH_SOCK=/tmp/agent.sock", "SSH_CLIENT_EXTRA=kept"}
	if !slices.Equal(got, want) {
		t.Fatalf("withoutSSHLoginEnv = %q, want %q", got, want)
	}
}
