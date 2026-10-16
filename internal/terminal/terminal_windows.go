// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package terminal

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// known are the terminals tried: Windows Terminal, in a window of its own.
// Without it, the command gets a console window from Windows (ownWindow);
// conhost.exe started from here opens none.
var known = []struct {
	name string
	run  []string
}{
	{"wt", []string{"-w", "new", "new-tab", "--"}},
}

// example is a terminal setting, for ErrNone.
const example = "wt -w new new-tab --"

// ownWindow: a command started with a new console gets a window of its own,
// so there is always a way to open one.
const ownWindow = true

// wrap leaves argv as it is: there is no service manager to step out of.
func wrap(argv []string, _ func(string) (string, error)) []string { return argv }

// detach gives cmd a console of its own, which is the window when nothing
// else opens one (Windows Terminal ignores it), and keeps it out of the
// starter's Ctrl+C.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE | windows.CREATE_NEW_PROCESS_GROUP}
}
