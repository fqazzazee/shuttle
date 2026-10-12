// SPDX-License-Identifier: GPL-2.0-or-later

package nfs3

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// NFSv3 procedures (RFC 1813).
const (
	procGetAttr     = 1
	procSetAttr     = 2
	procLookup      = 3
	procRead        = 6
	procWrite       = 7
	procCreate      = 8
	procMkdir       = 9
	procRemove      = 12
	procRmdir       = 13
	procRename      = 14
	procReadDirPlus = 17
	procFSInfo      = 19
	procCommit      = 21
)

// maxName bounds a name in a reply. Linux allows 255 bytes; this leaves room
// for servers that count characters, not bytes.
const maxName = 1024

// Stable says how far a WRITE must reach before the server answers.
type Stable uint32

const (
	Unstable Stable = 0 // the server's memory; COMMIT makes it durable
	DataSync Stable = 1
	FileSync Stable = 2
)

// Verifier changes when the server restarts: unstable writes made before a
// change may be lost.
type Verifier [8]byte

// call runs an NFS procedure and checks its status. On a status other than
// OK, the rest of the reply (attributes shuttle does not use) is skipped.
func (c *Client) call(ctx context.Context, op string, proc uint32, args func(*encoder)) (*decoder, error) {
	d, err := c.nfs.call(ctx, nfsProg, proc, args)
	if err != nil {
		return nil, err
	}
	st := Status(d.u32())
	if d.err != nil {
		return nil, fmt.Errorf("nfs %s: %w", op, d.err)
	}
	if st != StatusOK {
		return nil, &Error{Op: op, Status: st}
	}
	return d, nil
}

// done checks that a reply parsed.
func done(op string, d *decoder) error {
	if d.err != nil {
		return fmt.Errorf("nfs %s: malformed reply: %w", op, d.err)
	}
	return nil
}

func diropargs(e *encoder, dir FH, name string) {
	e.opaque(dir)
	e.string(name)
}

func decodeTime(d *decoder) time.Time {
	sec, nsec := d.u32(), d.u32()
	if nsec >= 1e9 {
		d.fail("bad time: %d ns", nsec)
		return time.Time{}
	}
	return time.Unix(int64(sec), int64(nsec))
}

// decodeAttr reads a fattr3.
func decodeAttr(d *decoder) Attr {
	var a Attr
	a.Type = FileType(d.u32())
	a.Mode = d.u32()
	d.u32() // nlink
	d.u32() // uid
	d.u32() // gid
	a.Size = d.u64()
	d.u64() // used
	d.u64() // rdev
	d.u64() // fsid
	a.FileID = d.u64()
	decodeTime(d) // atime
	a.Mtime = decodeTime(d)
	decodeTime(d) // ctime
	return a
}

// postOpAttr reads a post_op_attr: attributes the server may leave out.
func postOpAttr(d *decoder) (Attr, bool) {
	if !d.bool() {
		return Attr{}, false
	}
	return decodeAttr(d), true
}

func postOpFH(d *decoder) FH {
	if !d.bool() {
		return nil
	}
	return clone(d.opaque(maxFH))
}

// wcc skips a wcc_data: a file's attributes before and after a change.
func wcc(d *decoder) {
	if d.bool() {
		d.u64()       // size
		decodeTime(d) // mtime
		decodeTime(d) // ctime
	}
	postOpAttr(d)
}

// GetAttr returns fh's attributes.
func (c *Client) GetAttr(ctx context.Context, fh FH) (Attr, error) {
	d, err := c.call(ctx, "getattr", procGetAttr, func(e *encoder) { e.opaque(fh) })
	if err != nil {
		return Attr{}, err
	}
	a := decodeAttr(d)
	return a, done("getattr", d)
}

// Lookup finds name in folder dir. It does not follow a symlink: a symlink
// named name is returned as one.
func (c *Client) Lookup(ctx context.Context, dir FH, name string) (FH, Attr, error) {
	d, err := c.call(ctx, "lookup", procLookup, func(e *encoder) { diropargs(e, dir, name) })
	if err != nil {
		return nil, Attr{}, err
	}
	fh := clone(d.opaque(maxFH))
	a, ok := postOpAttr(d)
	if err := done("lookup", d); err != nil {
		return nil, Attr{}, err
	}
	if !ok {
		if a, err = c.GetAttr(ctx, fh); err != nil {
			return nil, Attr{}, err
		}
	}
	return fh, a, nil
}

