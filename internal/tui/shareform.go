// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/secrets"
	"github.com/fqazzazee/shuttle/internal/shares"
)

// field is one line of the share form.
type field struct {
	key, label, hint string
}

var (
	nameField = field{"name", "Name", "how shuttles refer to it, e.g. nas"}
	typeField = field{"type", "Type", "←/→ to change: smb, nfs3, or a mounted folder (path)"}
	// The fields of each type of share, in order.
	formFields = map[config.ShareType][]field{
		config.ShareSMB: {
			nameField, typeField,
			{"host", "Host", "the server's name or address"},
			{"share", "Share", "the share's name on the server"},
			{"user", "User", "empty for a guest share"},
			{"password", "Password", "kept in the keyring, never in the config file"},
			{"domain", "Domain", "optional"},
			{"port", "Port", "optional: 445 if empty"},
		},
		config.ShareNFS3: {
			nameField, typeField,
			{"host", "Host", "the server's name or address"},
			{"export", "Export", "the exported path on the server, e.g. /volume1/home"},
			{"uid", "User ID", fmt.Sprintf("optional: yours (%d) if empty; the server believes it as sent", os.Getuid())},
			{"gid", "Group ID", fmt.Sprintf("optional: yours (%d) if empty", os.Getgid())},
			{"port", "Port", "optional: found through the server's portmapper, else 2049"},
			{"mount_port", "Mount port", "optional: found through the server's portmapper"},
		},
		config.SharePath: {
			nameField, typeField,
			{"path", "Folder", "where the share is mounted, e.g. /mnt/nas"},
		},
	}
	shareTypes = []config.ShareType{config.ShareSMB, config.ShareNFS3, config.SharePath}
)

// shareForm adds a share, or edits one.
type shareForm struct {
	editing *config.Share // nil for a new share
	typ     config.ShareType
	inputs  map[string]*textinput.Model
	focus   string // the focused field's key
	busy    string // what it waits for: keys wait too
	result  string // how the last test or save went
	failed  bool
}

func (m *model) openForm(sh *config.Share) tea.Cmd {
	f := &shareForm{editing: sh, typ: config.ShareSMB, inputs: map[string]*textinput.Model{}}
	values := map[string]string{}
	if sh != nil {
		f.typ = sh.Type
		values = map[string]string{"name": sh.Name, "host": sh.Host, "share": sh.Share, "user": sh.User,
			"domain": sh.Domain, "export": sh.Export, "path": sh.Path}
		for key, n := range map[string]int{"port": sh.Port, "mount_port": sh.MountPort} {
			if n != 0 {
				values[key] = strconv.Itoa(n)
			}
		}
		for key, id := range map[string]*uint32{"uid": sh.UID, "gid": sh.GID} {
			if id != nil {
				values[key] = strconv.FormatUint(uint64(*id), 10)
			}
		}
	}
	for _, key := range []string{"name", "host", "share", "user", "password", "domain", "port", "mount_port",
		"export", "uid", "gid", "path"} {
		in := textinput.New()
		in.Prompt = ""
		in.SetWidth(40)
		// A steady cursor: no redraw twice a second while the form is open.
		st := in.Styles()
		st.Cursor.Blink = false
		in.SetStyles(st)
		in.SetValue(values[key])
		if key == "password" {
			in.EchoMode = textinput.EchoPassword
			if sh != nil {
				in.Placeholder = "unchanged"
			}
		}
		f.inputs[key] = &in
	}
	m.form = f
	first := "name"
	if sh != nil {
		first = "type" // renaming is not supported
	}
	return f.focusOn(first)
}

func (f *shareForm) fields() []field { return formFields[f.typ] }

// editable lists the keys the cursor can go to.
func (f *shareForm) editable() []string {
	var keys []string
	for _, fl := range f.fields() {
		if fl.key == "name" && f.editing != nil {
			continue
		}
		keys = append(keys, fl.key)
	}
	return keys
}

func (f *shareForm) focusOn(key string) tea.Cmd {
	if in, ok := f.inputs[f.focus]; ok {
		in.Blur()
	}
	f.focus = key
	if in, ok := f.inputs[key]; ok {
		return in.Focus()
	}
	return nil
}

func (f *shareForm) move(by int) tea.Cmd {
	keys := f.editable()
	i := slices.Index(keys, f.focus)
	return f.focusOn(keys[(i+by+len(keys))%len(keys)])
}

func (m *model) formKey(msg tea.KeyPressMsg) tea.Cmd {
	f := m.form
	k := msg.String()
	if f.busy != "" {
		if k == "esc" {
			m.form = nil
		}
		return nil
	}
	switch k {
	case "esc":
		m.form = nil
		return nil
	case "tab", "down":
		return f.move(1)
	case "shift+tab", "up":
		return f.move(-1)
	case "enter":
		if keys := f.editable(); f.focus == keys[len(keys)-1] {
			return m.saveForm()
		}
		return f.move(1)
	case "ctrl+s":
		return m.saveForm()
	case "ctrl+t":
		sh, err := f.share()
		if err != nil {
			f.result, f.failed = err.Error(), true
			return nil
		}
		f.busy, f.result = "testing "+where(&sh)+"…", ""
		return m.test(&sh, f.inputs["password"].Value(), true)
	}
	if f.focus == "type" {
		i := slices.Index(shareTypes, f.typ)
		switch k {
		case "left":
			f.typ = shareTypes[(i+len(shareTypes)-1)%len(shareTypes)]
		case "right", "space":
			f.typ = shareTypes[(i+1)%len(shareTypes)]
		}
		return nil
	}
	in := f.inputs[f.focus]
	updated, cmd := in.Update(msg)
	*in = updated
	return cmd
}

