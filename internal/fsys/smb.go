// SPDX-License-Identifier: GPL-2.0-or-later

package fsys

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/cloudsoda/go-smb2"
)

// SMBOptions say how to reach a folder on an SMB share.
type SMBOptions struct {
	Addr     string // host:port; the SMB port is 445
	Share    string // share name, e.g. "home"
	Folder   string // folder inside the share, slash-separated; "." for the share root
	User     string
	Password string
	Domain   string // optional workgroup or domain
}

// SMB is an FS on a folder of an SMB share, reached directly over SMB 2/3
// with NTLMv2. Every path is resolved inside Folder.
type SMB struct {
	session *smb2.Session
	share   *smb2.Share
	folder  string // "" for the share root
}

var _ FS = (*SMB)(nil)

// DialSMB connects and logs in to the share. The folder must already exist:
// a mistyped folder should fail, not be created.
func DialSMB(ctx context.Context, o *SMBOptions) (*SMB, error) {
	if o.Folder == "" {
		o.Folder = "."
	}
	if !ValidPath(o.Folder) || strings.ContainsRune(o.Folder, '\\') {
		return nil, fmt.Errorf("smb folder %q: %w", o.Folder, ErrInvalidPath)
	}
	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: o.User, Password: o.Password, Domain: o.Domain}}
	session, err := d.Dial(ctx, o.Addr)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", o.Addr, err)
	}
	share, err := session.WithContext(ctx).Mount(o.Share)
	if err != nil {
		// The mount error is the one worth reporting; a failed logoff on top
		// of it changes nothing for the caller.
		_ = session.Logoff()
		return nil, fmt.Errorf("opening share %q on %s: %w", o.Share, o.Addr, err)
	}
	s := &SMB{session: session, share: share}
	if o.Folder != "." {
		s.folder = o.Folder
	}
	info, err := s.Stat(ctx, ".")
	if err == nil && info.Kind != KindDir {
		err = fmt.Errorf("not a folder")
	}
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("folder %q on share %q: %w", o.Folder, o.Share, err)
	}
	return s, nil
}

// at checks name and returns the share path for it plus a share bound to ctx.
//
// A "\" is a path separator on SMB but an ordinary character in a Linux file
// name, so a local file named `..\..\x` would climb out of the folder once
// sent to the share. Names containing one are refused.
func (s *SMB) at(ctx context.Context, op, name string) (*smb2.Share, string, error) {
	if err := begin(ctx, op, name); err != nil {
		return nil, "", err
	}
	if strings.ContainsRune(name, '\\') {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: ErrInvalidPath}
	}
	p := path.Join(s.folder, name)
	if p == "." {
		p = ""
	}
	return s.share.WithContext(ctx), p, nil
}

func (s *SMB) Stat(ctx context.Context, name string) (FileInfo, error) {
	sh, p, err := s.at(ctx, "stat", name)
	if err != nil {
		return FileInfo{}, err
	}
	fi, err := sh.Lstat(p)
	if err != nil {
		return FileInfo{}, smbErr(err)
	}
	return smbInfo(name, fi), nil
}

func (s *SMB) ReadDir(ctx context.Context, name string) ([]FileInfo, error) {
	sh, p, err := s.at(ctx, "readdir", name)
	if err != nil {
		return nil, err
	}
	entries, err := sh.ReadDir(p)
	if err != nil {
		return nil, smbErr(err)
	}
	out := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		// A server is untrusted: never pass on a name that is not a single,
		// plain path element.
		if base := e.Name(); base == "" || base == "." || base == ".." || strings.ContainsAny(base, "/\\\x00") {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: fmt.Errorf("server sent an unsafe name %q: %w", base, ErrInvalidPath)}
		}
		out = append(out, smbInfo(path.Join(name, e.Name()), e))
	}
	slices.SortFunc(out, func(a, b FileInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (s *SMB) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	sh, p, err := s.at(ctx, "open", name)
	if err != nil {
		return nil, err
	}
	f, err := sh.Open(p)
	if err != nil {
		return nil, smbErr(err)
	}
	return f, nil
}

func (s *SMB) Create(ctx context.Context, name string) (io.WriteCloser, error) {
	sh, p, err := s.at(ctx, "create", name)
	if err != nil {
		return nil, err
	}
	f, err := sh.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return nil, smbErr(err)
	}
	return f, nil
}

func (s *SMB) Rename(ctx context.Context, oldname, newname string) error {
	sh, oldp, err := s.at(ctx, "rename", oldname)
	if err != nil {
		return err
	}
	_, newp, err := s.at(ctx, "rename", newname)
	if err != nil {
		return err
	}
	// go-smb2 renames with ReplaceIfExists, as the FS contract needs.
	return smbErr(sh.Rename(oldp, newp))
}

func (s *SMB) Remove(ctx context.Context, name string) error {
	sh, p, err := s.at(ctx, "remove", name)
	if err != nil {
		return err
	}
	return smbErr(sh.Remove(p))
}

func (s *SMB) MkdirAll(ctx context.Context, name string) error {
	sh, p, err := s.at(ctx, "mkdir", name)
	if err != nil {
		return err
	}
	if p == "" {
		return nil // the folder itself, which DialSMB checked exists
	}
	return smbErr(sh.MkdirAll(p, 0o777))
}

func (s *SMB) Chtimes(ctx context.Context, name string, mtime time.Time) error {
	sh, p, err := s.at(ctx, "chtimes", name)
	if err != nil {
		return err
	}
	// go-smb2 sends the access time as given; a zero time.Time would become
	// a garbage timestamp, so use the modification time for both.
	return smbErr(sh.Chtimes(p, mtime, mtime))
}

func (s *SMB) Close() error {
	return errors.Join(s.share.Umount(), s.session.Logoff())
}

// NTSTATUS codes smbErr translates (MS-ERREF 2.3).
const (
	statusNoSuchFile          = 0xC000000F
	statusObjectNameNotFound  = 0xC0000034
	statusObjectNameCollision = 0xC0000035
	statusObjectPathNotFound  = 0xC000003A
)

// smbErr makes go-smb2's errors answer errors.Is(err, fs.ErrNotExist) and
// fs.ErrExist like local ones do. go-smb2 already maps the common cases;
// the status codes catch the rest.
func smbErr(err error) error {
	if err == nil || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrExist) {
		return err
	}
	var re *smb2.ResponseError
	if errors.As(err, &re) {
		switch re.Code {
		case statusNoSuchFile, statusObjectNameNotFound, statusObjectPathNotFound:
			return fmt.Errorf("%w (%w)", err, fs.ErrNotExist)
		case statusObjectNameCollision:
			return fmt.Errorf("%w (%w)", err, fs.ErrExist)
		}
	}
	return err
}

func smbInfo(name string, fi fs.FileInfo) FileInfo {
	info := FileInfo{Name: name, ModTime: fi.ModTime()}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		info.Kind = KindOther
	case fi.IsDir():
		info.Kind = KindDir
	case fi.Mode().IsRegular():
		info.Kind = KindFile
		info.Size = fi.Size()
	default:
		info.Kind = KindOther
	}
	return info
}
