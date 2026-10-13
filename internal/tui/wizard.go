// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// wizard adds a shuttle, or edits one, a step at a time: its name and
// share, its folder on the share, its folder here, its profile, and the
// settings it has of its own.
type wizard struct {
	editing  string          // the shuttle's name; "" for a new one
	was      *config.Shuttle // the shuttle as it was, for an edit
	raw      map[string]any  // its table as written, for an edit
	step     int
	field    int // on the first step: 0 the name, 1 the share
	name     *textinput.Model
	shares   []string
	share    int
	profiles []string // "" first: no profile
	profile  int
	remote   *tree
	local    *tree
	settings *settingsForm
	remoteFS fsys.FS
	fsShare  string // the share remoteFS is open on
	dirs     [2]string
	busy     string
	result   string
	failed   bool
}

var wizardSteps = []string{"Name and share", "On the share", "On this computer", "Profile", "Settings"}

const (
	stepName = iota
	stepRemote
	stepLocal
	stepProfile
	stepSettings
)

type openedMsg struct {
	share string
	fs    fsys.FS
	err   error
}

// openWizard opens the wizard on st, or on a new shuttle when st is nil.
func (m *model) openWizard(st *config.Shuttle) tea.Cmd {
	if m.cfg == nil || m.cfgErr != nil {
		m.say("the config file has an error: fix it first", true)
		return nil
	}
	shares := m.shareNames()
	if len(shares) == 0 {
		m.say("there is no share yet: add one on the Shares tab (5)", true)
		return nil
	}
	w := &wizard{shares: shares, profiles: append([]string{""}, m.cfg.Profiles...), name: newInput("")}
	w.dirs = [2]string{"/", m.o.Home}
	if st != nil {
		w.editing, w.was = st.Name, st
		w.raw, _ = config.RawTable(m.cfgData, "shuttles", st.Name)
		w.share = max(slices.Index(shares, st.Share), 0)
		w.profile = max(slices.Index(w.profiles, st.Profile), 0)
		w.dirs = [2]string{cleanSlash(st.Remote), st.Local}
		w.field = 1 // renaming is not supported
	}
	m.wizard = w
	if st == nil {
		return w.name.Focus()
	}
	return nil
}

func (m *model) closeWizard() {
	if w := m.wizard; w != nil && w.remoteFS != nil {
		w.remoteFS.Close()
	}
	m.wizard = nil
}

// connect opens the chosen share for browsing, unless it is open already.
func (m *model) connect() tea.Cmd {
	w := m.wizard
	name := w.shares[w.share]
	if w.fsShare == name && w.remote != nil {
		return nil
	}
	if w.remoteFS != nil {
		w.remoteFS.Close()
		w.remoteFS = nil
	}
	sh := m.cfg.Shares[name]
	w.busy = "connecting to " + where(&sh) + "…"
	browse, store := m.o.Browse, m.o.Store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, keyringTimeout)
		defer cancel()
		f, err := browse(ctx, &sh, store)
		return openedMsg{share: name, fs: f, err: err}
	}
}

func (m *model) opened(msg openedMsg) tea.Cmd {
	w := m.wizard
	if w == nil || msg.share != w.shares[w.share] {
		if msg.fs != nil {
			msg.fs.Close()
		}
		return nil
	}
	w.busy, w.remoteFS, w.fsShare = "", msg.fs, msg.share
	old := w.remote
	if old != nil && old.where != "on "+msg.share {
		old = nil // marks on another share mean nothing here
	}
	dir := "/"
	if old != nil {
		dir = old.dir()
	} else if w.was != nil && w.was.Share == msg.share {
		dir = w.dirs[0]
	}
	if msg.err != nil {
		openErr := msg.err
		w.remote = newTree("on "+msg.share, dir, func(context.Context, string) ([]entry, error) { return nil, openErr })
	} else {
		w.remote = remoteTree("on "+msg.share, msg.fs, dir)
	}
	switch {
	case old != nil:
		w.remote.pick, w.remote.marks = old.pick, old.marks
	case w.was != nil && w.was.Share == msg.share:
		w.remote.setSelection(w.was.Settings.Include, w.was.Settings.Exclude)
	}
	return w.remote.load(m.ctx, w.remote.root)
}

