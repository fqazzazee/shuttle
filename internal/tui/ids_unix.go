// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package tui

import (
	"fmt"
	"os"
)

// uidHint and gidHint explain an NFS share's IDs: this user's when empty.
func uidHint() string {
	return fmt.Sprintf("optional: yours (%d) if empty; the server believes it as sent", os.Getuid())
}

func gidHint() string { return fmt.Sprintf("optional: yours (%d) if empty", os.Getgid()) }
