// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package notifytest is a stand-in desktop notification server: it owns
// org.freedesktop.Notifications on a bus and records what it is asked to
// show, for tests and the systemd exit test.
package notifytest

import (
	"errors"
	"sync"

	"github.com/godbus/dbus/v5"
)

// Note is one notification as the server received it.
type Note struct {
	ID         uint32
	AppName    string
	ReplacesID uint32
	Icon       string
	Summary    string
	Body       string
	Category   string // the "category" hint
	Urgency    byte   // the "urgency" hint
	Actions    []string
}

// Server records notifications.
type Server struct {
	conn   *dbus.Conn
	markup bool
	onNote func(Note)

	mu    sync.Mutex
	notes []Note
	next  uint32
}

// Serve claims org.freedesktop.Notifications on conn. markup says whether
// to advertise the body-markup capability. onNote, if set, is called with
// each note as it arrives.
func Serve(conn *dbus.Conn, markup bool, onNote func(Note)) (*Server, error) {
	s := &Server{conn: conn, markup: markup, onNote: onNote}
	if err := conn.Export(methods{s}, "/org/freedesktop/Notifications", "org.freedesktop.Notifications"); err != nil {
		return nil, err
	}
	reply, err := conn.RequestName("org.freedesktop.Notifications", dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, errors.New("another notification server owns the name")
	}
	return s, nil
}

// Invoke clicks a notification's action, as the desktop does: it emits
// ActionInvoked.
func (s *Server) Invoke(id uint32, action string) error {
	return s.conn.Emit("/org/freedesktop/Notifications", "org.freedesktop.Notifications.ActionInvoked", id, action)
}

// Notes returns the notifications received so far.
func (s *Server) Notes() []Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Note(nil), s.notes...)
}

// methods are the D-Bus methods, apart from Server's own.
type methods struct{ s *Server }

func (m methods) Notify(app string, replaces uint32, icon, summary, body string,
	actions []string, hints map[string]dbus.Variant, _ int32,
) (uint32, *dbus.Error) {
	s := m.s
	s.mu.Lock()
	n := Note{ID: replaces, AppName: app, ReplacesID: replaces, Icon: icon, Summary: summary, Body: body, Actions: actions}
	if n.ID == 0 {
		s.next++
		n.ID = s.next
	}
	if v, ok := hints["category"]; ok {
		_ = v.Store(&n.Category)
	}
	if v, ok := hints["urgency"]; ok {
		_ = v.Store(&n.Urgency)
	}
	s.notes = append(s.notes, n)
	s.mu.Unlock()
	if s.onNote != nil {
		s.onNote(n)
	}
	return n.ID, nil
}

func (m methods) GetCapabilities() ([]string, *dbus.Error) {
	caps := []string{"body"}
	if m.s.markup {
		caps = append(caps, "body-markup")
	}
	return caps, nil
}

func (methods) GetServerInformation() (name, vendor, version, spec string, _ *dbus.Error) {
	return "notifytest", "shuttle", "1", "1.2", nil
}

func (methods) CloseNotification(uint32) *dbus.Error { return nil }
