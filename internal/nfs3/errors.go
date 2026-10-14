// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package nfs3

import (
	"fmt"
	"io/fs"
)

// Status is an NFSv3 result code (RFC 1813, nfsstat3).
type Status uint32

const (
	StatusOK          Status = 0
	StatusPerm        Status = 1
	StatusNoEnt       Status = 2
	StatusIO          Status = 5
	StatusNXIO        Status = 6
	StatusAcces       Status = 13
	StatusExist       Status = 17
	StatusXDev        Status = 18
	StatusNoDev       Status = 19
	StatusNotDir      Status = 20
	StatusIsDir       Status = 21
	StatusInval       Status = 22
	StatusFBig        Status = 27
	StatusNoSpc       Status = 28
	StatusROFS        Status = 30
	StatusMLink       Status = 31
	StatusNameTooLong Status = 63
	StatusNotEmpty    Status = 66
	StatusDQuot       Status = 69
	StatusStale       Status = 70
	StatusRemote      Status = 71
	StatusBadHandle   Status = 10001
	StatusNotSync     Status = 10002
	StatusBadCookie   Status = 10003
	StatusNotSupp     Status = 10004
	StatusTooSmall    Status = 10005
	StatusServerFault Status = 10006
	StatusBadType     Status = 10007
	StatusJukebox     Status = 10008
)

var statusText = map[Status]string{
	StatusPerm:        "not allowed",
	StatusNoEnt:       "no such file or folder",
	StatusIO:          "I/O error on the server",
	StatusNXIO:        "no such device",
	StatusAcces:       "permission denied",
	StatusExist:       "already exists",
	StatusXDev:        "cannot move across file systems",
	StatusNoDev:       "no such device",
	StatusNotDir:      "not a folder",
	StatusIsDir:       "is a folder",
	StatusInval:       "invalid argument",
	StatusFBig:        "file too large for the server",
	StatusNoSpc:       "no space left on the server",
	StatusROFS:        "the export is read-only",
	StatusMLink:       "too many links",
	StatusNameTooLong: "name too long",
	StatusNotEmpty:    "folder not empty",
	StatusDQuot:       "over quota on the server",
	StatusStale:       "stale file handle",
	StatusRemote:      "too many levels of remote paths",
	StatusBadHandle:   "bad file handle",
	StatusNotSync:     "attribute update out of sync",
	StatusBadCookie:   "folder listing changed while being read",
	StatusNotSupp:     "not supported by the server",
	StatusTooSmall:    "buffer too small",
	StatusServerFault: "server fault",
	StatusBadType:     "file type not supported by the server",
	StatusJukebox:     "the server is busy; try again later",
}

func (s Status) String() string {
	if t, ok := statusText[s]; ok {
		return t
	}
	return fmt.Sprintf("NFS error %d", uint32(s))
}

// Error is a call the server answered with a status other than OK. It
// matches fs.ErrNotExist, fs.ErrExist and fs.ErrPermission with errors.Is.
type Error struct {
	Op     string // "lookup", "create", ...
	Status Status
}

func (e *Error) Error() string { return "nfs " + e.Op + ": " + e.Status.String() }

func (e *Error) Is(target error) bool {
	switch target {
	case fs.ErrNotExist:
		return e.Status == StatusNoEnt
	case fs.ErrExist:
		return e.Status == StatusExist
	case fs.ErrPermission:
		return e.Status == StatusPerm || e.Status == StatusAcces
	}
	return false
}

// MountStatus is a MOUNT v3 result code (RFC 1813, mountstat3).
type MountStatus uint32

// MountError is an export the server would not hand out.
type MountError struct {
	Export string
	Status MountStatus
}

func (e *MountError) Error() string {
	switch e.Status {
	case 2: // MNT3ERR_NOENT
		return fmt.Sprintf("nfs: the server has no export %q", e.Export)
	case 20: // MNT3ERR_NOTDIR
		return fmt.Sprintf("nfs: export %q is not a folder", e.Export)
	case 1, 13: // MNT3ERR_PERM, MNT3ERR_ACCES
		return fmt.Sprintf("nfs: the server will not share %q with this computer: check that the export "+
			"allows this computer's address, and that it accepts connections from ports above 1023 "+
			"(\"insecure\" on Linux, \"allow connections from non-privileged ports\" on Synology)", e.Export)
	}
	return fmt.Sprintf("nfs: mounting %q failed: mount status %d", e.Export, uint32(e.Status))
}

func (e *MountError) Is(target error) bool {
	switch target {
	case fs.ErrNotExist:
		return e.Status == 2
	case fs.ErrPermission:
		return e.Status == 1 || e.Status == 13
	}
	return false
}
