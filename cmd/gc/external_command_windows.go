//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

// execExternalCommand runs the extension as a child and mirrors its exit
// status. Windows has no process-image replacement, so this path supervises
// where the Unix one hands over: the standard streams are passed through
// unwrapped, and console control events already reach the whole process group,
// but an extension killed by a signal has no faithful status to report.
func execExternalCommand(path string, argv, env []string) (int, error) {
	cmd := exec.Command(path)
	cmd.Args = argv
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}
