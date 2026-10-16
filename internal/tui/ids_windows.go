// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

// uidHint and gidHint explain an NFS share's IDs: a Windows user has none
// of their own, so the share needs both.
func uidHint() string {
	return "needed on Windows: the one the NFS server knows you by; the server believes it as sent"
}

func gidHint() string { return "needed on Windows: the server's group for you" }
