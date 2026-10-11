// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

// maxBurst is the most a limited transfer reads in one go: small enough that
// the rate is smooth at a second's scale, large enough not to slow the copy
// loop down.
const maxBurst = 64 << 10

// Limiter caps a transfer rate. A nil *Limiter means no limit. One Limiter
// can be shared by several shuttles to cap their total.
type Limiter struct {
	l     *rate.Limiter
	burst int
}

// NewLimiter returns a Limiter for bytesPerSecond, or nil for 0 (unlimited).
func NewLimiter(bytesPerSecond int64) *Limiter {
	if bytesPerSecond <= 0 {
		return nil
	}
	burst := int(min(bytesPerSecond, maxBurst))
	return &Limiter{l: rate.NewLimiter(rate.Limit(bytesPerSecond), burst), burst: burst}
}

// Limits are the upload (to the share) and download limits of a transfer.
type Limits struct {
	Up, Down *Limiter
}

// limit paces r by this shuttle's limit and the shared one, for a transfer
// towards the share when up is set.
func (e *executor) limit(r io.Reader, up bool) io.Reader {
	var ls []*Limiter
	for _, l := range []Limits{e.own, e.o.Shared} {
		x := l.Down
		if up {
			x = l.Up
		}
		if x != nil {
			ls = append(ls, x)
		}
	}
	if len(ls) == 0 {
		return r
	}
	return &limitedReader{ctx: e.ctx, r: r, limiters: ls}
}

type limitedReader struct {
	ctx      context.Context
	r        io.Reader
	limiters []*Limiter
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	for _, l := range lr.limiters {
		if len(p) > l.burst {
			p = p[:l.burst]
		}
	}
	n, err := lr.r.Read(p)
	for _, l := range lr.limiters {
		// Pay for what was read; this is where the transfer waits.
		if werr := l.l.WaitN(lr.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}
