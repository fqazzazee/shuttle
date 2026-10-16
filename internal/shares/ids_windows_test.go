// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package shares

import (
	"errors"
	"testing"

	"github.com/fqazzazee/shuttle/internal/config"
)

// A Windows user has no Unix IDs to send: an NFS share must set both, and
// one that does not says so, never sends -1 as a uid.
func TestNFSUser(t *testing.T) {
	for _, sh := range []config.Share{
		{Name: "n", Type: config.ShareNFS3},
		{Name: "n", Type: config.ShareNFS3, UID: ptr[uint32](1000)},
		{Name: "n", Type: config.ShareNFS3, GID: ptr[uint32](100)},
	} {
		if _, _, _, err := NFSUser(&sh); !errors.Is(err, errNoUnixIDs) {
			t.Errorf("uid %v, gid %v: %v, want errNoUnixIDs", sh.UID, sh.GID, err)
		}
	}
	uid, gid, groups, err := NFSUser(&config.Share{Type: config.ShareNFS3, UID: ptr[uint32](1000), GID: ptr[uint32](100)})
	if err != nil || uid != 1000 || gid != 100 || groups != nil {
		t.Errorf("both set: %d, %d, %v, %v", uid, gid, groups, err)
	}
}

// testIDs are an NFS share's IDs in a test's config: Windows needs both.
const testIDs = "uid = 1000\ngid = 1000\n"

// setTestIDs gives sh the IDs of testIDs.
func setTestIDs(sh *config.Share) {
	sh.UID, sh.GID = ptr[uint32](1000), ptr[uint32](1000)
}
