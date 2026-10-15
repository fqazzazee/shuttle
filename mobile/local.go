// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package mobile

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/shares"
)

// LocalJSON lists dir, a full path on the phone, as the wizard shows it, as
// BrowseJSON lists a share's folder: [{"Name": "DCIM", "Dir": true}, …],
// folders first. It reads through the local backend, as a run sees the
// folder (the TUI's local tree). The backend is opened on dir itself, not
// on "/" as the TUI's is: Android lets the app open neither "/" nor
// "/storage", only what is in shared storage.
func LocalJSON(dir string) (string, error) {
	f, err := fsys.OpenLocal(dir)
	if err != nil {
		return "", err
	}
	defer f.Close()
	list, err := shares.List(context.Background(), f, "/")
	if err != nil {
		return "", err
	}
	return toJSON(list), nil
}

// MakeFolder makes the folder name in dir, a full path on the phone, and
// returns its path. A folder by that name already there is not an error: the
// wizard opens it. A name the phone's storage cannot hold is refused before
// anything is made, saying why (localNames).
func MakeFolder(dir, name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", errors.New("the folder needs a name")
	case name == "." || name == ".." || strings.Contains(name, "/"):
		return "", fmt.Errorf("%q cannot be a folder's name", name)
	case !path.IsAbs(dir):
		return "", fmt.Errorf("%q is not a full path", dir)
	}
	names := localNames(dir)
	if names.Refuse != nil {
		if err := names.Refuse(name); err != nil {
			return "", fmt.Errorf("the phone cannot hold that name: %s", strings.TrimPrefix(err.Error(), fsys.ErrRefusedName.Error()+": "))
		}
	}
	// Opened on dir, as LocalJSON is.
	f, err := fsys.OpenLocalWith(dir, names)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.MkdirAll(context.Background(), name); err != nil {
		return "", err
	}
	return path.Join(path.Clean(dir), name), nil
}
