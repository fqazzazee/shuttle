// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// The Shares tab reads and writes the config file itself, so shares can be
// set up with the daemon stopped. The daemon follows the file.

type configMsg struct {
	data []byte // nil when there is no config file yet
	cfg  *config.Config
	err  error
}

// loadConfig reads the config file.
func (m *model) loadConfig() tea.Cmd {
	path, home := m.o.Config, m.o.Home
	return func() tea.Msg {
		data, err := os.ReadFile(path) //nolint:gosec // the user's own config file
		if errors.Is(err, fs.ErrNotExist) {
			return configMsg{cfg: &config.Config{Shares: map[string]config.Share{}}}
		}
		if err != nil {
			return configMsg{err: err}
		}
		cfg, err := config.Parse(data, home)
		return configMsg{data: data, cfg: cfg, err: err}
	}
}

func (m *model) gotConfig(msg configMsg) {
	if msg.err == nil && m.cfg != nil && bytes.Equal(msg.data, m.cfgData) && m.cfgErr == nil {
		return // unchanged
	}
	m.cfgLoaded = true
	m.cfgData, m.cfg, m.cfgErr = msg.data, msg.cfg, msg.err
	names := m.shareNames()
	if !slices.Contains(names, m.shareSel) && len(names) > 0 {
		m.shareSel = names[0]
	}
	if m.cfg != nil && !slices.Contains(m.cfg.Profiles, m.profileSel) && len(m.cfg.Profiles) > 0 {
		m.profileSel = m.cfg.Profiles[0]
	}
}

func (m *model) shareNames() []string {
	if m.cfg == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(m.cfg.Shares))
}

// usedBy lists the shuttles that use a share.
func (m *model) usedBy(share string) []string {
	var out []string
	for i := range m.cfg.Shuttles {
		if m.cfg.Shuttles[i].Share == share {
			out = append(out, m.cfg.Shuttles[i].Name)
		}
	}
	return out
}

// testResult is how a share's last test went.
type testResult struct {
	at    time.Time
	err   error
	space fsys.Space
}

type testedMsg struct {
	share string
	err   error
	space fsys.Space
	form  bool // tested from the form, with what it holds
}

// test tries a share, with password if given, else the stored one.
func (m *model) test(share *config.Share, password string, form bool) tea.Cmd {
	o, sh := m.o, *share
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, keyringTimeout)
		defer cancel()
		space, err := o.Test(ctx, &sh, password, o.Store)
		return testedMsg{share: sh.Name, err: err, space: space, form: form}
	}
}

func (m *model) sharesKey(k string) tea.Cmd {
	names := m.shareNames()
	i := slices.Index(names, m.shareSel)
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
		m.shareSel = names[min(max(i, 0), len(names)-1)]
	}
	if m.cfgErr != nil {
		return nil // the file must be fixed first: saving checks all of it
	}
	if k == "n" {
		return m.openForm(nil)
	}
	sh, ok := m.cfg.Shares[m.shareSel]
	if !ok {
		return nil
	}
	switch k {
	case "e", "enter":
		return m.openForm(&sh)
	case "t":
		m.say("testing "+sh.Name+"…", false)
		return m.test(&sh, "", false)
	case "x":
		if users := m.usedBy(sh.Name); len(users) > 0 {
			m.say(fmt.Sprintf("%s is used by %s: change or remove them first", sh.Name, strings.Join(users, ", ")), true)
			return nil
		}
		name := sh.Name
		m.ask(fmt.Sprintf("Delete the share %s from the config file?", name), func() tea.Cmd {
			return m.saveConfig("deleted the share "+name, func(d *config.Doc) error { return d.DeleteShare(name) })
		})
	}
	return nil
}

type savedMsg struct {
	text     string
	err      error
	form     bool   // the share form's save: it closes on success
	share    string // the share saved, selected next
	settings bool   // the settings form's save: it closes on success
	profile  string // the profile saved, selected next
	wizard   bool   // the shuttle wizard's save: it closes on success
	shuttle  string // the shuttle saved, selected next
}

// saveConfig edits the config file as it was read, and saves it if it is
// still that: an edit made meanwhile, by hand or by the daemon, is not
// overwritten.
func (m *model) saveConfig(done string, change func(*config.Doc) error) tea.Cmd {
	path, home, before := m.o.Config, m.o.Home, m.cfgData
	return func() tea.Msg {
		err := editConfig(path, home, before, change)
		return savedMsg{text: done, err: err}
	}
}

func editConfig(path, home string, before []byte, change func(*config.Doc) error) error {
	d, err := config.ParseDoc(before)
	if err != nil {
		return err
	}
	if err := change(d); err != nil {
		return err
	}
	err = config.Save(path, before, d.Bytes(), home)
	if errors.Is(err, config.ErrChanged) {
		return errors.New("the config file changed meanwhile; it is read again: try once more")
	}
	return err
}

