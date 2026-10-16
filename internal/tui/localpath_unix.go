// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package tui

import (
	"context"

	"github.com/fqazzazee/shuttle/internal/fsys"
)

// localSlash is the tree's path for a folder of this computer: here, the
// folder's own path.
func localSlash(native string) string { return cleanSlash(native) }

// localNative is the folder a tree path of this computer's is: the path.
func localNative(slash string) string { return slash }

// listLocal lists dir, a tree path of this computer's, through the local
// backend, as a run sees it; opened for each listing, so there is nothing
// to close.
func listLocal(ctx context.Context, dir string) ([]entry, error) {
	f, err := fsys.OpenLocal("/")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return listFS(ctx, f, dir)
}
