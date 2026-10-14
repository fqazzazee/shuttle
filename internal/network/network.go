// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package network tells whether the computer's connection is metered, as
// NetworkManager has it on the system bus: a phone's hotspot, a mobile
// broadband modem, or a connection the user marked so.
//
// This package is desktop-specific; the sync core must not import it.
package network

import (
	"context"
	"fmt"
	"slices"

	"github.com/godbus/dbus/v5"
)

// NetworkManager's name, object and interface on the system bus.
const (
	nmName  = "org.freedesktop.NetworkManager"
	nmPath  = dbus.ObjectPath("/org/freedesktop/NetworkManager")
	nmIface = "org.freedesktop.NetworkManager"
)

// NetworkManager's NMMetered, what it knows or guesses of the primary
// connection: 0 unknown, 1 yes, 2 no, 3 guess yes, 4 guess no.
const (
	meteredYes      uint32 = 1
	meteredGuessYes uint32 = 3
)

// isMetered reads NMMetered: yes, or a guess of yes. Unknown is not
// metered: a shuttle that pauses for nothing is worse than one that runs.
func isMetered(v uint32) bool { return v == meteredYes || v == meteredGuessYes }

// Watch calls onChange with whether the connection is metered, at once and
// then each time that changes, until ctx is done. conn is the system bus.
// Without NetworkManager the connection is not metered, until it starts.
func Watch(ctx context.Context, conn *dbus.Conn, onChange func(metered bool)) error {
	matches := []dbus.MatchOption{
		dbus.WithMatchObjectPath(nmPath),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchArg(0, nmIface),
	}
	owner := []dbus.MatchOption{
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, nmName),
	}
	if err := conn.AddMatchSignal(matches...); err != nil {
		return fmt.Errorf("network: watch NetworkManager: %w", err)
	}
	defer conn.RemoveMatchSignal(matches...) //nolint:errcheck // on the way out, nothing to do about it
	if err := conn.AddMatchSignal(owner...); err != nil {
		return fmt.Errorf("network: watch NetworkManager: %w", err)
	}
	defer conn.RemoveMatchSignal(owner...) //nolint:errcheck // on the way out, nothing to do about it
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	metered := read(conn)
	onChange(metered)
	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-signals:
			if !ok {
				return nil
			}
			now, ok := fromSignal(sig, conn)
			if ok && now != metered {
				metered = now
				onChange(metered)
			}
		}
	}
}

// read asks NetworkManager whether the connection is metered: not, when it
// cannot be asked.
func read(conn *dbus.Conn) bool {
	v, err := conn.Object(nmName, nmPath).GetProperty(nmIface + ".Metered")
	if err != nil {
		return false
	}
	u, ok := v.Value().(uint32)
	return ok && isMetered(u)
}

// fromSignal is what a signal says of the connection, asking again where it
// only says something changed: NetworkManager came or went, or invalidated
// Metered. ok is false for a signal about NetworkManager's other properties,
// which change far more often.
func fromSignal(sig *dbus.Signal, conn *dbus.Conn) (metered, ok bool) {
	if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
		return read(conn), true
	}
	if sig.Name != "org.freedesktop.DBus.Properties.PropertiesChanged" || sig.Path != nmPath || len(sig.Body) < 3 {
		return false, false
	}
	if changed, isMap := sig.Body[1].(map[string]dbus.Variant); isMap {
		if v, has := changed["Metered"]; has {
			u, isU := v.Value().(uint32)
			return isU && isMetered(u), true
		}
	}
	if invalidated, isList := sig.Body[2].([]string); isList && slices.Contains(invalidated, "Metered") {
		return read(conn), true
	}
	return false, false
}
