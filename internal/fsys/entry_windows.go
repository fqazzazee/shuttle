// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"io/fs"
	"os"
)

// entryInfo is what the folder listing's entry e is, as the listing says:
// on Windows, e.Info opens nothing, and resolves no path. os.Root's Lstat
// opens the file asking to read it, so a file this user may not read, or
// one another program holds exclusively, failed the whole scan; now only
// its copy fails, as on Linux. It is one handle per folder, not per file.
func entryInfo(_ *os.Root, _ string, e fs.DirEntry) (fs.FileInfo, error) {
	return e.Info()
}
