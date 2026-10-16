// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"context"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/fqazzazee/shuttle/internal/fsys"
)

// localSlash is the tree's path for a folder of this computer. Windows has
// no one top, so the tree's "/" holds the drives: C:\Users\me is
// /C:/Users/me. A path without a drive is kept as it is, for the wizard to
// refuse.
func localSlash(native string) string {
	vol := filepath.VolumeName(native)
	if len(vol) != 2 || vol[1] != ':' {
		return native
	}
	rest := filepath.ToSlash(strings.TrimPrefix(filepath.Clean(native), vol))
	return cleanSlash(strings.ToUpper(vol) + "/" + strings.TrimPrefix(rest, "/"))
}

// localNative is the folder a tree path of this computer's is: /C:/Users/me
// is C:\Users\me, /C: is C:\, and the top of the drives is none.
func localNative(slash string) string {
	if !strings.HasPrefix(slash, "/") {
		return slash // as typed
	}
	drive, rest, _ := strings.Cut(strings.TrimPrefix(slash, "/"), "/")
	if drive == "" {
		return ""
	}
	return filepath.Join(drive+`\`, filepath.FromSlash(rest))
}

// listLocal lists dir, a tree path of this computer's: at the top, the
// drives; below, through the local backend on the drive, as a run sees it.
func listLocal(ctx context.Context, dir string) ([]entry, error) {
	drive, rest, _ := strings.Cut(strings.TrimPrefix(dir, "/"), "/")
	if drive == "" {
		return drives()
	}
	f, err := fsys.OpenLocal(drive + `\`)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return listFS(ctx, f, "/"+rest)
}

// drives are this computer's drives, as folders.
func drives() ([]entry, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, err
	}
	var out []entry
	for i := range 26 {
		if mask&(1<<i) != 0 {
			out = append(out, entry{name: string(rune('A'+i)) + ":", dir: true})
		}
	}
	return out, nil
}
