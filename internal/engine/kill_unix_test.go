// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package engine

import (
	"os/exec"
	"syscall"
)

// killSelf ends the process with SIGKILL: nothing flushed or closed.
func killSelf() {
	syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
}

// killed says whether a crash child ended by killSelf.
func killed(exit *exec.ExitError) bool {
	return exit.Sys().(syscall.WaitStatus).Signaled()
}
