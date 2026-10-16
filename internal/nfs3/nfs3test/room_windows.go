// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package nfs3test

import (
	nfs "github.com/willscott/go-nfs"
	"golang.org/x/sys/windows"
)

// diskRoom fills s with the room of dir's volume. NTFS counts no inodes, so
// the file counts stay go-nfs's defaults.
func diskRoom(dir string, s *nfs.FSStat) error {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	return windows.GetDiskFreeSpaceEx(p, &s.AvailableSize, &s.TotalSize, &s.FreeSize)
}