func (m *model) wizardKey(msg tea.KeyPressMsg) tea.Cmd {
	w := m.wizard
	k := msg.String()
	if w.busy != "" {
		if k == "esc" {
			m.closeWizard()
		}
		return nil
	}
	browsing := (w.step == stepRemote && w.remote != nil && w.remote.typing) ||
		(w.step == stepLocal && w.local != nil && w.local.typing)
	if k == "esc" && !browsing {
		return m.back()
	}
	switch w.step {
	case stepName:
		switch k {
		case "tab", "shift+tab":
			if w.editing == "" {
				w.field = 1 - w.field
				if w.field == 0 {
					return w.name.Focus()
				}
				w.name.Blur()
			}
			return nil
		case "enter":
			return m.next()
		}
		if w.field == 1 {
			switch k {
			case "up", "k":
				w.share = max(w.share-1, 0)
			case "down", "j":
				w.share = min(w.share+1, len(w.shares)-1)
			}
			return nil
		}
		updated, cmd := w.name.Update(msg)
		*w.name = updated
		return cmd
	case stepRemote:
		cmd, chosen := w.remote.key(m.ctx, msg)
		if chosen {
			return tea.Batch(cmd, m.next())
		}
		return cmd
	case stepLocal:
		cmd, chosen := w.local.key(m.ctx, msg)
		if chosen {
			return tea.Batch(cmd, m.next())
		}
		return cmd
	case stepProfile:
		switch k {
		case "up", "k":
			w.profile = max(w.profile-1, 0)
		case "down", "j":
			w.profile = min(w.profile+1, len(w.profiles)-1)
		case "enter":
			return m.next()
		}
		return nil
	}
	cmd, save := w.settings.key(msg)
	if save {
		return m.saveWizard()
	}
	return cmd
}

func (m *model) back() tea.Cmd {
	w := m.wizard
	w.result = ""
	if w.step == stepName {
		m.closeWizard()
		return nil
	}
	if w.step == stepLocal {
		w.remote.adopt(w.local) // what was marked here
	}
	w.step--
	return nil
}

// next checks the step and moves on.
func (m *model) next() tea.Cmd {
	w := m.wizard
	w.result, w.failed = "", false
	switch w.step {
	case stepName:
		if w.editing == "" {
			name := strings.TrimSpace(w.name.Value())
			switch {
			case name == "":
				return w.fail("the shuttle needs a name")
			case slices.ContainsFunc(m.cfg.Shuttles, func(s config.Shuttle) bool { return s.Name == name }):
				return w.fail("there is a shuttle named " + name + " already")
			}
		}
		w.step = stepRemote
		return m.connect()
	case stepRemote:
		if w.remote.pick == pickInclude && len(w.remote.inside()) == 0 {
			return w.fail("nothing is marked to sync: space marks a file or folder, or m syncs everything")
		}
		w.dirs[0] = w.remote.dir()
		w.step = stepLocal
		var cmd tea.Cmd
		if w.local == nil {
			w.local = localTree(w.dirs[1], m.o.Home)
			cmd = w.local.load(m.ctx, w.local.root)
		}
		w.local.adopt(w.remote)
		return cmd
	case stepLocal:
		if w.local.pick == pickInclude && len(w.local.inside()) == 0 {
			return w.fail("nothing is marked to sync: space marks a file or folder, or m syncs everything")
		}
		dir := w.local.dir()
		if !filepath.IsAbs(dir) {
			return w.fail("the folder here must be a full path, e.g. " + filepath.Join(m.o.Home, "Documents"))
		}
		w.dirs[1] = dir
		w.step = stepProfile
	case stepProfile:
		profile := w.profiles[w.profile]
		var base map[string]any
		from := ""
		if profile != "" {
			base, from = m.rawProfile(profile), profile
		}
		w.settings = newSettingsForm("shuttle", w.editing, w.raw, base, from)
		w.settings.title = "Settings of its own"
		if profile == "" {
			w.settings.title = "Settings (no profile: what is not set takes the default)"
		}
		w.step = stepSettings
		return w.settings.focusOn(0)
	}
	return nil
}

func (w *wizard) fail(msg string) tea.Cmd {
	w.result, w.failed = msg, true
	return nil
}

// saveWizard asks what needs asking, then saves.
func (m *model) saveWizard() tea.Cmd {
	w := m.wizard
	name, share, profile := w.editing, w.shares[w.share], w.profiles[w.profile]
	if name == "" {
		name = strings.TrimSpace(w.name.Value())
	}
	remote, local := share+":"+w.dirs[0], w.dirs[1]
	include, exclude := w.local.selection()
	save := func() tea.Cmd {
		w.busy = "saving…"
		path, home, before, f := m.o.Config, m.o.Home, m.cfgData, w.settings
		written := local
		if rest, ok := strings.CutPrefix(local, home+string(filepath.Separator)); ok {
			written = "~/" + filepath.ToSlash(rest)
		}
		return func() tea.Msg {
			table := []string{"shuttles", name}
			err := editConfig(path, home, before, func(d *config.Doc) error {
				var err error
				if profile == "" {
					err = d.Delete(table, "profile")
				} else {
					err = d.Set(table, "profile", profile)
				}
				if err != nil {
					return err
				}
				if err := d.Set(table, "local", written); err != nil {
					return err
				}
				if err := d.Set(table, "remote", remote); err != nil {
					return err
				}
				for key, paths := range map[string][]string{"include": include, "exclude": exclude} {
					if len(paths) == 0 {
						err = d.Delete(table, key)
					} else {
						err = d.Set(table, key, paths)
					}
					if err != nil {
						return err
					}
				}
				return f.apply(d, table)
			})
			return savedMsg{text: "saved the shuttle " + name, err: err, wizard: true, shuttle: name}
		}
	}
	moved := w.was != nil && (w.was.Share != share || cleanSlash(w.was.Remote) != w.dirs[0] || w.was.Local != local)
	then := func() tea.Cmd {
		if !moved {
			return save()
		}
		m.ask(name+" now syncs other folders: it starts afresh, like a first sync. Nothing is deleted; "+
			"files that differ become conflicts. Save?", save)
		return nil
	}
	if _, err := os.Stat(local); errors.Is(err, os.ErrNotExist) {
		m.ask("There is no folder "+local+" here. Create it?", func() tea.Cmd {
			if err := os.MkdirAll(local, 0o755); err != nil { //nolint:gosec // a folder the user chose, theirs to read
				w.result, w.failed = err.Error(), true
				return nil
			}
			return then()
		})
		return nil
	}
	return then()
}

