// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package nfs3

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A fake server: the other end of a pipe, answering each call as the test
// says. It is how these tests play a server that misbehaves.

type fakeCall struct {
	xid, prog, proc uint32
	args            []byte
}

// fakeConn returns a conn whose server answers each call with the reply
// body answer returns; nil sends nothing. Each call is answered in a
// goroutine of its own, so answers may come back out of order.
func fakeConn(t testing.TB, answer func(fakeCall) []byte) (*conn, net.Conn) {
	cli, srv := net.Pipe()
	var wmu sync.Mutex
	go func() {
		r := bufio.NewReader(srv)
		for {
			rec, err := readRecord(r)
			if err != nil {
				return
			}
			d := &decoder{b: rec}
			var fc fakeCall
			fc.xid = d.u32()
			d.u32() // CALL
			d.u32() // RPC version
			fc.prog = d.u32()
			d.u32() // program version
			fc.proc = d.u32()
			d.u32()
			d.opaque(400) // credential
			d.u32()
			d.opaque(400) // verifier
			fc.args = d.b
			go func() {
				body := answer(fc)
				if body == nil {
					return
				}
				e := &encoder{b: make([]byte, 4)}
				e.u32(fc.xid)
				e.u32(msgReply)
				e.b = append(e.b, body...)
				binary.BigEndian.PutUint32(e.b, 0x80000000|uint32(len(e.b)-4))
				wmu.Lock()
				defer wmu.Unlock()
				srv.Write(e.b)
			}()
		}
	}()
	c := newConn(cli, nil)
	t.Cleanup(func() {
		c.close()
		srv.Close()
	})
	return c, srv
}

// accepted is a successful reply carrying the results results writes.
func accepted(results func(*encoder)) []byte {
	e := &encoder{}
	e.u32(replyAccepted)
	e.u32(authNone)
	e.opaque(nil)
	e.u32(acceptSuccess)
	if results != nil {
		results(e)
	}
	return e.b
}

func TestRepliesFindTheirCalls(t *testing.T) {
	c, _ := fakeConn(t, func(fc fakeCall) []byte {
		time.Sleep(time.Duration(20-fc.proc) * 5 * time.Millisecond) // the last call is answered first
		return accepted(func(e *encoder) { e.u32(fc.proc * 10) })
	})
	var wg sync.WaitGroup
	for proc := uint32(1); proc <= 20; proc++ {
		wg.Go(func() {
			d, err := c.call(t.Context(), nfsProg, proc, func(*encoder) {})
			if err != nil {
				t.Error(err)
				return
			}
			if got := d.u32(); got != proc*10 {
				t.Errorf("call %d got the reply for call %d", proc, got/10)
			}
		})
	}
	wg.Wait()
}

func TestCallGivesUpAtItsDeadline(t *testing.T) {
	c, _ := fakeConn(t, func(fakeCall) []byte { return nil })
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.call(ctx, nfsProg, 1, func(*encoder) {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("gave up after %v", d)
	}
	if len(c.pending) != 0 {
		t.Errorf("%d calls still pending", len(c.pending))
	}
}

// A server that claims a huge reply is cut off before shuttle allocates it.
func TestOversizedReply(t *testing.T) {
	c, srv := fakeConn(t, func(fakeCall) []byte { return nil })
	go func() {
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], 0x80000000|1<<30)
		srv.Write(hdr[:])
	}()
	_, err := c.call(t.Context(), nfsProg, 1, func(*encoder) {})
	if err == nil || !strings.Contains(err.Error(), "reply of more than") {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.call(t.Context(), nfsProg, 1, func(*encoder) {}); err == nil {
		t.Error("a call after the connection failed worked")
	}
}

func TestServerHangsUp(t *testing.T) {
	var c *conn
	c, srv := fakeConn(t, func(fakeCall) []byte { return nil })
	go func() {
		time.Sleep(50 * time.Millisecond)
		srv.Close()
	}()
	start := time.Now()
	_, err := c.call(t.Context(), nfsProg, 1, func(*encoder) {})
	if err == nil || !strings.Contains(err.Error(), "connection lost") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("noticed after %v", d)
	}
}