// SetMtime sets fh's modification time, and its access time to the same.
func (c *Client) SetMtime(ctx context.Context, fh FH, t time.Time) error {
	sec, nsec := t.Unix(), t.Nanosecond()
	if sec < 0 || sec > 1<<32-1 {
		return &Error{Op: "setattr", Status: StatusInval} // NFSv3 times run from 1970 to 2106
	}
	d, err := c.call(ctx, "setattr", procSetAttr, func(e *encoder) {
		e.opaque(fh)
		e.bool(false) // mode
		e.bool(false) // uid
		e.bool(false) // gid
		e.bool(false) // size
		for range 2 { // atime, mtime
			e.u32(2)            // SET_TO_CLIENT_TIME
			e.u32(uint32(sec))  //nolint:gosec // range checked above
			e.u32(uint32(nsec)) //nolint:gosec // 0 to 999,999,999
		}
		e.bool(false) // no guard
	})
	if err != nil {
		return err
	}
	wcc(d)
	return done("setattr", d)
}

// ReadSize is the most Read returns at once.
func (c *Client) ReadSize() int { return int(c.rsize) }

// WriteSize is the most Write takes at once.
func (c *Client) WriteSize() int { return int(c.wsize) }

// Read reads up to ReadSize bytes of fh from off. eof is true when the data
// ends the file.
func (c *Client) Read(ctx context.Context, fh FH, off uint64, n int) (data []byte, eof bool, err error) {
	n = max(min(n, int(c.rsize)), 0)
	d, err := c.call(ctx, "read", procRead, func(e *encoder) {
		e.opaque(fh)
		e.u64(off)
		e.u32(size(n))
	})
	if err != nil {
		return nil, false, err
	}
	postOpAttr(d)
	count := d.u32()
	eof = d.bool()
	data = d.opaque(n)
	if err := done("read", d); err != nil {
		return nil, false, err
	}
	if int(count) != len(data) {
		return nil, false, fmt.Errorf("nfs read: the server said %d bytes and sent %d", count, len(data))
	}
	if len(data) == 0 && !eof {
		return nil, false, errors.New("nfs read: the server sent nothing, and not the end of the file")
	}
	return data, eof, nil
}

// Write writes data, at most WriteSize bytes, to fh at off. It returns how
// much the server took, how durably, and the server's verifier.
func (c *Client) Write(ctx context.Context, fh FH, off uint64, data []byte, how Stable) (int, Stable, Verifier, error) {
	var v Verifier
	if len(data) > int(c.wsize) {
		return 0, 0, v, fmt.Errorf("nfs write: %d bytes, more than the %d the server takes", len(data), c.wsize)
	}
	d, err := c.call(ctx, "write", procWrite, func(e *encoder) {
		e.opaque(fh)
		e.u64(off)
		e.u32(size(len(data)))
		e.u32(uint32(how))
		e.opaque(data)
	})
	if err != nil {
		return 0, 0, v, err
	}
	wcc(d)
	count := d.u32()
	committed := Stable(d.u32())
	copy(v[:], d.fixed(8))
	if err := done("write", d); err != nil {
		return 0, 0, v, err
	}
	if count > size(len(data)) {
		return 0, 0, v, fmt.Errorf("nfs write: the server took %d of %d bytes", count, len(data))
	}
	return int(count), committed, v, nil
}

// Commit makes every unstable write to fh durable, and returns the
// server's verifier: if it differs from the writes', they may be lost.
func (c *Client) Commit(ctx context.Context, fh FH) (Verifier, error) {
	var v Verifier
	d, err := c.call(ctx, "commit", procCommit, func(e *encoder) {
		e.opaque(fh)
		e.u64(0)
		e.u32(0) // to the end of the file
	})
	if err != nil {
		return v, err
	}
	wcc(d)
	copy(v[:], d.fixed(8))
	return v, done("commit", d)
}

// Create makes an empty file name in dir. It fails with fs.ErrExist if name
// is there already (GUARDED).
func (c *Client) Create(ctx context.Context, dir FH, name string, mode uint32) (FH, error) {
	d, err := c.call(ctx, "create", procCreate, func(e *encoder) {
		diropargs(e, dir, name)
		e.u32(1) // GUARDED
		setMode(e, mode)
	})
	if err != nil {
		return nil, err
	}
	return c.made(ctx, "create", d, dir, name)
}

// Mkdir makes folder name in dir.
func (c *Client) Mkdir(ctx context.Context, dir FH, name string, mode uint32) (FH, error) {
	d, err := c.call(ctx, "mkdir", procMkdir, func(e *encoder) {
		diropargs(e, dir, name)
		setMode(e, mode)
	})
	if err != nil {
		return nil, err
	}
	return c.made(ctx, "mkdir", d, dir, name)
}

