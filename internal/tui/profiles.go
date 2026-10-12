// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/config"
)

// The Profiles tab edits the config file as the Shares tab does: in place,
// with the daemon running or not.

// profilesUsing lists the shuttles that use a profile.
func (m *model) profilesUsing(profile string) []string {
	var out []string
	for i := range m.cfg.Shuttles {
		if m.cfg.Shuttles[i].Profile == profile {
			out = append(out, m.cfg.Shuttles[i].Name)
		}
	}
	return out
}

func (m *model) rawProfile(name string) map[string]any {
	raw, err := config.RawTable(m.cfgData, "profiles", name)
	if err != nil {
		return nil
	}
	return raw
}

func (m *model) profilesKey(k string) tea.Cmd {
	var names []string
	if m.cfg != nil {
		names = m.cfg.Profiles
	}
	i := slices.Index(names, m.profileSel)
	switch k {
	case "up", "k":
		i--
	case "down", "j":
		i++
	case "home", "g":
		i = 0
	case "end", "G":
		i = len(names) - 1
	}
	if len(names) > 0 {
		m.profileSel = names[min(max(i, 0), len(names)-1)]
	}
	if m.cfgErr != nil || m.cfg == nil {
		return nil // the file must be fixed first: saving checks all of it
	}
	if k == "n" {
		m.settings = newProfileForm("", "", nil)
		return m.settings.focusOn(0)
	}
	name := m.profileSel
	if !slices.Contains(names, name) {
		return nil
	}
	switch k {
	case "e", "enter":
		m.settings = newProfileForm(name, "", m.rawProfile(name))
		return m.settings.focusOn(0)
	case "c":
		m.settings = newProfileForm("", name+"-copy", m.rawProfile(name))
		return m.settings.focusOn(0)
	case "x":
		if users := m.profilesUsing(name); len(users) > 0 {
			m.say(fmt.Sprintf("%s is used by %s: change them first", name, strings.Join(users, ", ")), true)
			return nil
		}
		m.ask(fmt.Sprintf("Delete the profile %s from the config file?", name), func() tea.Cmd {
			return m.saveConfig("deleted the profile "+name, func(d *config.Doc) error {
				return d.DeleteTable([]string{"profiles", name})
			})
		})
	}
	return nil
}

// settingsKey passes a key to the open settings form, and saves it when it
// asks.
func (m *model) settingsKey(msg tea.KeyPressMsg) tea.Cmd {
	f := m.settings
	if f.busy != "" {
		if msg.String() == "esc" {
			m.settings = nil
		}
		return nil
	}
	if msg.String() == "esc" {
		m.settings = nil
		return nil
	}
	cmd, save := f.key(msg)
	if !save {
		return cmd
	}
	name := f.name()
	var err error
	switch {
	case name == "":
		err = fmt.Errorf("the %s needs a name", f.kind)
	case f.editing == "" && slices.Contains(m.cfg.Profiles, name):
		err = fmt.Errorf("there is a profile named %s already", name)
	case !f.set():
		err = errNothingSet
	}
	if err != nil {
		f.result, f.failed = err.Error(), true
		return nil
	}
	// Saving checks the whole config, and writes nothing if it does not load.
	f.busy, f.result = "saving…", ""
	table := []string{"profiles", name}
	path, home, before := m.o.Config, m.o.Home, m.cfgData
	return func() tea.Msg {
		err := editConfig(path, home, before, func(d *config.Doc) error { return f.apply(d, table) })
		return savedMsg{text: "saved the profile " + name, err: err, settings: true, profile: name}
	}
}

func (m *model) profilesLines(room int) []string {
	switch {
	case m.settings != nil:
		return m.settings.lines(room, m.width)
	case !m.cfgLoaded:
		return []string{faint.Render("Reading the config file…")}
	case m.cfgErr != nil:
		return []string{
			badStyle.Render("The config file has an error: " + firstLine(m.cfgErr.Error())),
			"",
			"Fix it in " + m.o.Config + " first: saving a profile checks the whole file.",
		}
	case len(m.cfg.Profiles) == 0:
		return []string{"No profiles yet: n adds one. A profile is a set of settings that shuttles share."}
	}
	nameWidth := len("NAME")
	for _, n := range m.cfg.Profiles {
		nameWidth = max(nameWidth, len(n))
	}
	nameWidth += 2
	row := func(name, mode, triggers, users string) string {
		return pad(name, nameWidth) + pad(mode, 7) + pad(triggers, 34) + users
	}
	lines := []string{headStyle.Render(row("NAME", "MODE", "TRIGGERS", "USED BY"))}
	for _, n := range m.cfg.Profiles {
		if len(lines) >= room {
			break
		}
		raw := m.rawProfile(n)
		triggers := rawText(raw["triggers"])
		if _, set := raw["triggers"]; !set || triggers == "" {
			triggers = "manual only"
		}
		line := row(n, orNone(rawText(raw["mode"])), triggers, orNone(strings.Join(m.profilesUsing(n), ", ")))
		if n == m.profileSel {
			line = selected.Render(pad(line, m.width))
		}
		lines = append(lines, line)
	}
	return lines
}
