// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const profilesConfig = `[shares.nas]
type = "path"
path = "/mnt/nas"

# Things that sync all the time.
[profiles.live]
mode = "sync"
triggers = ["realtime"]   # as soon as a file changes
versioning = { kind = "trash", keep = "30d" }

[profiles.spare]
mode = "push"

[shuttles.docs]
profile = "live"
local = "/home/u/Documents"
remote = "nas:/docs"
`

func profilesTab(t *testing.T, doc string, height int) *model {
	t.Helper()
	o := testOptions(t)
	if err := os.WriteFile(o.Config, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), newFake(), o)
	m.every, m.now = 0, func() time.Time { return now }
	send(t, m, tea.WindowSizeMsg{Width: 150, Height: height})
	run(t, m, m.Init())
	press(t, m, "4")
	return m
}

func ctrl(t *testing.T, m *model, r rune) {
	t.Helper()
	send(t, m, tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl})
}

// toRow moves the form's focus to the row of key.
func toRow(t *testing.T, m *model, key string) {
	t.Helper()
	for range len(m.settings.rows) {
		if m.settings.rows[m.settings.focus] == key {
			return
		}
		press(t, m, "tab")
	}
	t.Fatalf("no row %s", key)
}

func clearRow(t *testing.T, m *model) {
	t.Helper()
	for range 40 {
		send(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
}

func TestProfilesList(t *testing.T) {
	m := profilesTab(t, profilesConfig, 20)
	s := screen(m)
	for _, want := range []string{
		"NAME   MODE   TRIGGERS                          USED BY",
		"live   sync   realtime                          docs",
		"spare  push   manual only                       -",
		"n new  e edit  c copy  x delete",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}

func TestNewProfile(t *testing.T) {
	m := profilesTab(t, profilesConfig, 30)
	press(t, m, "n")
	s := screen(m)
	if !strings.Contains(s, "New profile") || !strings.Contains(s, "not set: 60s") || !strings.Contains(s, "not set: keep-both") {
		t.Fatalf("form:\n%s", s)
	}
	typeIn(t, m, "nightly")
	toRow(t, m, "mode")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyRight}) // push
	toRow(t, m, "triggers")
	typeIn(t, m, "schedule")
	toRow(t, m, "at")
	typeIn(t, m, "02:00, 14:30")
	toRow(t, m, "notify.completed")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyLeft}) // off, from not set
	ctrl(t, m, 's')
	if m.settings != nil {
		t.Fatalf("form still open: %s", m.settings.result)
	}
	want := strings.Replace(profilesConfig, "[profiles.spare]\nmode = \"push\"\n",
		"[profiles.spare]\nmode = \"push\"\n\n[profiles.nightly]\nmode = \"push\"\ntriggers = [\"schedule\"]\nat = [\"02:00\", \"14:30\"]\nnotify = { completed = false }\n", 1)
	if got := configFile(t, m); got != want {
		t.Errorf("config:\n%s\nwant:\n%s", got, want)
	}
	if s := screen(m); !strings.Contains(s, "saved the profile nightly") || m.profileSel != "nightly" {
		t.Errorf("after saving, selected %q:\n%s", m.profileSel, s)
	}
}

func TestEditProfile(t *testing.T) {
	m := profilesTab(t, profilesConfig, 30)
	press(t, m, "e")
	if s := screen(m); !strings.Contains(s, "The profile live") || strings.Contains(s, "  Name") {
		t.Fatalf("form:\n%s", s)
	}
	toRow(t, m, "triggers")
	clearRow(t, m) // manual only
	toRow(t, m, "conflicts")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyLeft}) // ask, from not set
	toRow(t, m, "versioning.kind")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyRight}) // simple
	toRow(t, m, "versioning.keep")
	clearRow(t, m)
	toRow(t, m, "versioning.count")
	typeIn(t, m, "5")
	ctrl(t, m, 's')
	if m.settings != nil {
		t.Fatalf("form still open: %s", m.settings.result)
	}
	// Changed in place; conflicts, new, goes at the end of the table.
	want := strings.Replace(profilesConfig, `triggers = ["realtime"]   # as soon as a file changes