// share is the share the form describes.
func (f *shareForm) share() (config.Share, error) {
	v := func(key string) string { return strings.TrimSpace(f.inputs[key].Value()) }
	sh := config.Share{Name: v("name"), Type: f.typ} // an edit's name is in its field, which cannot be focused
	set := map[string]*string{"host": &sh.Host, "share": &sh.Share, "user": &sh.User, "domain": &sh.Domain,
		"export": &sh.Export, "path": &sh.Path}
	for _, fl := range f.fields() {
		if p, ok := set[fl.key]; ok {
			*p = v(fl.key)
		}
		var err error
		switch fl.key {
		case "port":
			sh.Port, err = number(fl, v(fl.key))
		case "mount_port":
			sh.MountPort, err = number(fl, v(fl.key))
		case "uid":
			sh.UID, err = id(fl, v(fl.key))
		case "gid":
			sh.GID, err = id(fl, v(fl.key))
		}
		if err != nil {
			return sh, err
		}
	}
	if sh.Name == "" {
		return sh, errors.New("the share needs a name")
	}
	return sh, nil
}

// number reads an optional port; empty is 0.
func number(fl field, s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("the %s must be a number, not %q", strings.ToLower(fl.label), s)
	}
	return n, nil
}

// id reads an optional user or group ID; empty is nil, as 0 is root.
func id(fl field, s string) (*uint32, error) {
	if s == "" {
		return nil, nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("the %s must be a number from 0 to 4294967295, not %q", strings.ToLower(fl.label), s)
	}
	v := uint32(n)
	return &v, nil
}

// saveForm checks the share against the whole config, stores its password,
// then writes it to the config file.
func (m *model) saveForm() tea.Cmd {
	f := m.form
	sh, err := f.share()
	if err == nil && f.editing == nil {
		if _, taken := m.cfg.Shares[sh.Name]; taken {
			err = fmt.Errorf("there is a share named %s already", sh.Name)
		}
	}
	if err == nil {
		err = checkShare(m.cfgData, m.o.Home, &sh)
	}
	if err != nil {
		f.result, f.failed = err.Error(), true
		return nil
	}
	f.busy, f.result = "saving "+sh.Name+"…", ""
	password, old := f.inputs["password"].Value(), f.editing
	path, home, before, store := m.o.Config, m.o.Home, m.cfgData, m.o.Store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, keyringTimeout)
		defer cancel()
		// The password first: a config pointing at a share with no password
		// would fail its next run; a password no share uses harms nothing.
		note, err := storePassword(ctx, store, old, &sh, password)
		if err != nil {
			return savedMsg{err: err, form: true}
		}
		err = editConfig(path, home, before, func(d *config.Doc) error { return d.SetShare(&sh) })
		return savedMsg{text: "saved the share " + sh.Name + note, err: err, form: true, share: sh.Name}
	}
}

// checkShare checks that the config, with sh in it, loads.
func checkShare(before []byte, home string, sh *config.Share) error {
	d, err := config.ParseDoc(before)
	if err != nil {
		return err
	}
	if err := d.SetShare(sh); err != nil {
		return err
	}
	_, err = config.Parse(d.Bytes(), home)
	return err
}

// storePassword keeps sh's password in the keyring. With none typed, an
// edit that moved the share to another server, share or user takes the
// stored one along. It returns a note for the user.
func storePassword(ctx context.Context, store secrets.Store, old, sh *config.Share, password string) (string, error) {
	k, needs := shares.Key(sh)
	switch {
	case !needs:
		return "", nil
	case password != "":
		return "", store.Set(ctx, k, password)
	case old == nil:
		return "; no password is stored for it yet", nil
	}
	was, had := shares.Key(old)
	if !had || was == k {
		return "", nil
	}
	pw, err := store.Get(ctx, was)
	if errors.Is(err, secrets.ErrNotFound) {
		return "; no password is stored for it yet", nil
	}
	if err != nil {
		return "", err
	}
	return "", store.Set(ctx, k, pw)
}

func (f *shareForm) lines(width int) []string {
	title := "New share"
	if f.editing != nil {
		title = "The share " + f.editing.Name
	}
	lines := []string{headStyle.Render(title), ""}
	for _, fl := range f.fields() {
		var value string
		switch {
		case fl.key == "type" && f.focus == "type":
			value = keyStyle.Render("‹ " + string(f.typ) + " ›")
		case fl.key == "type":
			value = string(f.typ)
		case fl.key == "name" && f.editing != nil:
			value = f.editing.Name + faint.Render("  (renaming is not supported)")
		default:
			value = f.inputs[fl.key].View()
		}
		label := "  " + pad(fl.label, 12)
		if fl.key == f.focus {
			label = "› " + pad(fl.label, 12)
		}
		hint := ""
		if fl.key == f.focus {
			hint = faint.Render("  " + fl.hint)
		}
		lines = append(lines, label+pad(value, 42)+hint)
	}
	lines = append(lines, "")
	switch {
	case f.busy != "":
		lines = append(lines, faint.Render(f.busy+" (the keyring may ask to be unlocked)"))
	case f.result != "" && f.failed:
		// Servers explain at length: wrapped, not cut short.
		for _, l := range strings.Split(ansi.Wordwrap(f.result, max(width-2, 20), " "), "\n") {
			lines = append(lines, badStyle.Render(l))
		}
	case f.result != "":
		lines = append(lines, goodStyle.Render(f.result))
	}
	return lines
}
