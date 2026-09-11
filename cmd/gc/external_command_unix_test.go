//go:build !windows

package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// extensionHandoverScript records the pid it runs as, announces itself, and
// then becomes a process with nothing to do but wait for a signal. Recording
// `$$` before the final exec is what makes the pid observable: exec keeps it.
const extensionHandoverScript = `#!/bin/sh
echo $$ > "$1"
echo ready
exec sleep 30
`

// TestDelegateToExternalCommandRunsTheExtensionAsGCsOwnProcess drives a real
// gc at an extension and signals it.
//
// The rest of this package's tests pass unchanged against an implementation
// that supervises a child instead of handing the process image over, because
// argv, environment, working directory and an ordinary exit code look the same
// either way. The three properties that do not survive supervision are here:
// the extension runs as the pid gc was launched as, its output leaves it while
// it is still running, and the invoker reads its signal death rather than a
// status something in the middle chose.
func TestDelegateToExternalCommandRunsTheExtensionAsGCsOwnProcess(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			dir := t.TempDir()
			pidPath := filepath.Join(dir, "extension.pid")
			extension := filepath.Join(dir, externalCommandPrefix+"streamer")
			if err := os.WriteFile(extension, []byte(extensionHandoverScript), 0o755); err != nil {
				t.Fatalf("write extension: %v", err)
			}

			cmd := exec.Command(reexecGCTestBinaryForTests(t), "streamer", pidPath)
			cmd.Dir = dir
			cmd.Env = sanitizedBaseEnv("PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"))
			// Its own process group, so the test can signal the extension the
			// way a terminal signals a foreground job without reaching the
			// test binary that launched it.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatalf("stdout pipe: %v", err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatalf("start gc: %v", err)
			}
			launched := cmd.Process.Pid
			t.Cleanup(func() { _ = syscall.Kill(-launched, syscall.SIGKILL) })

			readExtensionReadyLine(t, stdout)

			running := readExtensionPID(t, pidPath)
			if running != launched {
				t.Fatalf("extension runs as pid %d, want %d, the pid gc was launched as; a supervised child leaves gc in the middle of every signal and every exit status", running, launched)
			}

			if err := syscall.Kill(-launched, signal); err != nil {
				t.Fatalf("signal extension process group: %v", err)
			}

			status := waitStatus(t, cmd)
			if !status.Signaled() || status.Signal() != signal {
				t.Fatalf("wait status = %v, want death by %v; the invoker must read the extension's own fate", status, signal)
			}
		})
	}
}

// readExtensionReadyLine blocks until the extension's announcement arrives,
// which proves the process is live and that its output is not being collected
// for re-emission after it exits.
func readExtensionReadyLine(t *testing.T, stdout io.Reader) {
	t.Helper()
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "ready" {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read extension output: %v", err)
	}
	t.Fatal("extension produced no output before exiting; gc did not delegate")
}

func readExtensionPID(t *testing.T, path string) int {
	t.Helper()
	recorded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read extension pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(recorded)))
	if err != nil {
		t.Fatalf("parse extension pid %q: %v", recorded, err)
	}
	return pid
}

func waitStatus(t *testing.T, cmd *exec.Cmd) syscall.WaitStatus {
	t.Helper()
	var exitErr *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exitErr) {
		t.Fatalf("Wait() = %v, want a signal death", err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("Sys() = %T, want syscall.WaitStatus", exitErr.Sys())
	}
	return status
}
