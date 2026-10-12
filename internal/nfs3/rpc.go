// SPDX-License-Identifier: GPL-2.0-or-later

package nfs3

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

// ONC RPC version 2 over TCP (RFC 5531), as much of it as an NFSv3 client
// needs.

const (
	rpcVersion = 2

	msgCall  = 0
	msgReply = 1

	replyAccepted = 0
	replyDenied   = 1

	acceptSuccess      = 0
	acceptProgUnavail  = 1
	acceptProgMismatch = 2
	acceptProcUnavail  = 3
	acceptGarbageArgs  = 4
	acceptSystemErr    = 5

	rejectRPCMismatch = 0
	rejectAuthError   = 1

	authNone = 0
	authSys  = 1
)

// maxIO is the most data one READ or WRITE moves, whatever the server
// offers.
const maxIO = 1 << 20

// maxRecord bounds one reply: the largest READ plus room for its headers,
// or a page of a folder listing. A server that sends more is cut off.
const maxRecord = maxIO + 64<<10

// callTimeout is how long a call waits for its reply when its context has
// no earlier deadline: long enough for a 1 MiB write over a slow link.
const callTimeout = 60 * time.Second

// ErrClosed is returned by calls on a closed client.
var ErrClosed = errors.New("nfs: connection closed")

// conn is one TCP connection to one RPC program. Calls may run in parallel:
// replies are matched to them by xid.
type conn struct {
	nc   net.Conn
	cred []byte // AUTH_SYS body, or nil for AUTH_NONE

	wmu sync.Mutex // one record written at a time

	mu      sync.Mutex
	pending map[uint32]chan []byte
	xid     uint32
	err     error         // why the connection stopped; set once
	done    chan struct{} // closed when it stops
}

// dial connects to addr. With reserved, it first tries a source port below
// 1024, which many servers want ("secure", the Linux default) and which
// only root, or a process allowed to, can bind.
func dial(ctx context.Context, addr string, cred []byte, reserved bool) (*conn, error) {
	var nc net.Conn
	var err error
	if reserved {
		nc, err = dialReserved(ctx, addr)
	}
	if nc == nil && err == nil {
		var d net.Dialer
		nc, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	return newConn(nc, cred), nil
}

func newConn(nc net.Conn, cred []byte) *conn {
	c := &conn{
		nc: nc, cred: cred, pending: map[uint32]chan []byte{}, done: make(chan struct{}),
		xid: rand.Uint32(), //nolint:gosec // xids only tell calls apart on this connection; they guard nothing
	}
	go c.read()
	return c
}

// dialReserved connects from a port below 1024. It returns no connection and
// no error when this process may not bind one, the usual case.
func dialReserved(ctx context.Context, addr string) (net.Conn, error) {
	for range 8 {
		d := net.Dialer{LocalAddr: &net.TCPAddr{Port: 600 + rand.IntN(424)}} //nolint:gosec // any free port will do
		nc, err := d.DialContext(ctx, "tcp", addr)
		switch {
		case err == nil:
			return nc, nil
		case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
			return nil, nil
		case errors.Is(err, syscall.EADDRINUSE), errors.Is(err, syscall.EADDRNOTAVAIL):
			continue // taken, or in TIME_WAIT with this server: another one
		default:
			return nil, err
		}
	}
	return nil, nil
}

func (c *conn) close() { c.stop(ErrClosed) }

// stop ends the connection: every waiting call and every later one fails
// with err.
func (c *conn) stop(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	close(c.done)
	c.nc.Close()
}

// read hands each reply to the call waiting for it, until the connection
// fails. A reply nobody waits for (its call gave up) is dropped.
func (c *conn) read() {
	r := bufio.NewReader(c.nc)
	for {
		rec, err := readRecord(r)
		if err == nil && len(rec) < 8 {
			err = errShort
		}
		if err == nil && binary.BigEndian.Uint32(rec[4:]) != msgReply {
			err = errors.New("the server sent a call, not a reply")
		}
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				err = ErrClosed
			}
			c.stop(fmt.Errorf("nfs: connection lost: %w", err))
			return
		}
		xid := binary.BigEndian.Uint32(rec)
		c.mu.Lock()
		ch := c.pending[xid]
		delete(c.pending, xid)
		c.mu.Unlock()
		if ch != nil {
			ch <- rec[8:]
		}
	}
}

// readRecord reads one record, which may come in several fragments, and
// refuses one longer than maxRecord before reading it.
func readRecord(r io.Reader) ([]byte, error) {
	var rec []byte
	var hdr [4]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, err
		}
		h := binary.BigEndian.Uint32(hdr[:])
		n := int(h & 0x7fffffff)
		if len(rec)+n > maxRecord {
			return nil, fmt.Errorf("reply of more than %d bytes", maxRecord)
		}
		rec = append(rec, make([]byte, n)...)
		if _, err := io.ReadFull(r, rec[len(rec)-n:]); err != nil {
			return nil, err
		}
		if h&0x80000000 != 0 {
			return rec, nil
		}
	}
}

// program names an RPC program and version, for calls and their errors.
type program struct {
	name       string
	prog, vers uint32
}

// v4Only is what to do about a server that has turned NFSv3 off.
const v4Only = "an NFSv4-only server: mount it and use a mounted folder"

var (
	portmapProg = program{"portmapper", 100000, 2}
	mountProg   = program{"mount service", 100005, 3}
	nfsProg     = program{"NFS service", 100003, 3}
)