func TestRefusals(t *testing.T) {
	mismatch := func(lo, hi uint32) func(*encoder) {
		return func(e *encoder) {
			e.u32(replyAccepted)
			e.u32(authNone)
			e.opaque(nil)
			e.u32(acceptProgMismatch)
			e.u32(lo)
			e.u32(hi)
		}
	}
	unavail := func(e *encoder) {
		e.u32(replyAccepted)
		e.u32(authNone)
		e.opaque(nil)
		e.u32(acceptProgUnavail)
	}
	failed := func(err error) bool { return err != nil }
	for _, tc := range []struct {
		name   string
		prog   program
		reply  func(*encoder)
		check  func(error) bool
		want   string
		v4Only bool // the error says to mount an NFSv4-only server
	}{
		{"auth too weak", nfsProg, func(e *encoder) { e.u32(replyDenied); e.u32(rejectAuthError); e.u32(5) },
			func(err error) bool { return errors.Is(err, os.ErrPermission) }, "Kerberos", false},
		{"bad credentials", nfsProg, func(e *encoder) { e.u32(replyDenied); e.u32(rejectAuthError); e.u32(1) },
			func(err error) bool { return errors.Is(err, os.ErrPermission) }, "user and group IDs", false},
		{"NFS v4 alone", nfsProg, mismatch(4, 4), failed, "versions 4 to 4, not 3", true},
		{"NFS v2 alone", nfsProg, mismatch(2, 2), failed, "versions 2 to 2, not 3", false},
		// What the Linux kernel server answers with NFSv3 turned off.
		{"mount v1 and v2", mountProg, mismatch(1, 2), failed, "mount service speaks versions 1 to 2, not 3", true},
		{"mount v4", mountProg, mismatch(4, 4), failed, "versions 4 to 4, not 3", false},
		{"no NFS service", nfsProg, unavail, failed, "runs no NFS service", false},
		{"no mount service", mountProg, unavail, failed, "runs no mount service", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := fakeConn(t, func(fakeCall) []byte {
				e := &encoder{}
				tc.reply(e)
				return e.b
			})
			_, err := c.call(t.Context(), tc.prog, 1, func(*encoder) {})
			if !tc.check(err) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one saying %q", err, tc.want)
			}
			if got := strings.Contains(err.Error(), "NFSv4-only server: mount it"); got != tc.v4Only {
				t.Errorf("err = %v; says to mount an NFSv4-only server: %v, want %v", err, got, tc.v4Only)
			}
		})
	}
}

func TestMountReplies(t *testing.T) {
	mount := func(t *testing.T, results func(*encoder)) error {
		c, _ := fakeConn(t, func(fakeCall) []byte { return accepted(results) })
		_, err := mnt(t.Context(), c, "/export")
		return err
	}
	err := mount(t, func(e *encoder) { e.u32(13) }) // MNT3ERR_ACCES
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "insecure") {
		t.Errorf("refused: %v", err)
	}
	err = mount(t, func(e *encoder) { e.u32(2) }) // MNT3ERR_NOENT
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), `no export "/export"`) {
		t.Errorf("no export: %v", err)
	}
	err = mount(t, func(e *encoder) { // OK, but Kerberos only
		e.u32(0)
		e.opaque([]byte{1, 2, 3, 4})
		e.u32(1)
		e.u32(390003) // RPCSEC_GSS krb5
	})
	if err == nil || !strings.Contains(err.Error(), "Kerberos") {
		t.Errorf("Kerberos-only: %v", err)
	}
	err = mount(t, func(e *encoder) {
		e.u32(0)
		e.opaque(make([]byte, 65)) // a handle longer than NFSv3 allows
		e.u32(0)
	})
	if err == nil {
		t.Error("a 65-byte handle was taken")
	}
}

