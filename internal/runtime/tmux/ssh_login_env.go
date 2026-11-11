package tmux

import (
	"errors"
	"fmt"
	"strings"
)

// sshLoginEnvKeys describe the SSH login a process descends from; in an agent
// pane SSH_CLIENT makes bash source ~/.bashrc for every `bash -c`. SSH_AUTH_SOCK
// is not one of them: it locates the ssh-agent that git over SSH needs.
var sshLoginEnvKeys = []string{"SSH_CLIENT", "SSH_CONNECTION", "SSH_TTY"}

// withoutSSHLoginEnv returns environ minus the sshLoginEnvKeys entries.
func withoutSSHLoginEnv(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, kv := range environ {
		if !isSSHLoginEnv(kv) {
			kept = append(kept, kv)
		}
	}
	return kept
}

func isSSHLoginEnv(kv string) bool {
	for _, k := range sshLoginEnvKeys {
		if strings.HasPrefix(kv, k+"=") {
			return true
		}
	}
	return false
}

// scrubServerSSHLoginEnv unsets sshLoginEnvKeys in the global environment of
// the city's tmux server, which holds them when an SSH login started it. A
// default (personal) server is not gc's to edit, and an absent one is started
// clean by the next new-session because the tmux client env is filtered.
func (t *Tmux) scrubServerSSHLoginEnv() error {
	if t.cfg.SocketName == "" {
		return nil
	}
	_, err := t.run(setEnvironmentForSSHLoginKeys("-g", "-u")...)
	if err != nil && !errors.Is(err, ErrNoServer) {
		return fmt.Errorf("unsetting SSH login env on tmux server %q: %w", t.cfg.SocketName, err)
	}
	return nil
}

// withholdSSHLoginEnvFromRespawn prepares a pane for respawn-pane, which
// inherits the global env plus its session env, where an attach over SSH
// copies SSH_CONNECTION through update-environment.
func (t *Tmux) withholdSSHLoginEnvFromRespawn(pane string) error {
	args := setEnvironmentForSSHLoginKeys("-t", pane, "-r")
	if t.cfg.SocketName != "" {
		args = append(append(setEnvironmentForSSHLoginKeys("-g", "-u"), ";"), args...)
	}
	if _, err := t.run(args...); err != nil {
		return fmt.Errorf("withholding SSH login env from pane %q: %w", pane, err)
	}
	return nil
}

// setEnvironmentForSSHLoginKeys builds one tmux command list that runs
// `set-environment <flags> KEY` for each of sshLoginEnvKeys.
func setEnvironmentForSSHLoginKeys(flags ...string) []string {
	var args []string
	for i, k := range sshLoginEnvKeys {
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args, "set-environment")
		args = append(args, flags...)
		args = append(args, k)
	}
	return args
}