versioning = { kind = "trash", keep = "30d" }`, `versioning = { kind = "simple", count = 5 }
conflicts = "ask"`, 1)
	if got := configFile(t, m); got != want {
		t.Errorf("config:\n%s\nwant:\n%s", got, want)
	}
}

func TestCopyProfile(t *testing.T) {
	m := profilesTab(t, profilesConfig, 30)
	press(t, m, "c")
	if s := screen(m); !strings.Contains(s, "New profile") || !strings.Contains(s, "live-copy") {
		t.Fatalf("form:\n%s", s)
	}
	ctrl(t, m, 's')
	got := configFile(t, m)
	if !strings.Contains(got, "[profiles.live-copy]\nmode = \"sync\"\ntriggers = [\"realtime\"]\nversioning = { kind = \"trash\", keep = \"30d\" }\n") {
		t.Errorf("config:\n%s", got)
	}
}

func TestProfileFormRefuses(t *testing.T) {
	m := profilesTab(t, profilesConfig, 30)
	before := configFile(t, m)
	check := func(what, want string) {
		t.Helper()
		ctrl(t, m, 's')
		if m.settings == nil || !strings.Contains(screen(m), want) {
			t.Errorf("%s: want %q:\n%s", what, want, screen(m))
		}
		if configFile(t, m) != before {
			t.Errorf("%s: the config was written", what)
		}
	}
	press(t, m, "n")
	check("no name", "the profile needs a name")
	typeIn(t, m, "spare")
	toRow(t, m, "mode")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	check("a taken name", "there is a profile named spare already")
	toRow(t, m, "name")
	clearRow(t, m)
	typeIn(t, m, "x")
	toRow(t, m, "mode")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace}) // not set
	check("nothing set", "set at least one setting")
	toRow(t, m, "interval")
	typeIn(t, m, "soon")
	check("a bad duration", `invalid duration "soon": use e.g. 90s`)
	clearRow(t, m)
	toRow(t, m, "deletion_guard")
	typeIn(t, m, "half")
	check("a bad number", `guard must be a number, not "half"`)
	press(t, m, "esc")
	if m.settings != nil {
		t.Error("esc did not close the form")
	}
}

func TestDeleteProfile(t *testing.T) {
	m := profilesTab(t, profilesConfig, 20)
	press(t, m, "x")
	if s := screen(m); !strings.Contains(s, "live is used by docs") || m.question != "" {
		t.Errorf("deleting a profile in use:\n%s", s)
	}
	press(t, m, "down", "x")
	if !strings.Contains(screen(m), "Delete the profile spare from the config file? (y/n)") {
		t.Fatalf("no question:\n%s", screen(m))
	}
	press(t, m, "y")
	if got := configFile(t, m); strings.Contains(got, "spare") || !strings.Contains(got, "[profiles.live]") {
		t.Errorf("after deleting spare:\n%s", got)
	}
}

// In a short terminal, the form scrolls to keep the row being edited in view.
func TestProfileFormScrolls(t *testing.T) {
	m := profilesTab(t, profilesConfig, 12)
	press(t, m, "e")
	toRow(t, m, "notify.share")
	s := screen(m)
	if !strings.Contains(s, "› Tell share") {
		t.Errorf("the focused row is out of view:\n%s", s)
	}
	for _, l := range strings.Split(s, "\n") {
		if ansi.StringWidth(l) > 150 {
			t.Errorf("line too wide: %q", l)
		}
	}
	if !strings.HasPrefix(strings.Split(s, "\n")[11], "tab next") {
		t.Errorf("the keys are not at the bottom:\n%s", s)
	}
}

func TestRawText(t *testing.T) {
	for v, want := range map[any]string{nil: "", true: "on", false: "off", int64(5): "5", "30d": "30d"} {
		if got := rawText(v); got != want {
			t.Errorf("rawText(%v) = %q, want %q", v, got, want)
		}
	}
	if got := rawText([]any{"a", "b"}); got != "a, b" {
		t.Errorf("a list: %q", got)
	}
}