// call sends procedure proc with the arguments args writes, and returns a
// decoder positioned at the procedure's results.
func (c *conn) call(ctx context.Context, p program, proc uint32, args func(*encoder)) (*decoder, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, callTimeout)
		defer cancel()
	}
	ch := make(chan []byte, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return nil, c.err
	}
	c.xid++
	xid := c.xid
	c.pending[xid] = ch
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		delete(c.pending, xid)
		c.mu.Unlock()
	}

	e := &encoder{b: make([]byte, 4, 128)} // room for the record mark
	e.u32(xid)
	e.u32(msgCall)
	e.u32(rpcVersion)
	e.u32(p.prog)
	e.u32(p.vers)
	e.u32(proc)
	if c.cred != nil {
		e.u32(authSys)
		e.opaque(c.cred)
	} else {
		e.u32(authNone)
		e.opaque(nil)
	}
	e.u32(authNone) // verifier
	e.opaque(nil)
	args(e)
	binary.BigEndian.PutUint32(e.b, 0x80000000|size(len(e.b)-4))

	if err := c.write(ctx, e.b); err != nil {
		forget()
		return nil, err
	}
	select {
	case rec := <-ch:
		return acceptedResults(p, rec)
	case <-ctx.Done():
		forget()
		return nil, fmt.Errorf("nfs: %s did not answer: %w", p.name, ctx.Err())
	case <-c.done:
		forget()
		return nil, c.err
	}
}

func (c *conn) write(ctx context.Context, rec []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if d, ok := ctx.Deadline(); ok {
		_ = c.nc.SetWriteDeadline(d) // fails only once closed, and then so does Write
	}
	if _, err := c.nc.Write(rec); err != nil {
		// Part of a record may be on the wire: nothing after it would parse.
		c.stop(fmt.Errorf("nfs: connection lost: %w", err))
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.err
	}
	return nil
}

// acceptedResults checks a reply's status and returns its results.
func acceptedResults(p program, rec []byte) (*decoder, error) {
	d := &decoder{b: rec}
	switch d.u32() {
	case replyAccepted:
		d.u32()       // verifier flavor
		d.opaque(400) // verifier body
		stat := d.u32()
		if d.err != nil {
			return nil, d.err
		}
		switch stat {
		case acceptSuccess:
			return d, nil
		case acceptProgUnavail:
			if p == mountProg {
				return nil, fmt.Errorf("nfs: the server runs no %s (%s)", p.name, v4Only)
			}
			return nil, fmt.Errorf("nfs: the server runs no %s", p.name)
		case acceptProgMismatch:
			lo, hi := d.u32(), d.u32()
			err := fmt.Errorf("nfs: the server's %s speaks versions %d to %d, not %d", p.name, lo, hi, p.vers)
			// With NFSv3 turned off, the Linux kernel server keeps MOUNT v1
			// and v2, and its portmapper gives their port when asked for v3.
			if (p == mountProg && hi < p.vers) || (p == nfsProg && lo > p.vers) {
				err = fmt.Errorf("%w (%s)", err, v4Only)
			}
			return nil, err
		case acceptProcUnavail:
			return nil, fmt.Errorf("nfs: the server's %s lacks a call shuttle needs", p.name)
		case acceptGarbageArgs:
			return nil, fmt.Errorf("nfs: the server's %s could not read the call", p.name)
		case acceptSystemErr:
			return nil, fmt.Errorf("nfs: the server's %s failed", p.name)
		}
		return nil, fmt.Errorf("nfs: %s: unknown reply status %d", p.name, stat)
	case replyDenied:
		switch why := d.u32(); why {
		case rejectRPCMismatch:
			return nil, fmt.Errorf("nfs: the server's %s does not speak RPC version 2", p.name)
		case rejectAuthError:
			return nil, &AuthError{Program: p.name, Stat: d.u32()}
		}
	}
	if d.err != nil {
		return nil, d.err
	}
	return nil, fmt.Errorf("nfs: %s: malformed reply", p.name)
}

// AuthError is a call the server refused for its credentials.
type AuthError struct {
	Program string
	Stat    uint32
}

func (e *AuthError) Error() string {
	why := "credentials refused"
	switch e.Stat {
	case 5: // AUTH_TOOWEAK
		why = "it wants stronger authentication (Kerberos?) than shuttle offers"
	case 1, 2: // AUTH_BADCRED, AUTH_REJECTEDCRED
		why = "it refused this computer's user and group IDs"
	}
	return fmt.Sprintf("nfs: the server's %s refused the call: %s", e.Program, why)
}

func (e *AuthError) Is(target error) bool { return target == os.ErrPermission }

// authSysBody is an AUTH_SYS credential (RFC 5531 appendix A): who the
// server should take this user to be. The server believes it as sent.
func authSysBody(machine string, uid, gid uint32, groups []uint32) []byte {
	if len(machine) > 255 {
		machine = machine[:255]
	}
	if len(groups) > 16 {
		groups = groups[:16]
	}
	e := &encoder{}
	e.u32(uint32(time.Now().Unix())) //nolint:gosec // any 32 bits do: the stamp only tells credentials apart
	e.string(machine)
	e.u32(uid)
	e.u32(gid)
	e.u32(size(len(groups)))
	for _, g := range groups {
		e.u32(g)
	}
	return e.b
}
