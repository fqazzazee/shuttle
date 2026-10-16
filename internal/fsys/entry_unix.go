// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package fsys

import (
	"io/fs"
	"os"
)

// entryInfo is what the folder listing's entry e, at name in r, is: an
// Lstat through the root, not e.Info, which resolves the entry by path
// string outside the root's protection.
func entryInfo(r *os.Root, name string, _ fs.DirEntry) (fs.FileInfo, error) {
	return r.Lstat(name)
}
