// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package shares

import "errors"

// errNoUnixIDs is why an NFS share without both IDs cannot be used here.
var errNoUnixIDs = errors.New("a Windows user has no Unix user and group IDs: set the share's uid and gid to the ones the NFS server knows you by")

// ownIDs are none on Windows (os.Getuid is -1).
func ownIDs() (uid, gid uint32, groups []uint32, err error) {
	return 0, 0, nil, errNoUnixIDs
}
