// SPDX-License-Identifier: GPL-2.0-or-later

// Package bus is shuttle's D-Bus interface: the daemon serves it, and the
// CLI, the TUI and the GNOME extension use it. See SPEC.md, "D-Bus API".
//
// This package is desktop-specific; the sync core must not import it.
package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// The daemon's well-known name, object and interface.
const (
	Name      = "io.github.fqazzazee.Shuttle"
	Path      = dbus.ObjectPath("/io/github/fqazzazee/Shuttle")
	Interface = "io.github.fqazzazee.Shuttle1"
)

// Errors on the bus, by D-Bus error name. The client maps them back.
const (
	errUnknownShuttle = Interface + ".Error.UnknownShuttle"
	errDisabled       = Interface + ".Error.Disabled"
	errInvalid        = Interface + ".Error.InvalidArgs"
	errFailed         = Interface + ".Error.Failed"
)

// ErrNameTaken means another process owns the daemon's bus name.
var ErrNameTaken = errors.New("another shuttle daemon owns " + Name + " on the session bus")

// server holds the exported object. Its exported methods are the bus API.
type server struct {
	conn  *dbus.Conn
	d     *daemon.Daemon
	props *prop.Properties

	mu   sync.Mutex
	held map[string]map[string]bool // shuttle -> held conflict paths already announced
}

// Serve exports the daemon on conn and claims the well-known name, then
// keeps the properties and signals current until ctx is done. Call it once
// the daemon is Ready.
func Serve(ctx context.Context, conn *dbus.Conn, d *daemon.Daemon) error {
	s := &server{conn: conn, d: d, held: map[string]map[string]bool{}}
	if err := conn.Export(s, Path, Interface); err != nil {
		return err
	}
	props, err := prop.Export(conn, Path, prop.Map{Interface: {
		"Paused": {Value: d.Paused(), Emit: prop.EmitTrue},
		"State":  {Value: string(d.State()), Emit: prop.EmitTrue},
	}})
	if err != nil {
		return err
	}
	s.props = props
	node := &introspect.Node{
		Name: string(Path),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       Interface,
				Methods:    introspect.Methods(s),
				Properties: props.Introspection(Interface),
				Signals: []introspect.Signal{
					{Name: "ShuttleChanged", Args: []introspect.Arg{{Name: "name", Type: "s"}, {Name: "status", Type: "a{sv}"}}},
					{Name: "ConflictRaised", Args: []introspect.Arg{{Name: "name", Type: "s"}, {Name: "path", Type: "s"}, {Name: "reason", Type: "s"}}},
				},
			},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), Path, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	all := d.Statuses()
	for i := range all {
		s.newlyHeld(&all[i]) // what is already held at start is not news
	}
	// Stop emitting before Serve returns: the caller may close conn next.
	stop := d.Watch(s.changed)
	defer stop()

	reply, err := conn.RequestName(Name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return ErrNameTaken
	}
	<-ctx.Done()
	_, err = conn.ReleaseName(Name)
	return err
}

// changed is the daemon's listener: it emits ShuttleChanged, ConflictRaised
// for conflicts newly held, and updates the properties.
func (s *server) changed(st *daemon.Status) {
	_ = s.conn.Emit(Path, Interface+".ShuttleChanged", st.Name, encode(st))
	for _, c := range s.newlyHeld(st) {
		_ = s.conn.Emit(Path, Interface+".ConflictRaised", st.Name, c.Path, c.Reason)
	}
	s.setProp("State", string(s.d.State()))
	s.setProp("Paused", s.d.Paused())
}

// setProp updates a read-only property and emits PropertiesChanged. prop's
// only setter for read-only properties, SetMust, panics if emitting fails
// (the bus connection closed); that must never take the sync daemon down,
// so the panic stops here. (Properties.Set is the clients' setter: it
// refuses read-only properties.)
func (s *server) setProp(name string, v any) {
	defer func() { _ = recover() }()
	s.props.SetMust(Interface, name, v)
}

// newlyHeld returns the held conflicts of st not announced before, and
// remembers st's held set.
func (s *server) newlyHeld(st *daemon.Status) []engine.Issue {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.held[st.Name]
	now := map[string]bool{}
	var fresh []engine.Issue
	for _, c := range st.Conflicts {
		if c.Outcome != "held" {
			continue
		}
		now[c.Path] = true
		if !before[c.Path] {
			fresh = append(fresh, c)
		}
	}
	// A status without run results (e.g. a trigger) does not clear the set.
	if st.LastRun.IsZero() && len(now) == 0 {
		return nil
	}
	s.held[st.Name] = now
	return fresh
}

// busErr maps daemon errors to named D-Bus errors.
func busErr(err error) *dbus.Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, daemon.ErrUnknownShuttle):
		return dbus.NewError(errUnknownShuttle, []any{err.Error()})
	case errors.Is(err, daemon.ErrDisabled):
		return dbus.NewError(errDisabled, []any{err.Error()})
	}
	return dbus.NewError(errFailed, []any{err.Error()})
}

// The bus API. Each method is exported over D-Bus with the same name.

func (s *server) ListShuttles() (map[string]map[string]dbus.Variant, *dbus.Error) {
	out := map[string]map[string]dbus.Variant{}
	all := s.d.Statuses()
	for i := range all {
		out[all[i].Name] = encode(&all[i])
	}
	return out, nil
}

func (s *server) SyncNow(name string) *dbus.Error {
	return busErr(s.d.SyncNow(name))
}

func (s *server) SetPaused(name string, paused bool) *dbus.Error {
	return busErr(s.d.SetPaused(name, paused))
}

func (s *server) ResolveConflict(name, path, choice string) *dbus.Error {
	switch c := engine.Choice(choice); c {
	case engine.KeepLocal, engine.KeepRemote, engine.KeepBoth:
		// Resolve may wait for a run in progress; it is bounded by the
		// caller's own D-Bus timeout on their side, not here.
		return busErr(s.d.Resolve(context.Background(), name, path, c))
	}
	return dbus.NewError(errInvalid, []any{fmt.Sprintf("choice %q must be keep-local, keep-remote or keep-both", choice)})
}

func (s *server) AllowDeletions(name string) *dbus.Error {
	return busErr(s.d.AllowDeletions(name))
}
