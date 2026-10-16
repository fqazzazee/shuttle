// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package bus

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// ErrNotRunning means no daemon serves this user's pipe.
var ErrNotRunning = errors.New("the shuttle daemon is not running (start it with: shuttle daemon)")

// dialTimeout bounds waiting for the pipe when it is busy with another
// client being let in.
const dialTimeout = 5 * time.Second

// Client talks to the daemon over this user's pipe. Its calls take turns.
type Client struct {
	name string

	mu   sync.Mutex
	conn net.Conn // nil after a call broke it: the next call dials again
	dec  *json.Decoder
	enc  *json.Encoder
	id   uint64
}

// Connect connects to the daemon on this user's pipe.
func Connect() (*Client, error) {
	name, err := PipeName()
	if err != nil {
		return nil, err
	}
	return connect(name)
}

func connect(name string) (*Client, error) {
	c := &Client{name: name}
	if err := c.dial(context.Background()); err != nil {
		return nil, err
	}
	return c, nil
}

func dialPipe(ctx context.Context, name string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := winio.DialPipeContext(ctx, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotRunning
	}
	return conn, err
}

func (c *Client) dial(ctx context.Context) error {
	conn, err := dialPipe(ctx, c.name)
	if err != nil {
		return err
	}
	c.conn, c.dec, c.enc = conn, json.NewDecoder(bufio.NewReader(conn)), json.NewEncoder(conn)
	return nil
}

// Close closes the client's connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// do sends req and waits for its reply, or for ctx. A call cut short leaves
// the connection out of step, so it is closed, and the next call dials again.
func (c *Client) do(ctx context.Context, req *request) (*reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if err := c.dial(ctx); err != nil {
			return nil, err
		}
	}
	c.id++
	req.ID = c.id
	conn := c.conn
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.SetDeadline(time.Now()) }) //nolint:errcheck // only to wake the call
	defer stop()
	var r reply
	err := c.enc.Encode(req)
	if err == nil {
		err = c.dec.Decode(&r)
	}
	if err == nil && r.ID != req.ID {
		err = fmt.Errorf("the daemon answered call %d to call %d", r.ID, req.ID)
	}
	if err != nil {
		conn.Close()
		c.conn = nil
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w (%w)", ErrNotRunning, err)
	}
	return &r, fromWire(r.Error)
}

// Shuttles returns every shuttle's status, sorted by name.
func (c *Client) Shuttles(ctx context.Context) ([]daemon.Status, error) {
	r, err := c.do(ctx, &request{Method: methodShuttles})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(r.Shuttles, func(a, b daemon.Status) int { return strings.Compare(a.Name, b.Name) })
	return r.Shuttles, nil
}

// SyncNow triggers a run of the named shuttle.
func (c *Client) SyncNow(ctx context.Context, name string) error {
	_, err := c.do(ctx, &request{Method: methodSyncNow, Name: name})
	return err
}

// SetPaused pauses or resumes the named shuttle, or all when name is "".
func (c *Client) SetPaused(ctx context.Context, name string, paused bool) error {
	_, err := c.do(ctx, &request{Method: methodSetPaused, Name: name, On: paused})
	return err
}

// Resolve settles a held conflict.
func (c *Client) Resolve(ctx context.Context, name, path string, choice engine.Choice) error {
	_, err := c.do(ctx, &request{Method: methodResolve, Name: name, Path: path, Choice: string(choice)})
	return err
}

// SetEnabled turns the named shuttle on or off in the config file. The
// daemon applies it shortly after.
func (c *Client) SetEnabled(ctx context.Context, name string, on bool) error {
	_, err := c.do(ctx, &request{Method: methodSetEnabled, Name: name, On: on})
	return err
}

// AllowDeletions lets the next run pass the deletion guard.
func (c *Client) AllowDeletions(ctx context.Context, name string) error {
	_, err := c.do(ctx, &request{Method: methodAllowDeletions, Name: name})
	return err
}

// Activity returns the newest entries of a shuttle's activity log, or of
// every shuttle's when name is "", newest first: at most limit (0: all).
func (c *Client) Activity(ctx context.Context, name string, limit int) ([]activity.Entry, error) {
	r, err := c.do(ctx, &request{Method: methodActivity, Name: name, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]activity.Entry, 0, len(r.Activity))
	for _, e := range r.Activity {
		e.Entry.Shuttle = e.Shuttle
		out = append(out, e.Entry)
	}
	return out, nil
}

// Reload asks the daemon to re-read its config file now.
func (c *Client) Reload(ctx context.Context) error {
	_, err := c.do(ctx, &request{Method: methodReload})
	return err
}

// ConfigError returns why the daemon's config file did not load, or "".
func (c *Client) ConfigError(ctx context.Context) (string, error) {
	r, err := c.do(ctx, &request{Method: methodConfigError})
	if err != nil {
		return "", err
	}
	return r.ConfigError, nil
}

// State returns the overall state and whether everything is paused.
func (c *Client) State(ctx context.Context) (daemon.State, bool, error) {
	r, err := c.do(ctx, &request{Method: methodState})
	if err != nil {
		return "", false, err
	}
	return r.State, r.Paused, nil
}

// Rates returns every shuttle's transfer rates together, to and from their
// shares, in bytes per second.
func (c *Client) Rates(ctx context.Context) (up, down int64, err error) {
	r, err := c.do(ctx, &request{Method: methodRates})
	if err != nil {
		return 0, 0, err
	}
	return r.Up, r.Down, nil
}

// Changes streams status changes until ctx is done, on a connection of its
// own.
func (c *Client) Changes(ctx context.Context) (<-chan daemon.Status, error) {
	conn, err := dialPipe(ctx, c.name)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bufio.NewReader(conn))
	var ack reply
	if err := json.NewEncoder(conn).Encode(&request{ID: 1, Method: methodWatch}); err != nil {
		conn.Close()
		return nil, err
	}
	if err := dec.Decode(&ack); err != nil {
		conn.Close()
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	out := make(chan daemon.Status, watchBuffer)
	go func() {
		defer close(out)
		defer stop()
		defer conn.Close()
		for {
			var r reply
			if err := dec.Decode(&r); err != nil {
				return
			}
			if r.Changed == nil {
				continue
			}
			select {
			case out <- *r.Changed:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