func (w *wizard) lines(m *model, room int) []string {
	title := "New shuttle"
	if w.editing != "" {
		title = "The shuttle " + w.editing
	}
	steps := make([]string, 0, len(wizardSteps))
	for i, s := range wizardSteps {
		label := fmt.Sprintf("%d %s", i+1, s)
		if i == w.step {
			label = activeTab.Render(" " + label + " ")
		} else {
			label = faint.Render(label)
		}
		steps = append(steps, label)
	}
	head := []string{headStyle.Render(title) + "   " + strings.Join(steps, faint.Render(" › ")), ""}
	var foot []string
	switch {
	case w.busy != "":
		foot = []string{"", faint.Render(w.busy + " (the keyring may ask to be unlocked)")}
	case w.result != "" && w.failed:
		foot = []string{""}
		for _, l := range strings.Split(w.result, "\n") {
			foot = append(foot, badStyle.Render(l))
		}
	}
	room = max(room-len(head)-len(foot), 1)
	var body []string
	switch w.step {
	case stepName:
		name := w.name.View()
		if w.editing != "" {
			name = w.editing + faint.Render("  (renaming is not supported)")
		}
		mark := map[bool]string{true: "› ", false: "  "}
		body = append(body, mark[w.field == 0]+pad("Name", 8)+name, "", mark[w.field == 1]+"Share:")
		for i, s := range w.shares {
			sh := m.cfg.Shares[s]
			line := "    " + pad(s, 12) + faint.Render(where(&sh))
			if i == w.share {
				line = "  " + selected.Render("▸ "+pad(s, 12)) + faint.Render(where(&sh))
			}
			body = append(body, line)
		}
	case stepRemote:
		if w.remote != nil {
			body = w.remote.lines(room, m.width)
		}
	case stepLocal:
		body = w.local.lines(room, m.width)
	case stepProfile:
		body = []string{"Profile: the settings it shares with other shuttles", ""}
		for i, p := range w.profiles {
			label, about := p, ""
			if p == "" {
				label, about = "none", "its own settings only"
			} else if raw := m.rawProfile(p); raw != nil {
				about = rawText(raw["mode"]) + ", " + orManual(rawText(raw["triggers"]))
			}
			line := "    " + pad(label, 14) + faint.Render(about)
			if i == w.profile {
				line = "  " + selected.Render("▸ "+pad(label, 14)) + faint.Render(about)
			}
			body = append(body, line)
		}
	case stepSettings:
		body = w.settings.lines(room, m.width)
	}
	if len(body) > room {
		body = body[:room]
	}
	return append(append(head, body...), foot...)
}

func orManual(triggers string) string {
	if triggers == "" || triggers == "none" {
		return "manual only"
	}
	return triggers
}

// wizardKeys are the keys of the wizard's step.
func (w *wizard) keys() [][2]string {
	switch w.step {
	case stepName:
		return [][2]string{{"tab", "name/share"}, {"↑↓", "share"}, {"enter", "next"}, {"esc", "cancel"}}
	case stepRemote, stepLocal:
		t := w.remote
		if w.step == stepLocal {
			t = w.local
		}
		if t != nil && t.typing {
			return [][2]string{{"enter", "use this path"}, {"esc", "back to the list"}}
		}
		keys := [][2]string{{"↑↓", "choose"}, {"→←", "open/close"}, {"o", "use as folder"}, {"⌫", "up"}, {"m", "what to sync"}}
		if t != nil && t.pick != pickAll {
			keys = append(keys, [2]string{"space", "mark"})
		}
		return append(keys, [2]string{"enter", "next"}, [2]string{"t", "type a path"}, [2]string{"esc", "back"})
	case stepProfile:
		return [][2]string{{"↑↓", "choose"}, {"enter", "next"}, {"esc", "back"}}
	}
	return [][2]string{{"tab", "next"}, {"←→", "choose"}, {"ctrl+s", "save"}, {"esc", "back"}}
}

// input is the text input the wizard's step types into, if any.
func (w *wizard) input() *textinput.Model {
	switch {
	case w.step == stepName && w.field == 0 && w.editing == "":
		return w.name
	case w.step == stepRemote && w.remote != nil && w.remote.typing:
		return w.remote.input
	case w.step == stepLocal && w.local != nil && w.local.typing:
		return w.local.input
	case w.step == stepSettings:
		return w.settings.inputs[w.settings.rows[w.settings.focus]]
	}
	return nil
}
