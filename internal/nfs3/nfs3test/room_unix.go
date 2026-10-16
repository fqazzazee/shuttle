// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package nfs3test

import (
	"syscall"

	nfs "github.com/willscott/go-nfs"
)

// diskRoom fills s with the room of dir's file system.
func diskRoom(dir string, s *nfs.FSStat) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return err
	}
	bs := uint64(st.Bsize) //nolint:gosec // a block size is positive
	s.TotalSize, s.FreeSize, s.AvailableSize = st.Blocks*bs, st.Bfree*bs, st.Bavail*bs
	s.TotalFiles, s.FreeFiles, s.AvailableFiles = st.Files, st.Ffree, st.Ffree
	return nil
}