// A listing whose pages never end, or never move on, stops with an error.
func TestReadDirMustMoveOn(t *testing.T) {
	nc, _ := fakeConn(t, func(fakeCall) []byte {
		return accepted(func(e *encoder) {
			e.u32(uint32(StatusOK))
			e.bool(false) // dir attributes
			e.fixed(make([]byte, 8))
			e.bool(true) // an entry
			e.u64(1)
			e.string("same")
			e.u64(7)      // the same cookie every time
			e.bool(false) // attributes
			e.bool(false) // handle
			e.bool(false) // no more entries
			e.bool(false) // not the end
		})
	})
	c := &Client{nfs: nc, rsize: 4096, wsize: 4096}
	n := 0
	err := c.ReadDir(t.Context(), FH{1}, func(Entry) error { n++; return nil })
	if err == nil || n > 2 {
		t.Errorf("err = %v after %d entries", err, n)
	}
}

// Whatever a server sends, the client returns an error or a value, and
// never panics or hangs.
func FuzzReplies(f *testing.F) {
	f.Add(accepted(nil))
	f.Add(accepted(func(e *encoder) { e.u32(0); e.opaque([]byte{1, 2}); e.bool(false); e.bool(false) }))
	f.Add(accepted(func(e *encoder) {
		e.u32(0)
		e.bool(true)
		e.b = append(e.b, make([]byte, 84)...)
		e.u32(3)
		e.bool(true)
		e.opaque([]byte("abc"))
	}))
	f.Add(accepted(func(e *encoder) { e.u32(0); e.bool(false); e.fixed(make([]byte, 8)); e.bool(false); e.bool(true) }))
	f.Add([]byte{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 5})
	f.Fuzz(func(t *testing.T, reply []byte) {
		nc, _ := fakeConn(t, func(fakeCall) []byte { return reply })
		c := &Client{mount: nc, nfs: nc, export: "/", rsize: 4096, wsize: 4096}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		fh := FH{1, 2, 3}
		c.GetAttr(ctx, fh)
		c.Lookup(ctx, fh, "x")
		c.SetMtime(ctx, fh, time.Unix(1e9, 0))
		c.Read(ctx, fh, 0, 4096)
		c.Write(ctx, fh, 0, []byte("data"), Unstable)
		c.Commit(ctx, fh)
		c.Create(ctx, fh, "x", 0o644)
		c.Mkdir(ctx, fh, "x", 0o755)
		c.Remove(ctx, fh, "x")
		c.Rmdir(ctx, fh, "x")
		c.Rename(ctx, fh, "x", fh, "y")
		c.ReadDir(ctx, fh, func(Entry) error { return nil })
		c.FSInfo(ctx, fh)
		c.FSStat(ctx, fh)
		mnt(ctx, nc, "/")
		getPort(ctx, nc, nfsProg)
		if ctx.Err() != nil {
			t.Fatal("a call hung until the deadline")
		}
	})
}

// A reply may arrive in several fragments; only the last has the top bit.
func TestFragmentedReply(t *testing.T) {
	xids := make(chan uint32, 1)
	c, srv := fakeConn(t, func(fc fakeCall) []byte { xids <- fc.xid; return nil })
	// The fake leaves the call to this goroutine, which answers it in three
	// fragments.
	go func() {
		e := &encoder{}
		e.u32(<-xids)
		e.u32(msgReply)
		e.b = append(e.b, accepted(func(e *encoder) { e.u32(42) })...)
		rec := e.b
		for i, part := range [][]byte{rec[:5], rec[5:13], rec[13:]} {
			h := uint32(len(part))
			if i == 2 {
				h |= 0x80000000
			}
			srv.Write(binary.BigEndian.AppendUint32(nil, h))
			srv.Write(part)
		}
	}()
	d, err := c.call(t.Context(), nfsProg, 1, func(*encoder) {})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.u32(); got != 42 {
		t.Errorf("result %d, want 42", got)
	}
}

func TestReadCountMustMatchData(t *testing.T) {
	nc, _ := fakeConn(t, func(fakeCall) []byte {
		return accepted(func(e *encoder) {
			e.u32(uint32(StatusOK))
			e.bool(false) // file attributes
			e.u32(10)     // count: says 10 bytes
			e.bool(true)
			e.opaque([]byte("four")) // sends 4
		})
	})
	c := &Client{nfs: nc, rsize: 4096, wsize: 4096}
	if _, _, err := c.Read(t.Context(), FH{1}, 0, 4096); err == nil {
		t.Error("a READ that sent less than it said was taken")
	}
}
