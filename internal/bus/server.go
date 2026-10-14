// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

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
	errConfig         = Interface + ".Error.Config"
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
		// Why the config file did not load ("" when it did); the daemon then
		// runs the config it last loaded.
		"ConfigError": {Value: d.ConfigError(), Emit: prop.EmitTrue},
		// Every shuttle's transfer rates together, in bytes per second.
		"RateUp":   {Value: uint64(0), Emit: prop.EmitTrue},
		"RateDown": {Value: uint64(0), Emit: prop.EmitTrue},
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
					{Name: "ShuttleRemoved", Args: []introspect.Arg{{Name: "name", Type: "s"}}},
					{Name: "ConfigReloaded"},
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
	stopReloads := d.WatchReloads(s.reloaded)
	defer stopReloads()

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

// changed is the daemon's listener: it updates the properties, then emits
// ShuttleChanged and ConflictRaised for conflicts newly held. Properties
// first: a client that reads them when a signal arrives must not get the
// state from before it.
func (s *server) changed(st *daemon.Status) {
	s.setProp("State", string(s.d.State()))
	s.setProp("Paused", s.d.Paused())
	up, down := s.d.Rates()
	s.setProp("RateUp", u64(up))
	s.setProp("RateDown", u64(down))
	_ = s.conn.Emit(Path, Interface+".ShuttleChanged", st.Name, encode(st))
	for _, c := range s.newlyHeld(st) {
		_ = s.conn.Emit(Path, Interface+".ConflictRaised", st.Name, c.Path, c.Reason)
	}
}

// reloaded is the daemon's reload listener: it updates the properties,
// then emits ShuttleRemoved (see changed).
func (s *server) reloaded(r *daemon.Reloaded) {
	defer func() { _ = s.conn.Emit(Path, Interface+".ConfigReloaded") }() // last: the settings are those on disk now
	s.setProp("ConfigError", r.ConfigError)
	s.setProp("State", string(s.d.State()))
	for _, name := range r.Removed {
		s.mu.Lock()
		delete(s.held, name)
		s.mu.Unlock()
		_ = s.conn.Emit(Path, Interface+".ShuttleRemoved", name)
	}
}

// setProp updates a read-only property and emits PropertiesChanged, if its
// value changed: progress updates come several times a second. prop's
// only setter for read-only properties, SetMust, panics if emitting fails
// (the bus connection closed); that must never take the sync daemon down,
// so the panic stops here. (Properties.Set is the clients' setter: it
// refuses read-only properties.)
func (s *server) setProp(name string, v any) {
	defer func() { _ = recover() }()
	if s.props.GetMust(Interface, name) == v {
		return
	}
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
	case errors.Is(err, daemon.ErrConfig):
		return dbus.NewError(errConfig, []any{err.Error()})
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

// setting is a settings row on the bus: a(ssssasssss).
type setting struct {
	Key, Label, Hint string
	Kind             string   // choice, list, number or text
	Choices          []string // for a choice; on and off for a switch
	Value            string   // the shuttle's own; "" when it sets none
	Inherited, From  string   // what applies when it sets none, and from where
	Off              string   // why the row does not apply; "" when it does
}

// ShuttleSettings are a shuttle's settings as rows, for an editor such as
// the GNOME extension's preferences.
func (s *server) ShuttleSettings(name string) ([]setting, *dbus.Error) {
	rows, err := s.d.ShuttleSettings(name)
	if err != nil {
		return nil, busErr(err)
	}
	out := make([]setting, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		kind := "text"
		switch {
		case r.Choices != nil:
			kind = "choice"
		case r.List:
			kind = "list"
		case r.Number:
			kind = "number"
		}
		out = append(out, setting{Key: r.Key, Label: r.Label, Hint: r.Hint, Kind: kind, Choices: r.Choices,
			Value: r.Value, Inherited: r.Inherited, From: r.From, Off: r.Off})
	}
	return out, nil
}

// SetShuttleSetting sets one of a shuttle's settings in the config file; ""
// removes it, so its profile's or the default applies again.
func (s *server) SetShuttleSetting(name, key, value string) *dbus.Error {
	return busErr(s.d.SetShuttleSetting(name, key, value))
}

// SetEnabled turns a shuttle on or off in the config file, which the
// daemon then reloads.
func (s *server) SetEnabled(name string, on bool) *dbus.Error {
	return busErr(s.d.SetEnabled(name, on))
}

// ReloadConfig re-reads the config file now. A broken one is refused with
// its error, and the running config stays in use.
func (s *server) ReloadConfig() *dbus.Error {
	return busErr(s.d.Reload())
}

// entry is an activity log entry on the bus: (shuttle, time in unix
// seconds, kind, path, text).
type entry struct {
	Shuttle string
	Time    int64
	Kind    string
	Path    string
	Text    string
}

// Activity returns the newest entries of a shuttle's activity log, or of
// every shuttle's when name is "", newest first: at most limit (0: all).
func (s *server) Activity(name string, limit uint32) ([]entry, *dbus.Error) {
	es, err := s.d.Activity(name, int(limit))
	if err != nil {
		return nil, busErr(err)
	}
	out := make([]entry, 0, len(es))
	for _, e := range es {
		out = append(out, entry{e.Shuttle, e.Time.Unix(), string(e.Kind), e.Path, e.Text})
	}
	return out, nil
}

func (s *server) AllowDeletions(name string) *dbus.Error {
	return busErr(s.d.AllowDeletions(name))
}