// setMode writes a sattr3 that sets the mode and nothing else.
func setMode(e *encoder, mode uint32) {
	e.bool(true)
	e.u32(mode)
	e.bool(false) // uid
	e.bool(false) // gid
	e.bool(false) // size
	e.u32(0)      // atime: don't change
	e.u32(0)      // mtime: don't change
}

// made reads the handle a CREATE or MKDIR returned, looking it up when the
// server left it out.
func (c *Client) made(ctx context.Context, op string, d *decoder, dir FH, name string) (FH, error) {
	fh := postOpFH(d)
	postOpAttr(d)
	wcc(d)
	if err := done(op, d); err != nil {
		return nil, err
	}
	if fh == nil {
		var err error
		if fh, _, err = c.Lookup(ctx, dir, name); err != nil {
			return nil, err
		}
	}
	return fh, nil
}

// Remove deletes file name from dir.
func (c *Client) Remove(ctx context.Context, dir FH, name string) error {
	return c.dirop(ctx, "remove", procRemove, dir, name)
}

// Rmdir deletes empty folder name from dir.
func (c *Client) Rmdir(ctx context.Context, dir FH, name string) error {
	return c.dirop(ctx, "rmdir", procRmdir, dir, name)
}

func (c *Client) dirop(ctx context.Context, op string, proc uint32, dir FH, name string) error {
	d, err := c.call(ctx, op, proc, func(e *encoder) { diropargs(e, dir, name) })
	if err != nil {
		return err
	}
	wcc(d)
	return done(op, d)
}

// Rename moves from/fromName to to/toName, replacing what is there.
func (c *Client) Rename(ctx context.Context, from FH, fromName string, to FH, toName string) error {
	d, err := c.call(ctx, "rename", procRename, func(e *encoder) {
		diropargs(e, from, fromName)
		diropargs(e, to, toName)
	})
	if err != nil {
		return err
	}
	wcc(d)
	wcc(d)
	return done("rename", d)
}

// Entry is one name in a folder listing. FH and Attr may be missing, when
// the server leaves them out: Lookup the name for them.
type Entry struct {
	Name    string
	FH      FH
	Attr    Attr
	HasAttr bool
}

// maxEntries bounds one folder's listing.
const maxEntries = 1 << 22

// ReadDir lists folder dir with READDIRPLUS, calling fn for each entry
// other than "." and "..", in the server's order.
func (c *Client) ReadDir(ctx context.Context, dir FH, fn func(Entry) error) error {
	var cookie uint64
	var verf Verifier
	total := 0
	for {
		from := cookie
		d, err := c.call(ctx, "readdirplus", procReadDirPlus, func(e *encoder) {
			e.opaque(dir)
			e.u64(cookie)
			e.fixed(verf[:])
			e.u32(32 << 10)  // dircount: names and cookies
			e.u32(128 << 10) // maxcount: the whole reply
		})
		if err != nil {
			return err
		}
		postOpAttr(d)
		copy(verf[:], d.fixed(8))
		n := 0
		for d.bool() {
			d.u64() // fileid
			var e Entry
			e.Name = d.string(maxName)
			cookie = d.u64()
			e.Attr, e.HasAttr = postOpAttr(d)
			e.FH = postOpFH(d)
			if d.err != nil {
				break
			}
			n++
			if e.Name == "." || e.Name == ".." {
				continue
			}
			if total++; total > maxEntries {
				return fmt.Errorf("nfs readdirplus: more than %d entries in one folder", maxEntries)
			}
			if err := fn(e); err != nil {
				return err
			}
		}
		eof := d.bool()
		if err := done("readdirplus", d); err != nil {
			return err
		}
		if eof {
			return nil
		}
		if n == 0 || cookie == from {
			return errors.New("nfs readdirplus: the server's listing does not move on")
		}
	}
}

// FSInfo is what the server says about transfer sizes (FSINFO).
type FSInfo struct {
	RTMax, RTPref, WTMax, WTPref uint32
}

// FSInfo asks the server for its transfer sizes.
func (c *Client) FSInfo(ctx context.Context, root FH) (FSInfo, error) {
	d, err := c.call(ctx, "fsinfo", procFSInfo, func(e *encoder) { e.opaque(root) })
	if err != nil {
		return FSInfo{}, err
	}
	var f FSInfo
	postOpAttr(d)
	f.RTMax, f.RTPref = d.u32(), d.u32()
	d.u32() // rtmult
	f.WTMax, f.WTPref = d.u32(), d.u32()
	return f, done("fsinfo", d)
}
