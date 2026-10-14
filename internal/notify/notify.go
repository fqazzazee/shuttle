// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package notify shows the daemon's events as desktop notifications,
// through org.freedesktop.Notifications on the session bus: GNOME Shell's,
// or whichever notification server the desktop runs.
//
// This package is desktop-specific; the sync core must not import it.
package notify

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/notice"
)

// desktopEntry is shuttle's desktop file, without .desktop: it gives the
// notifications shuttle's name and icon.
const desktopEntry = "io.github.fqazzazee.Shuttle"

const (
	busName = "org.freedesktop.Notifications"
	objPath = dbus.ObjectPath("/org/freedesktop/Notifications")
	appName = "shuttle"
)

// callTimeout bounds one call to the notification server.
const callTimeout = 5 * time.Second

// queueSize is how many events wait for the server before more are dropped.
const queueSize = 64

// Notifier shows events. Send queues them; Run shows them.
type Notifier struct {
	log   *slog.Logger
	queue chan daemon.Event
	// Open, if set, is called when the user clicks a notification's "Open
	// shuttle" button, with the TUI tab to open.
	Open func(tab string)
}

// New returns a Notifier that logs to log, which may be nil.
func New(log *slog.Logger) *Notifier {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Notifier{log: log, queue: make(chan daemon.Event, queueSize)}
}

// Send queues e to be shown. It never blocks, since the daemon calls it from
// its runners: when the queue is full, because the server stopped
// answering, the event is dropped.
func (n *Notifier) Send(e *daemon.Event) {
	select {
	case n.queue <- *e:
	default:
		n.log.Warn("notification dropped: the notification server is not keeping up", "kind", e.Kind)
	}
}

// Run shows queued events through conn's notification server until ctx is
// done. A missing or failing server is logged, once, and never stops the
// daemon: events meanwhile are lost, the sync goes on.
func (n *Notifier) Run(ctx context.Context, conn *dbus.Conn) {
	s := &sender{log: n.log, obj: conn.Object(busName, objPath), ids: map[string]uint32{}, opens: map[uint32]string{}}
	clicks := make(chan *dbus.Signal, 8)
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchObjectPath(objPath), dbus.WithMatchInterface(busName),
		dbus.WithMatchMember("ActionInvoked")); err != nil {
		n.log.Warn("not listening for notification buttons", "err", err)
	}
	conn.Signal(clicks)
	defer conn.RemoveSignal(clicks)
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-n.queue:
			s.show(ctx, &e)
		case sig, ok := <-clicks:
			if !ok || sig == nil {
				clicks = nil // the connection closed
				continue
			}
			var id uint32
			var action string
			if sig.Name != busName+".ActionInvoked" || dbus.Store(sig.Body, &id, &action) != nil || action != "open" {
				continue
			}
			if tab, ok := s.opens[id]; ok && n.Open != nil {
				go n.Open(tab)
			}
		}
	}
}

// sender talks to the notification server; only Run's goroutine uses it.
type sender struct {
	log     *slog.Logger
	obj     dbus.BusObject
	ids     map[string]uint32 // note key -> the server's id, to replace it
	opens   map[uint32]string // the server's id -> the TUI tab its button opens
	markup  *bool             // whether the server reads markup; nil until asked
	failing bool
}

func (s *sender) show(ctx context.Context, e *daemon.Event) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	err := s.call(ctx, e)
	switch {
	case err != nil && !s.failing:
		s.log.Warn("cannot show desktop notifications; syncing goes on without them", "err", err)
		s.failing, s.markup = true, nil // the next server may differ
	case err == nil && s.failing:
		s.log.Info("desktop notifications work again")
		s.failing = false
	}
}

func (s *sender) call(ctx context.Context, e *daemon.Event) error {
	if s.markup == nil {
		var caps []string
		if err := s.obj.CallWithContext(ctx, busName+".GetCapabilities", 0).Store(&caps); err != nil {
			return err
		}
		has := false
		for _, c := range caps {
			has = has || c == "body-markup"
		}
		s.markup = &has
	}
	m := message(e)
	body := m.Body
	if *s.markup {
		// File names come from the share: never let them read as markup.
		body = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(body)
	}
	hints := map[string]dbus.Variant{
		"category":      dbus.MakeVariant(m.category),
		"urgency":       dbus.MakeVariant(byte(1)), // normal
		"desktop-entry": dbus.MakeVariant(desktopEntry),
	}
	actions := []string{}
	if m.Open != "" {
		actions = []string{"open", "Open shuttle"}
	}
	var id uint32
	err := s.obj.CallWithContext(ctx, busName+".Notify", 0,
		appName, s.ids[m.Key], m.icon, m.Summary, body, actions, hints, int32(-1)).Store(&id)
	if err != nil {
		return err
	}
	if m.Key != "" {
		s.ids[m.Key] = id
	}
	if m.Open != "" {
		s.opens[id] = m.Open
	}
	return nil
}

// note is a notice with the desktop's own hints.
type note struct {
	notice.Note
	icon, category string
}

// message words an event, with the icon and category the desktop's
// notification server shows it by.
func message(e *daemon.Event) note {
	n := note{Note: notice.Of(e), icon: "dialog-information-symbolic"}
	switch e.Kind {
	case daemon.EventError, daemon.EventConfigError:
		n.icon, n.category = "dialog-error-symbolic", "transfer.error"
	case daemon.EventConflicts, daemon.EventDeletionsHeld:
		n.icon, n.category = "dialog-warning-symbolic", "transfer"
	case daemon.EventCompleted:
		n.icon, n.category = "io.github.fqazzazee.Shuttle", "transfer.complete"
	case daemon.EventShareLost:
		n.icon, n.category = "network-offline-symbolic", "network.disconnected"
	case daemon.EventShareConnected:
		n.icon, n.category = "network-server-symbolic", "network.connected"
	}
	return n
}
