// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package main

import (
	"context"
	"fmt"
	"io"
)

// runTray says what shows shuttle in the top bar here: the GNOME extension.
func runTray(_ context.Context, stderr io.Writer) int {
	fmt.Fprintln(stderr, "shuttle tray: on Linux, the GNOME Shell extension is shuttle's icon in the top bar")
	return 2
}
