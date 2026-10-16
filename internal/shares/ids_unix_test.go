// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package shares

import (
	"os"
	"testing"

	"github.com/fqazzazee/shuttle/internal/config"
)

// Unset IDs are the user running shuttle, with its groups; a share that sets
// either is someone else, and gets none of this user's groups.
func TestNFSUser(t *testing.T) {
	me, mine := uint32(os.Getuid()), uint32(os.Getgid()) //nolint:gosec // test IDs
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	uid, gid, got, err := NFSUser(&config.Share{Type: config.ShareNFS3})
	if err != nil || uid != me || gid != mine || len(got) != len(groups) {
		t.Errorf("unset: %d, %d, %v, %v; want %d, %d and %d groups", uid, gid, got, err, me, mine, len(groups))
	}
	uid, gid, got, err = NFSUser(&config.Share{Type: config.ShareNFS3, UID: ptr[uint32](0)})
	if err != nil || uid != 0 || gid != mine || got != nil {
		t.Errorf("uid = 0: %d, %d, %v, %v", uid, gid, got, err)
	}
	uid, gid, got, err = NFSUser(&config.Share{Type: config.ShareNFS3, GID: ptr[uint32](100)})
	if err != nil || uid != me || gid != 100 || got != nil {
		t.Errorf("gid = 100: %d, %d, %v, %v", uid, gid, got, err)
	}
}

// testIDs are an NFS share's IDs in a test's config: none, for the user's own.
const testIDs = ""

// setTestIDs leaves sh with the user's own IDs.
func setTestIDs(*config.Share) {}
