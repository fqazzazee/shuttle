// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package nfs3 is a small NFSv3 client (RFC 1813) over TCP: the portmapper,
// the MOUNT protocol and the NFS calls shuttle's fsys backend needs. It
// works on file handles; fsys turns paths into them.
//
// The server is untrusted. Every call takes a context and has a deadline,
// replies are size-checked before they are read, and nothing a reply says
// can make the client panic or allocate more than maxRecord.
//
// This package must stay free of desktop-specific imports; see the
// portability rule in SPEC.md.
package nfs3

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

// Default ports, used when neither the options nor the portmapper say.
const (
	portmapPort = 111
	nfsPort     = 2049
)

// Options say how to reach an export.
type Options struct {
	Host string
	// Port is the NFS service's port; 0 asks the server's portmapper,
	// falling back to 2049.
	Port int
	// MountPort is the mount service's port; 0 asks the portmapper.
	MountPort int
	// Export is the exported path, e.g. "/volume1/home".
	Export string
	// UID, GID and Groups are who the server takes this user to be
	// (AUTH_SYS). The server believes them as sent.
	UID, GID uint32
	Groups   []uint32
	// Machine is the host name sent with them; "" sends os.Hostname.
	Machine string
}

// FH is a file handle: the server's name for a file, at most 64 bytes.
type FH []byte

const maxFH = 64

// FileType is what kind of object an Attr describes (ftype3).
type FileType uint32

const (
	TypeReg  FileType = 1
	TypeDir  FileType = 2
	TypeBlk  FileType = 3
	TypeChr  FileType = 4
	TypeLnk  FileType = 5
	TypeSock FileType = 6
	TypeFIFO FileType = 7
)

// Attr is the part of a file's attributes (fattr3) shuttle uses.
type Attr struct {
	Type   FileType
	Mode   uint32
	Size   uint64
	FileID uint64
	Mtime  time.Time
}

// Client is a mounted export.
type Client struct {
	mount  *conn
	nfs    *conn
	export string
	root   FH
	rsize  uint32
	wsize  uint32
}

// Dial mounts o.Export and connects to the NFS service.
func Dial(ctx context.Context, o *Options) (*Client, error) {
	machine := o.Machine
	if machine == "" {
		machine, _ = os.Hostname()
	}
	cred := authSysBody(machine, o.UID, o.GID, o.Groups)
	mport, nport, err := ports(ctx, o)
	if err != nil {
		return nil, err
	}
	mc, err := dial(ctx, net.JoinHostPort(o.Host, strconv.Itoa(mport)), cred, true)
	if err != nil {
		return nil, fmt.Errorf("nfs: connecting to the mount service: %w", err)
	}
	root, err := mnt(ctx, mc, o.Export)
	if err != nil {
		mc.close()
		return nil, err
	}
	nc, err := dial(ctx, net.JoinHostPort(o.Host, strconv.Itoa(nport)), cred, true)
	if err != nil {
		mc.close()
		return nil, fmt.Errorf("nfs: connecting to the NFS service: %w", err)
	}
	c := &Client{mount: mc, nfs: nc, export: o.Export, root: root}
	info, err := c.FSInfo(ctx, root)
	if err != nil {
		c.Close()
		return nil, err
	}
	c.rsize = ioSize(info.RTPref, info.RTMax)
	c.wsize = ioSize(info.WTPref, info.WTMax)
	return c, nil
}

// ioSize picks a transfer size: the server's preferred one, within its
// maximum and ours.
func ioSize(pref, most uint32) uint32 {
	n := pref
	if n == 0 {
		n = most
	}
	if most != 0 && n > most {
		n = most
	}
	if n == 0 {
		n = 64 << 10
	}
	return min(n, maxIO)
}

// ports finds the mount and NFS services, asking the portmapper for any the
// options leave out.
func ports(ctx context.Context, o *Options) (mport, nport int, err error) {
	mport, nport = o.MountPort, o.Port
	if mport != 0 && nport != 0 {
		return mport, nport, nil
	}
	pm, err := dial(ctx, net.JoinHostPort(o.Host, strconv.Itoa(portmapPort)), nil, false)
	if err != nil {
		if mport == 0 {
			return 0, 0, fmt.Errorf("nfs: cannot reach the portmapper to find the mount service: %w "+
				"(an NFSv4-only server has none: mount it and use a mounted folder)", err)
		}
		return mport, nfsPort, nil
	}
	defer pm.close()
	if mport == 0 {
		if mport, err = getPort(ctx, pm, mountProg); err != nil {
			return 0, 0, err
		}
	}
	if nport == 0 {
		if nport, err = getPort(ctx, pm, nfsProg); err != nil {
			return 0, 0, err
		}
	}
	return mport, nport, nil
}

// getPort asks the portmapper where p listens over TCP (PMAPPROC_GETPORT).
func getPort(ctx context.Context, pm *conn, p program) (int, error) {
	d, err := pm.call(ctx, portmapProg, 3, func(e *encoder) {
		e.u32(p.prog)
		e.u32(p.vers)
		e.u32(6) // IPPROTO_TCP
		e.u32(0)
	})
	if err != nil {
		return 0, err
	}
	port := d.u32()
	if d.err != nil {
		return 0, d.err
	}
	if port == 0 || port > 65535 {
		return 0, fmt.Errorf("nfs: the server runs no NFSv3 %s over TCP (%s)", p.name, v4Only)
	}
	return int(port), nil
}

// mnt asks the mount service for the export's root handle (MOUNTPROC3_MNT).
func mnt(ctx context.Context, mc *conn, export string) (FH, error) {
	d, err := mc.call(ctx, mountProg, 1, func(e *encoder) { e.string(export) })
	if err != nil {
		return nil, err
	}
	if st := MountStatus(d.u32()); d.err == nil && st != 0 {
		return nil, &MountError{Export: export, Status: st}
	}
	fh := FH(d.opaque(maxFH))
	n := d.u32()
	usable := n == 0
	for range min(n, 64) {
		f := d.u32()
		usable = usable || f == authSys || f == authNone
	}
	if d.err != nil {
		return nil, fmt.Errorf("nfs: mount reply: %w", d.err)
	}
	if !usable {
		return nil, fmt.Errorf("nfs: export %q wants Kerberos (sec=krb5), which shuttle does not support", export)
	}
	return clone(fh), nil
}

// Root is the export's root folder.
func (c *Client) Root() FH { return c.root }

// Close tells the server the export is no longer in use and disconnects.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// MOUNTPROC3_UMNT only tidies the server's list of clients; a server
	// that does not answer changes nothing.
	_, _ = c.mount.call(ctx, mountProg, 3, func(e *encoder) { e.string(c.export) })
	c.mount.close()
	c.nfs.close()
	return nil
}

func clone(b []byte) []byte { return append([]byte(nil), b...) }
