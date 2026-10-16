// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package shares

import "os"

// ownIDs are the user running shuttle and its groups.
func ownIDs() (uid, gid uint32, groups []uint32, err error) {
	ids, _ := os.Getgroups() // without them, files shared through a group read as unreadable
	for _, g := range ids {
		groups = append(groups, uint32(g)) //nolint:gosec // from the kernel: 32-bit IDs
	}
	return uint32(os.Getuid()), uint32(os.Getgid()), groups, nil //nolint:gosec // from the kernel: 32-bit IDs
}
