// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package shares connects a shuttle to its remote side: the share from the
// config, with its password from the Secret Service. It is the one place a
// share password passes through on its way from the keyring to the server.
//
// This package is desktop-specific (it uses the keyring). The sync core must
// not import it; see the portability rule in SPEC.md.
package shares

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/nfs3"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

// smbPort is where SMB servers listen unless a share says otherwise.
const smbPort = 445

// Key is where the password for an SMB share is kept in the keyring. ok is
// false for shares that need no password: guest SMB shares (no user) and
// anything that is not SMB.
func Key(share *config.Share) (k secrets.Key, ok bool) {
	if share.Type != config.ShareSMB || share.User == "" {
		return secrets.Key{}, false
	}
	return secrets.Key{Host: share.Host, Port: share.Port, Share: share.Share, User: share.User, Domain: share.Domain}, true
}

// OpenRemote connects to the remote side of shuttle s.
func OpenRemote(ctx context.Context, cfg *config.Config, s *config.Shuttle, store secrets.Store) (fsys.FS, error) {
	share, ok := cfg.Shares[s.Share]
	if !ok {
		return nil, fmt.Errorf("shuttle %s: no share named %q", s.Name, s.Share)
	}
	return OpenShare(ctx, &share, s.Remote, store)
}

// OpenShare connects to folder, a slash path inside share ("." for its top),
// with the share's password from store.
func OpenShare(ctx context.Context, share *config.Share, folder string, store secrets.Store) (fsys.FS, error) {
	switch share.Type {
	case config.SharePath:
		dir := filepath.Join(share.Path, filepath.FromSlash(folder))
		f, err := fsys.OpenLocal(dir)
		if err != nil {
			return nil, fmt.Errorf("share %s: folder %s: %w (is the share mounted?)", share.Name, dir, err)
		}
		return f, nil
	case config.ShareSMB:
		return openSMB(ctx, share, folder, store)
	case config.ShareNFS3:
		return openNFS(ctx, share, folder)
	}
	return nil, fmt.Errorf("share %s: unknown type %q", share.Name, share.Type)
}

// Test checks that share can be reached and read: an SMB share is logged
// in to, with password if one is given and otherwise the one stored in the
// keyring; an NFS export is mounted; a mounted folder must be there. It
// reads the share's top folder and changes nothing. It also says how much
// room the share has, zero when it cannot tell: that is no failure.
func Test(ctx context.Context, share *config.Share, password string, store secrets.Store) (fsys.Space, error) {
	var f fsys.FS
	var err error
	switch share.Type {
	case config.SharePath:
		f, err = fsys.OpenLocal(share.Path)
		if err != nil {
			return fsys.Space{}, fmt.Errorf("folder %s: %w (is the share mounted?)", share.Path, err)
		}
	case config.ShareSMB:
		if password != "" {
			store = given(password)
		}
		f, err = openSMB(ctx, share, ".", store)
		if err != nil {
			return fsys.Space{}, err
		}
	case config.ShareNFS3:
		f, err = openNFS(ctx, share, ".")
		if err != nil {
			return fsys.Space{}, err
		}
	default:
		return fsys.Space{}, fmt.Errorf("unknown type %q", share.Type)
	}
	defer f.Close()
	if _, err := f.ReadDir(ctx, "."); err != nil {
		return fsys.Space{}, err
	}
	space, _ := fsys.SpaceOf(ctx, f)
	return space, nil
}

// given is a store that holds one password, the one typed in.
type given string

func (g given) Get(context.Context, secrets.Key) (string, error) { return string(g), nil }
func (given) Set(context.Context, secrets.Key, string) error     { return errors.ErrUnsupported }
func (given) Delete(context.Context, secrets.Key) error          { return errors.ErrUnsupported }

func openSMB(ctx context.Context, share *config.Share, folder string, store secrets.Store) (fsys.FS, error) {
	o := &fsys.SMBOptions{
		Addr:   net.JoinHostPort(share.Host, strconv.Itoa(cmp.Or(share.Port, smbPort))),
		Share:  share.Share,
		Folder: folder,
		User:   share.User,
		Domain: share.Domain,
	}
	if k, ok := Key(share); ok {
		password, err := store.Get(ctx, k)
		if errors.Is(err, secrets.ErrNotFound) {
			return nil, fmt.Errorf("share %s: no password stored in the keyring for %s; set it in shuttle's Shares screen: %w",
				share.Name, k.Account(), err)
		}
		if err != nil {
			return nil, fmt.Errorf("share %s: %w", share.Name, err)
		}
		o.Password = password
	}
	f, err := fsys.DialSMB(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("share %s: %w", share.Name, err)
	}
	return f, nil
}

// NFSUser is who shuttle says it is to share's server (AUTH_SYS): the IDs
// the share sets, or else the user running shuttle, with its groups. The
// server believes it as sent.
func NFSUser(share *config.Share) (uid, gid uint32, groups []uint32) {
	if share.UID == nil && share.GID == nil {
		ids, _ := os.Getgroups() // without them, files shared through a group read as unreadable
		for _, g := range ids {
			groups = append(groups, uint32(g)) //nolint:gosec // from the kernel: 32-bit IDs
		}
	}
	uid, gid = uint32(os.Getuid()), uint32(os.Getgid()) //nolint:gosec // from the kernel: 32-bit IDs
	if share.UID != nil {
		uid = *share.UID
	}
	if share.GID != nil {
		gid = *share.GID
	}
	return uid, gid, groups
}

func openNFS(ctx context.Context, share *config.Share, folder string) (fsys.FS, error) {
	o := &fsys.NFSOptions{
		Options: nfs3.Options{Host: share.Host, Port: share.Port, MountPort: share.MountPort, Export: share.Export},
		Folder:  folder,
	}
	o.UID, o.GID, o.Groups = NFSUser(share)
	f, err := fsys.DialNFS(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("share %s: %w", share.Name, err)
	}
	return f, nil
}

// StorePassword keeps share's password in store. With none typed, an edit
// that moved the share to another server, share or user (old is what it was;
// nil for a new share) takes the stored one along. none reports that the
// share is left with no password stored: its first run will ask for one.
func StorePassword(ctx context.Context, store secrets.Store, old, share *config.Share, password string) (none bool, err error) {
	k, needs := Key(share)
	switch {
	case !needs:
		return false, nil
	case password != "":
		return false, store.Set(ctx, k, password)
	case old == nil:
		return true, nil
	}
	was, had := Key(old)
	if !had || was == k {
		return false, nil
	}
	pw, err := store.Get(ctx, was)
	if errors.Is(err, secrets.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, store.Set(ctx, k, pw)
}
