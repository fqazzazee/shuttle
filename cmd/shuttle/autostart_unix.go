// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package main

import (
	"fmt"
	"io"
)

// runAutostart says how the daemon starts with the session here: systemd's
// user unit, which the user enables.
func runAutostart(_ []string, stdout, _ io.Writer) int {
	fmt.Fprintln(stdout, "systemd starts the daemon here: systemctl --user enable --now shuttle (disable to stop)")
	return 0
}
