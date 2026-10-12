// SPDX-License-Identifier: GPL-2.0-or-later

package nfs3

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// XDR (RFC 4506): big-endian 32-bit words; variable-length data carries its
// length first and is padded to a whole word.

// errShort is a reply that ends before its fields do.
var errShort = errors.New("reply too short")

type encoder struct{ b []byte }

func (e *encoder) u32(v uint32) { e.b = binary.BigEndian.AppendUint32(e.b, v) }
func (e *encoder) u64(v uint64) { e.b = binary.BigEndian.AppendUint64(e.b, v) }

func (e *encoder) bool(v bool) {
	if v {
		e.u32(1)
	} else {
		e.u32(0)
	}
}

// fixed writes p as fixed-length opaque data: no length, padded.
func (e *encoder) fixed(p []byte) {
	e.b = append(e.b, p...)
	e.b = append(e.b, make([]byte, pad(len(p)))...)
}

func (e *encoder) opaque(p []byte) {
	e.u32(size(len(p)))
	e.fixed(p)
}

func (e *encoder) string(s string) { e.opaque([]byte(s)) }

func pad(n int) int { return (4 - n%4) % 4 }

// size is a length as XDR carries it. The lengths shuttle sends are far
// below 4 GiB.
func size(n int) uint32 {
	return uint32(min(max(n, 0), math.MaxUint32)) //nolint:gosec // clamped on this line
}

// decoder reads a reply. The first error sticks: every later read returns a
// zero value, so a parser reads all its fields and checks err once. Nothing
// it returns is longer than the reply itself, whatever lengths the reply
// claims.
type decoder struct {
	b   []byte
	err error
}

func (d *decoder) fail(format string, a ...any) {
	if d.err == nil {
		d.err = fmt.Errorf(format, a...)
	}
}

func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if n < 0 || n > len(d.b) {
		d.err = errShort
		return nil
	}
	p := d.b[:n:n]
	d.b = d.b[n:]
	return p
}

func (d *decoder) u32() uint32 {
	if p := d.take(4); p != nil {
		return binary.BigEndian.Uint32(p)
	}
	return 0
}

func (d *decoder) u64() uint64 {
	if p := d.take(8); p != nil {
		return binary.BigEndian.Uint64(p)
	}
	return 0
}

func (d *decoder) bool() bool {
	switch v := d.u32(); v {
	case 0:
		return false
	case 1:
		return true
	default:
		d.fail("bad boolean %d", v)
		return false
	}
}

// fixed reads n bytes of fixed-length opaque data.
func (d *decoder) fixed(n int) []byte {
	p := d.take(n)
	d.take(pad(n))
	return p
}

// opaque reads variable-length data of at most limit bytes. The result
// shares the reply's memory.
func (d *decoder) opaque(limit int) []byte {
	n := d.u32()
	if d.err != nil {
		return nil
	}
	if n > size(limit) {
		d.fail("field of %d bytes, more than the %d allowed", n, limit)
		return nil
	}
	return d.fixed(int(n))
}

func (d *decoder) string(limit int) string { return string(d.opaque(limit)) }
