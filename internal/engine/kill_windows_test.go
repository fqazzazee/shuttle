// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// killedCode is the exit code killSelf ends with: a shell's for SIGKILL.
const killedCode = 128 + 9

// killSelf ends the process with TerminateProcess, Windows' SIGKILL:
// nothing flushed or closed.
func killSelf() {
	windows.TerminateProcess(windows.CurrentProcess(), killedCode)
}

// killed says whether a crash child ended by killSelf.
func killed(exit *exec.ExitError) bool {
	return exit.ExitCode() == killedCode
}
