// SPDX-License-Identifier: GPL-2.0-or-later

// Package notify shows the daemon's events as desktop notifications,
// through org.freedesktop.Notifications on the session bus: GNOME Shell's,
// or whichever notification server the desktop runs.
//
// This package is desktop-specific; the sync core must not import it.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
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
	body := m.body
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
	if m.open != "" {
		actions = []string{"open", "Open shuttle"}
	}
	var id uint32
	err := s.obj.CallWithContext(ctx, busName+".Notify", 0,
		appName, s.ids[m.key], m.icon, m.summary, body, actions, hints, int32(-1)).Store(&id)
	if err != nil {
		return err
	}
	if m.key != "" {
		s.ids[m.key] = id
	}
	if m.open != "" {
		s.opens[id] = m.open
	}
	return nil
}

// note is what one notification says.
type note struct {
	// key: a note replaces the last one with the same key, so a shuttle's
	// latest error or a share's latest state is shown once, not stacked.
	// Conflicts have none: each batch is news of its own.
	key            string
	icon, category string
	summary, body  string // body is plain text; call escapes it
	open           string // the TUI tab its "Open shuttle" button opens; "" for none
}

// message words an event.
func message(e *daemon.Event) note {
	switch e.Kind {
	case daemon.EventError:
		lines := []string{e.Error}
		lines = append(lines, issueLines(e.Errors, func(i engine.Issue) string { return i.Reason })...)
		return note{key: "error/" + e.Shuttle, icon: "dialog-error-symbolic", category: "transfer.error",
			summary: e.Shuttle + ": sync failed", body: strings.Join(lines, "\n")}
	case daemon.EventConflicts:
		open := ""
		if slices.ContainsFunc(e.Conflicts, func(i engine.Issue) bool { return i.Outcome == "held" }) {
			open = "conflicts" // it waits for the user
		}
		return note{icon: "dialog-warning-symbolic", category: "transfer", open: open,
			summary: fmt.Sprintf("%s: %s", e.Shuttle, plural(len(e.Conflicts), "conflict")),
			body: strings.Join(issueLines(e.Conflicts, func(i engine.Issue) string {
				if i.Outcome == "held" {
					return "waiting for you"
				}
				return i.Outcome
			}), "\n")}
	case daemon.EventDeletionsHeld:
		return note{key: "deletions/" + e.Shuttle, icon: "dialog-warning-symbolic", category: "transfer", open: "conflicts",
			summary: fmt.Sprintf("%s: %s held back", e.Shuttle, plural(e.HeldDeletions, "deletion")),
			body: "More files would be deleted at once than the deletion guard allows, so none were. " +
				"Check that both folders are there and mounted before allowing them."}
	case daemon.EventCompleted:
		return note{key: "completed/" + e.Shuttle, icon: "emblem-synchronizing-symbolic", category: "transfer.complete",
			summary: e.Shuttle + ": synced", body: done(e.Copied, e.Deleted)}
	case daemon.EventShareLost:
		return note{key: "share/" + e.Share, icon: "network-offline-symbolic", category: "network.disconnected",
			summary: "Share " + e.Share + " lost", body: "Waiting for it: " + names(e.Shuttles) + "."}
	case daemon.EventConfigError:
		return note{key: "config", icon: "dialog-error-symbolic", category: "transfer.error",
			summary: "shuttle: config not loaded", body: e.Error + "\nThe previous settings stay in use until it is fixed."}
	case daemon.EventShareConnected:
		return note{key: "share/" + e.Share, icon: "network-server-symbolic", category: "network.connected",
			summary: "Share " + e.Share + " connected", body: "Can sync again: " + names(e.Shuttles) + "."}
	}
	return note{icon: "dialog-information-symbolic", summary: string(e.Kind), body: e.Shuttle}
}

// maxLines is how many paths a note lists before "and N more".
const maxLines = 3

func issueLines(issues []engine.Issue, what func(engine.Issue) string) []string {
	var lines []string
	for i, is := range issues {
		if i == maxLines {
			lines = append(lines, fmt.Sprintf("and %d more", len(issues)-maxLines))
			break
		}
		lines = append(lines, clip(is.Path)+": "+what(is))
	}
	return lines
}

// clip shortens a path from the share to something a notification can show.
func clip(s string) string {
	const most = 80
	if r := []rune(s); len(r) > most {
		return "…" + string(r[len(r)-most+1:])
	}
	return s
}

func done(copied, deleted int) string {
	switch {
	case copied > 0 && deleted > 0:
		return fmt.Sprintf("Copied %s and deleted %d.", plural(copied, "file"), deleted)
	case copied > 0:
		return "Copied " + plural(copied, "file") + "."
	case deleted > 0:
		return "Deleted " + plural(deleted, "file") + "."
	}
	return "Nothing to do: already in step."
}

func names(n []string) string {
	switch {
	case len(n) == 0:
		return "no shuttles"
	case len(n) > 4:
		return strings.Join(n[:3], ", ") + fmt.Sprintf(" and %d more", len(n)-3)
	case len(n) == 1:
		return n[0]
	}
	return strings.Join(n[:len(n)-1], ", ") + " and " + n[len(n)-1]
}

func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}
