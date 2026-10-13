// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// The selected shuttle in full, under the table: its folders from the
// config, the room on each side, when it runs, and DETAILS uncut.
func TestShuttlePanel(t *testing.T) {
	o := testOptions(t)
	doc := `[shares.nas]
type = "smb"
host = "nas.example"
share = "home"
user = "user"

[shares.usb]
type = "path"
path = "/mnt/usb"

[profiles.live]
mode = "sync"
triggers = ["schedule"]
interval = "1h"

[shuttles.docs]
profile = "live"
local = "` + o.Home + `/Documents"
remote = "nas:/docs"
exclude = ["raw"]

[shuttles.photos]
mode = "push"
local = "/srv/photos"
remote = "usb:."
`
	if err := os.WriteFile(o.Config, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFake()
	f.shuttles[0].NextRun = now.Add(59 * time.Minute)
	f.shuttles[0].Copied, f.shuttles[0].Deleted = 3, 1
	f.shuttles[0].RemoteSpace = fsys.Space{Free: 1_200_000_000_000, Total: 4_000_000_000_000}
	m := newModel(context.Background(), f, o)
	m.every, m.now = 0, func() time.Time { return now }
	send(t, m, tea.WindowSizeMsg{Width: 140, Height: 24})
	run(t, m, m.Init())
	if m.cfgErr != nil {
		t.Fatal(m.cfgErr)
	}

	s := screen(m)
	for _, want := range []string{
		"docs  ⇅ sync · profile live",
		"  This computer   ~/Documents\n",
		"  The share       nas:/docs  ·  1.2 TB free of 4.0 TB",
		"  Syncs           all but raw",
		"  Runs            every 1h, next in 59m",
		"  Last run        5m ago: 3 copied, 1 deleted",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the panel lacks %q:\n%s", want, s)
		}
	}
	press(t, m, "down") // music: disabled, and not in this config
	s = screen(m)
	if !strings.Contains(s, "  Runs            never: it is disabled (d turns it on)") || strings.Contains(s, "This computer") {
		t.Errorf("the panel of a disabled shuttle the config lacks:\n%s", s)
	}
	press(t, m, "down")
	s = screen(m)
	for _, want := range []string{
		"photos  ↑ push\n",
		"  The share       usb:/\n",
		"  Runs            only when you ask (s)",
		"  Details         2 conflicts waiting for you",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the panel lacks %q:\n%s", want, s)
		}
	}
}

// With more shuttles than fit, the table scrolls to make room for the
// panel, down to a few rows; the selected shuttle stays in view.
func TestShuttlePanelMakesRoom(t *testing.T) {
	f := newFake()
	for i := range 20 {
		f.shuttles = append(f.shuttles, daemon.Status{Name: fmt.Sprintf("s%02d", i), Mode: config.ModePush,
			State: daemon.StateDisabled})
	}
	m := start(t, f, 120, 24)
	press(t, m, "G")
	s := screen(m)
	if !strings.Contains(s, "s19  ↑ push\n") || !strings.Contains(s, "  Runs") {
		t.Errorf("no room for the panel, or the selection is out of view:\n%s", s)
	}
	if rows := strings.Count(s, "↑ push  disabled"); rows < 3 {
		t.Errorf("the table has %d rows left", rows)
	}
	// Too short for both: the table keeps three rows.
	send(t, m, tea.WindowSizeMsg{Width: 120, Height: 9})
	if s := screen(m); strings.Count(s, "↑ push  disabled") != 3 || !strings.Contains(s, "s19 ") {
		t.Errorf("a short terminal:\n%s", s)
	}
}
