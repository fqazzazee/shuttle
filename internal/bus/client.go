// SPDX-License-Identifier: GPL-2.0-or-later

package bus

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// ErrNotRunning means no daemon owns the bus name.
var ErrNotRunning = errors.New("the shuttle daemon is not running (start it with: systemctl --user start shuttle)")

// Client talks to the daemon over D-Bus.
type Client struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

// Connect connects to the daemon on the session bus.
func Connect() (*Client, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}

// NewClient talks to the daemon over conn.
func NewClient(conn *dbus.Conn) *Client {
	return &Client{conn: conn, obj: conn.Object(Name, Path)}
}

// Close closes the client's connection.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) call(ctx context.Context, method string, args ...any) *dbus.Call {
	return c.obj.CallWithContext(ctx, Interface+"."+method, 0, args...)
}

// Shuttles returns every shuttle's status, sorted by name.
func (c *Client) Shuttles(ctx context.Context) ([]daemon.Status, error) {
	var m map[string]map[string]dbus.Variant
	if err := c.call(ctx, "ListShuttles").Store(&m); err != nil {
		return nil, clientErr(err)
	}
	out := make([]daemon.Status, 0, len(m))
	for name, st := range m {
		out = append(out, decode(name, st))
	}
	slices.SortFunc(out, func(a, b daemon.Status) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// SyncNow triggers a run of the named shuttle.
func (c *Client) SyncNow(ctx context.Context, name string) error {
	return clientErr(c.call(ctx, "SyncNow", name).Err)
}

// SetPaused pauses or resumes the named shuttle, or all when name is "".
func (c *Client) SetPaused(ctx context.Context, name string, paused bool) error {
	return clientErr(c.call(ctx, "SetPaused", name, paused).Err)
}

// Resolve settles a held conflict.
func (c *Client) Resolve(ctx context.Context, name, path string, choice engine.Choice) error {
	return clientErr(c.call(ctx, "ResolveConflict", name, path, string(choice)).Err)
}

// SetEnabled turns the named shuttle on or off in the config file. The
// daemon applies it shortly after.
func (c *Client) SetEnabled(ctx context.Context, name string, on bool) error {
	return clientErr(c.call(ctx, "SetEnabled", name, on).Err)
}

// AllowDeletions lets the next run pass the deletion guard.
func (c *Client) AllowDeletions(ctx context.Context, name string) error {
	return clientErr(c.call(ctx, "AllowDeletions", name).Err)
}

// Reload asks the daemon to re-read its config file now.
func (c *Client) Reload(ctx context.Context) error {
	return clientErr(c.call(ctx, "ReloadConfig").Err)
}

// ConfigError returns why the daemon's config file did not load, or "".
func (c *Client) ConfigError(ctx context.Context) (string, error) {
	var msg string
	if err := c.obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, Interface, "ConfigError").Store(&msg); err != nil {
		return "", clientErr(err)
	}
	return msg, nil
}

// State returns the overall state and whether everything is paused.
func (c *Client) State(ctx context.Context) (daemon.State, bool, error) {
	var state string
	var paused bool
	if err := c.obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, Interface, "State").Store(&state); err != nil {
		return "", false, clientErr(err)
	}
	if err := c.obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, Interface, "Paused").Store(&paused); err != nil {
		return "", false, clientErr(err)
	}
	return daemon.State(state), paused, nil
}

// Changes streams status changes until ctx is done.
func (c *Client) Changes(ctx context.Context) (<-chan daemon.Status, error) {
	if err := c.conn.AddMatchSignalContext(ctx,
		dbus.WithMatchObjectPath(Path), dbus.WithMatchInterface(Interface), dbus.WithMatchMember("ShuttleChanged")); err != nil {
		return nil, err
	}
	signals := make(chan *dbus.Signal, 64)
	c.conn.Signal(signals)
	out := make(chan daemon.Status, 64)
	go func() {
		defer close(out)
		defer c.conn.RemoveSignal(signals)
		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-signals:
				if sig.Name != Interface+".ShuttleChanged" || len(sig.Body) != 2 {
					continue
				}
				name, _ := sig.Body[0].(string)
				st, _ := sig.Body[1].(map[string]dbus.Variant)
				select {
				case out <- decode(name, st):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// clientErr maps D-Bus errors back to the daemon's errors.
func clientErr(err error) error {
	var de dbus.Error
	var dep *dbus.Error
	var name, msg string
	switch {
	case err == nil:
		return nil
	case errors.As(err, &de):
		name, msg = de.Name, fmt.Sprint(de.Body...)
	case errors.As(err, &dep):
		name, msg = dep.Name, fmt.Sprint(dep.Body...)
	default:
		return err
	}
	switch name {
	case "org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.DBus.Error.NameHasNoOwner":
		return ErrNotRunning
	case errUnknownShuttle:
		return fmt.Errorf("%s: %w", msg, daemon.ErrUnknownShuttle)
	case errDisabled:
		return fmt.Errorf("%s: %w", msg, daemon.ErrDisabled)
	case errConfig:
		return fmt.Errorf("%s: %w", msg, daemon.ErrConfig)
	}
	return errors.New(msg)
}
