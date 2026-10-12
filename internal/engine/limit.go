// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"context"
	"io"
	"sync"

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

// Cap is a cap on the total rate of several shuttles, which can change while
// they run: each file transfer is paced by the limits in force when it
// starts. A nil *Cap means no cap.
type Cap struct {
	mu       sync.Mutex
	up, down int64
	limits   Limits
}

// NewCap returns a Cap of up and down bytes per second; 0 means unlimited.
func NewCap(up, down int64) *Cap {
	c := &Cap{}
	c.Set(up, down)
	return c
}

// Set changes the cap's rates. A direction whose rate is unchanged keeps
// its limiter, and with it what transfers in progress have used.
func (c *Cap) Set(up, down int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if up != c.up {
		c.up, c.limits.Up = up, NewLimiter(up)
	}
	if down != c.down {
		c.down, c.limits.Down = down, NewLimiter(down)
	}
}

// Limits are the limits in force now.
func (c *Cap) Limits() Limits {
	if c == nil {
		return Limits{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limits
}

// limit paces r by this shuttle's limit and the shared cap, for a transfer
// towards the share when up is set. It also counts the transfer towards the
// run's progress, and as copied contents when copied is set.
func (e *executor) limit(r io.Reader, up, copied bool) io.Reader {
	if e.o.Tracker != nil {
		r = &counted{r: r, t: e.o.Tracker, up: up, copied: copied}
	}
	var ls []*Limiter
	for _, l := range []Limits{e.own, e.o.Cap.Limits()} {
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
