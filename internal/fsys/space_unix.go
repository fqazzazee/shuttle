// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package fsys

import (
	"io/fs"
	"syscall"
)

// LocalSpace is how much room the file system dir is on has: what a user
// can write, without root's reserve, out of the whole.
func LocalSpace(dir string) (Space, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return Space{}, &fs.PathError{Op: "statfs", Path: dir, Err: err}
	}
	return Space{Free: clampInt64(st.Bavail, st.Bsize), Total: clampInt64(st.Blocks, st.Bsize)}, nil
}
