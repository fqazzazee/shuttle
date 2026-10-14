// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/config"
)

// A setting that depends on another applies only while that one is set to
// match: as the form stands, else as the profile has it, else the default.
func TestSettingsThatDoNotApply(t *testing.T) {
	off := func(f *settingsForm, key string) string { t.Helper(); return f.off(key) }

	// A new profile: the defaults, and no mode yet.
	f := newSettingsForm("profile", "", nil, nil, "")
	for key, want := range map[string]string{
		"interval":            "only with the schedule trigger",
		"at":                  "only with the schedule trigger",
		"poll_interval":       "only with the realtime trigger",
		"conflict_hold":       "only when conflicts are ask",
		"versioning.count":    "only with simple versions",
		"versioning.keep":     "", // trash, the default
		"versioning.size_cap": "",
		"deletions":           "", // no mode yet: not guessed
		"deletion_guard":      "",
		"mode":                "",
	} {
		if got := off(f, key); got != want {
			t.Errorf("new profile, %s: %q, want %q", key, got, want)
		}
	}
	f.inputs["triggers"].SetValue("realtime, schedule")
	f.values["conflicts"] = "ask"
	f.values["versioning.kind"] = "none"
	f.values["mode"] = "push"
	for key, want := range map[string]string{
		"interval": "", "at": "", "poll_interval": "", "conflict_hold": "", "deletions": "",
		"versioning.keep":     "only with trash or staggered versions",
		"versioning.size_cap": "only while versions are kept",
		"deletion_guard":      "only when something is deleted: sync, or deletions mirror",
	} {
		if got := off(f, key); got != want {
			t.Errorf("after changes, %s: %q, want %q", key, got, want)
		}
	}
	f.values["deletions"] = "mirror"
	if got := off(f, "deletion_guard"); got != "" {
		t.Errorf("mirror push, deletion_guard: %q", got)
	}

	// A shuttle whose profile syncs in real time: what the profile sets
	// counts, until the shuttle sets otherwise.
	base := map[string]any{"mode": "sync", "triggers": []any{"realtime"}}
	s := newSettingsForm("shuttle", "docs", nil, base, "live")
	for key, want := range map[string]string{
		"poll_interval":  "",
		"interval":       "only with the schedule trigger",
		"deletions":      "only for push and pull: sync always deletes on both sides",
		"deletion_guard": "",
	} {
		if got := off(s, key); got != want {
			t.Errorf("shuttle on live, %s: %q, want %q", key, got, want)
		}
	}
	s.values["mode"] = "pull"
	if got := off(s, "deletions"); got != "" {
		t.Errorf("shuttle's own pull, deletions: %q", got)
	}
}

// The cursor passes greyed rows by, and comes to them once they apply.
func TestGreyedRowsAreSkipped(t *testing.T) {
	m := profilesTab(t, profilesConfig, 40)
	press(t, m, "n")
	typeIn(t, m, "nightly")
	toRow(t, m, "triggers")
	if s := screen(m); !strings.Contains(s, "Every          only with the schedule trigger") {
		t.Errorf("Every is not greyed out with its reason:\n%s", s)
	}
	press(t, m, "tab")
	if got := m.settings.rows[m.settings.focus]; got != "conflicts" {
		t.Errorf("tab from triggers went to %s, want conflicts (every, at and poll do not apply)", got)
	}
	press(t, m, "shift+tab")
	typeIn(t, m, "schedule")
	press(t, m, "tab")
	if got := m.settings.rows[m.settings.focus]; got != "interval" {
		t.Errorf("with the schedule trigger, tab went to %s, want interval", got)
	}
}

// A value in a row that no longer applies is shown, marked, and not saved.
func TestGreyedValuesAreNotSaved(t *testing.T) {
	m := profilesTab(t, profilesConfig, 40)
	press(t, m, "e") // live: sync, realtime, trash kept 30d
	toRow(t, m, "versioning.kind")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyLeft}) // none
	if s := screen(m); !strings.Contains(s, "30d · only with trash or staggered versions, not saved") {
		t.Errorf("the kept-for value is not shown as not saved:\n%s", s)
	}
	ctrl(t, m, 's')
	if m.settings != nil {
		t.Fatalf("form still open: %s", m.settings.result)
	}
	if got := configFile(t, m); !strings.Contains(got, "versioning = { kind = \"none\" }\n") {
		t.Errorf("config:\n%s", got)
	}
}

// What a profile does on a metered connection is a choice in its form, the
// default pause; run is written to the config file as such.
func TestMeteredSetting(t *testing.T) {
	m := profilesTab(t, profilesConfig, 40)
	press(t, m, "e") // live
	toRow(t, m, "metered")
	if s := screen(m); !strings.Contains(s, "Metered") || !strings.Contains(s, "wait for another, or run anyway") {
		t.Fatalf("no metered row:\n%s", s)
	}
	send(t, m, tea.KeyPressMsg{Code: tea.KeyLeft}) // from not set, round to run
	ctrl(t, m, 's')
	if m.settings != nil {
		t.Fatalf("form still open: %s", m.settings.result)
	}
	got := configFile(t, m)
	if !strings.Contains(got, `metered = "run"`) {
		t.Fatalf("config:\n%s", got)
	}
	cfg, err := config.Parse([]byte(got), "/home/user")
	if err != nil {
		t.Fatal(err)
	}
	// docs uses live, and takes its setting.
	if s, ok := cfg.Shuttle("docs"); !ok || s.Settings.Metered != config.MeteredRun || s.Sources["metered"] != config.SourceProfile {
		t.Errorf("docs loads with metered %q from %v, want run from its profile", s.Settings.Metered, s.Sources["metered"])
	}
}
