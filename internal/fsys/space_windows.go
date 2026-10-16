// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

// LocalSpace is how much room the volume dir is on has: what this user can
// write, under their quota if one is set, out of the whole.
func LocalSpace(dir string) (Space, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return Space{}, &fs.PathError{Op: "GetDiskFreeSpaceEx", Path: dir, Err: err}
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return Space{}, &fs.PathError{Op: "GetDiskFreeSpaceEx", Path: dir, Err: err}
	}
	return Space{Free: clampInt64(free, 1), Total: clampInt64(total, 1)}, nil
}