// where says where a share is: smb://user@host/share, nfs://host/export, or
// the mounted folder.
func where(sh *config.Share) string {
	host := sh.Host
	if sh.Port != 0 {
		host += ":" + strconv.Itoa(sh.Port)
	}
	switch sh.Type {
	case config.ShareSMB:
		user := sh.User
		if sh.Domain != "" {
			user = sh.Domain + ";" + user
		}
		if user != "" {
			user += "@"
		}
		return "smb://" + user + host + "/" + sh.Share
	case config.ShareNFS3:
		return "nfs://" + host + sh.Export
	}
	return sh.Path
}

func (m *model) sharesLines(room int) []string {
	switch {
	case m.form != nil:
		return m.form.lines(m.width)
	case !m.cfgLoaded:
		return []string{faint.Render("Reading the config file…")}
	case m.cfgErr != nil:
		return []string{
			badStyle.Render("The config file has an error: " + firstLine(m.cfgErr.Error())),
			"",
			"Fix it in " + m.o.Config + " first: saving a share checks the whole file.",
		}
	}
	names := m.shareNames()
	if len(names) == 0 {
		return withBus("No shares yet: n adds one.")
	}
	nameWidth, whereWidth := len("NAME"), len("WHERE")
	for _, n := range names {
		sh := m.cfg.Shares[n]
		nameWidth = max(nameWidth, len(n))
		whereWidth = max(whereWidth, len(where(&sh)))
	}
	nameWidth += 2
	whereWidth = min(whereWidth, 50) + 2
	row := func(name, typ, at, users, free, test string) string {
		return pad(name, nameWidth) + pad(typ, 6) + pad(at, whereWidth) + pad(users, 22) + pad(free, 20) + test
	}
	lines := []string{headStyle.Render(row("NAME", "TYPE", "WHERE", "USED BY", "FREE", "LAST TEST"))}
	now := m.now()
	for _, n := range names {
		if len(lines) >= room {
			break
		}
		sh := m.cfg.Shares[n]
		test := ""
		if r, ok := m.tested[n]; ok {
			test = goodStyle.Render("works, " + stamp(r.at, now))
			if r.err != nil {
				test = badStyle.Render("failed, " + stamp(r.at, now))
			}
		}
		free := freeText(m.shareSpace(n))
		if n == m.shareSel {
			free = ansi.Strip(free)
		}
		line := row(n, string(sh.Type), where(&sh), orNone(strings.Join(m.usedBy(n), ", ")), free, test)
		if n == m.shareSel {
			line = selected.Render(pad(line, m.width))
		}
		lines = append(lines, line)
	}
	if here := m.hereLine(); here != "" && len(lines)+2 <= room {
		lines = append(lines, "", here)
	}
	return lines
}

func (m *model) gotTest(msg testedMsg) {
	if msg.form {
		if m.form != nil {
			m.form.busy, m.form.failed = "", msg.err != nil
			m.form.result = "it works: logged in and read the share's top folder"
			if msg.err != nil {
				m.form.result = msg.err.Error()
			}
		}
		return
	}
	m.tested[msg.share] = testResult{at: m.now(), err: msg.err, space: msg.space}
	switch {
	case msg.err != nil:
		m.say(msg.share+": "+firstLine(msg.err.Error()), true)
	case msg.space.Total > 0:
		m.say(msg.share+" works: "+spaceText(msg.space), false)
	default:
		m.say(msg.share+" works", false)
	}
}

func (m *model) gotSave(msg *savedMsg) tea.Cmd {
	if msg.form && m.form != nil {
		m.form.busy = ""
		if msg.err != nil {
			m.form.result, m.form.failed = msg.err.Error(), true
			return m.loadConfig()
		}
		m.form = nil
	}
	if msg.settings && m.settings != nil {
		m.settings.busy = ""
		if msg.err != nil {
			m.settings.result, m.settings.failed = msg.err.Error(), true
			return m.loadConfig()
		}
		m.settings = nil
	}
	if msg.wizard && m.wizard != nil {
		m.wizard.busy = ""
		if msg.err != nil {
			m.wizard.result, m.wizard.failed = msg.err.Error(), true
			return m.loadConfig()
		}
		m.closeWizard()
	}
	if msg.shuttle != "" && msg.err == nil {
		m.selected = msg.shuttle
	}
	if msg.share != "" && msg.err == nil {
		m.shareSel = msg.share
	}
	if msg.profile != "" && msg.err == nil {
		m.profileSel = msg.profile
	}
	if msg.err != nil {
		m.say(firstLine(msg.err.Error()), true)
	} else {
		m.say(msg.text, false)
	}
	return m.loadConfig()
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
