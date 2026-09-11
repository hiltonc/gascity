//go:build !windows

package main

import (
	"fmt"
	"syscall"
)

// execExternalCommand replaces this process with the extension.
//
// Handing over the process image, rather than supervising a child, is what
// makes the contract hold without a line of forwarding code. argv and the
// environment arrive verbatim; the working directory and the three standard
// streams are the ones already open, so a streaming extension streams and
// nothing is buffered or re-emitted; the terminal's foreground process group
// is unchanged, so Ctrl-C is delivered to the extension itself; and the shell
// reads the extension's own exit status, signal death included, because no
// other process is left to translate it.
//
// It returns only on failure to hand over.
func execExternalCommand(path string, argv, env []string) (int, error) {
	if err := syscall.Exec(path, argv, env); err != nil {
		return 1, fmt.Errorf("exec: %w", err)
	}
	return 0, nil
}
