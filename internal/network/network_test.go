// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package network

import (
	"context"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/fqazzazee/shuttle/internal/dbustest"
)

// fakeNM is NetworkManager's Metered property on a private bus.
type fakeNM struct {
	conn  *dbus.Conn
	props *prop.Properties
}

func startNM(t *testing.T, conn *dbus.Conn, metered uint32) *fakeNM {
	t.Helper()
	props, err := prop.Export(conn, nmPath, prop.Map{nmIface: {
		"Metered": {Value: metered, Emit: prop.EmitTrue},
		"State":   {Value: uint32(70), Emit: prop.EmitTrue}, // NM_STATE_CONNECTED_GLOBAL
	}})
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(nmName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own %s: %v %v", nmName, reply, err)
	}
	return &fakeNM{conn, props}
}

func (f *fakeNM) set(t *testing.T, name string, v uint32) {
	t.Helper()
	f.props.SetMust(nmIface, name, v) // as NetworkManager does: emits PropertiesChanged
}

// watch runs Watch on conn and returns what it reports, as it does.
func watch(t *testing.T, conn *dbus.Conn) <-chan bool {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan bool, 16)
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, conn, func(m bool) { got <- m }) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Watch: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Watch did not return once its context was done")
		}
	})
	return got
}

func next(t *testing.T, got <-chan bool, want bool, why string) {
	t.Helper()
	select {
	case m := <-got:
		if m != want {
			t.Fatalf("%s: metered %v, want %v", why, m, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no report", why)
	}
}

func quiet(t *testing.T, got <-chan bool, why string) {
	t.Helper()
	select {
	case m := <-got:
		t.Fatalf("%s: reported %v, want nothing", why, m)
	case <-time.After(300 * time.Millisecond):
	}
}

// The connection is metered while NetworkManager says yes or guesses so,
// and Watch reports each change once, but no other property's.
func TestWatchFollowsMetered(t *testing.T) {
	bus := dbustest.Bus(t)
	nm := startNM(t, bus(), 2) // no
	got := watch(t, bus())
	next(t, got, false, "at start, not metered")
	nm.set(t, "Metered", 1)
	next(t, got, true, "yes")
	nm.set(t, "Metered", 3)
	quiet(t, got, "guess yes after yes: still metered")
	nm.set(t, "State", 60)
	quiet(t, got, "another property")
	nm.set(t, "Metered", 4)
	next(t, got, false, "guess no")
	nm.set(t, "Metered", 0)
	quiet(t, got, "unknown: not metered")
}

// Without NetworkManager the connection is not metered; once it starts, what
// it says counts.
func TestWatchWithoutNetworkManager(t *testing.T) {
	bus := dbustest.Bus(t)
	got := watch(t, bus())
	next(t, got, false, "no NetworkManager")
	nmConn := bus()
	startNM(t, nmConn, 1)
	next(t, got, true, "NetworkManager started, metered")
	nmConn.Close()
	next(t, got, false, "NetworkManager gone")
}

func TestIsMetered(t *testing.T) {
	for v, want := range map[uint32]bool{0: false, 1: true, 2: false, 3: true, 4: false} {
		if isMetered(v) != want {
			t.Errorf("isMetered(%d) = %v", v, !want)
		}
	}
}
