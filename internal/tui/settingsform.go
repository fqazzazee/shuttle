// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"errors"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/config"
)

// settingsForm edits the settings of a table: a profile's, or a shuttle's
// overrides of its profile.
type settingsForm struct {
	kind    string // "profile", "shuttle"
	title   string
	editing string // the table's name; "" for a new one
	// base is what applies where the form sets nothing: a shuttle's
	// profile's settings, as written; from names it. Empty for a profile:
	// the defaults apply.
	base   map[string]any
	from   string
	values map[string]string
	inputs map[string]*textinput.Model // the typed settings, and the name
	rows   []string                    // the keys, in order; "name" first when new
	focus  int
	busy   string
	result string
	failed bool
}

// newProfileForm opens a form on a profile's settings as written. A new
// profile gets a name row, filled in with name.
func newProfileForm(editing, name string, raw map[string]any) *settingsForm {
	f := newSettingsForm("profile", editing, raw, nil, "")
	f.title = "New profile"
	if editing != "" {
		f.title = "The profile " + editing
	}
	f.rows = append([]string{"name"}, f.rows...)
	if editing != "" {
		f.rows = f.rows[1:] // renaming is not supported
	}
	f.inputs["name"] = newInput(name)
	return f
}

// newSettingsForm opens a form on raw, a table's settings as written, over
// base, what applies where they are not set.
func newSettingsForm(kind, editing string, raw, base map[string]any, from string) *settingsForm {
	f := &settingsForm{kind: kind, editing: editing, base: base, from: from,
		values: map[string]string{}, inputs: map[string]*textinput.Model{}}
	for _, s := range config.Rows {
		f.rows = append(f.rows, s.Key)
		f.values[s.Key] = config.RawText(config.Lookup(raw, s.Key))
		if s.Choices == nil {
			f.inputs[s.Key] = newInput(f.values[s.Key])
		}
	}
	return f
}

func newInput(value string) *textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.SetWidth(36)
	in.SetValue(value)
	st := in.Styles()
	st.Cursor.Blink = false
	in.SetStyles(st)
	return &in
}

func (f *settingsForm) focusOn(i int) tea.Cmd {
	if in, ok := f.inputs[f.rows[f.focus]]; ok {
		in.Blur()
	}
	f.focus = (i + len(f.rows)) % len(f.rows)
	if in, ok := f.inputs[f.rows[f.focus]]; ok {
		return in.Focus()
	}
	return nil
}

// effective is what applies for key: the form's value, else the base's (a
// shuttle's profile), else the default.
func (f *settingsForm) effective(key string) string {
	if v := f.value(key); v != "" {
		return v
	}
	if v := config.RawText(config.Lookup(f.base, key)); v != "" {
		return v
	}
	return config.DefaultText(key)
}

// off says why the row for key does not apply as the form stands, "" when
// it does: an interval without the schedule trigger, say. Such a row is
// shown greyed out, the cursor passes it by, and it is not saved: the
// config refuses some of them (a count with trash versions) and ignores
// the rest.
func (f *settingsForm) off(key string) string {
	if s := config.RowOf(key); s != nil && s.Needs != nil {
		return s.Needs(f.effective)
	}
	return ""
}

// next is the row step rows on from the focused one that applies, wrapping.
func (f *settingsForm) next(step int) int {
	i := f.focus
	for range f.rows {
		i = (i + step + len(f.rows)) % len(f.rows)
		if f.off(f.rows[i]) == "" {
			break
		}
	}
	return i
}

// key handles a key; done reports that the form asks to be saved.
func (f *settingsForm) key(msg tea.KeyPressMsg) (cmd tea.Cmd, save bool) {
	switch k := msg.String(); k {
	case "tab", "down":
		return f.focusOn(f.next(1)), false
	case "shift+tab", "up":
		return f.focusOn(f.next(-1)), false
	case "enter":
		if f.next(1) <= f.focus { // the last row that applies
			return nil, true
		}
		return f.focusOn(f.next(1)), false
	case "ctrl+s":
		return nil, true
	}
	key := f.rows[f.focus]
	if s := config.RowOf(key); s != nil && s.Choices != nil {
		// The choices, and "not set" before them.
		all := append([]string{""}, s.Choices...)
		i := slices.Index(all, f.values[key])
		switch msg.String() {
		case "left":
			f.values[key] = all[(i+len(all)-1)%len(all)]
		case "right", "space":
			f.values[key] = all[(i+1)%len(all)]
		case "backspace", "delete":
			f.values[key] = ""
		}
		return nil, false
	}
	in := f.inputs[key]
	updated, cmd := in.Update(msg)
	*in = updated
	return cmd, false
}

// value is a row's value as the form holds it.
func (f *settingsForm) value(key string) string {
	if in, ok := f.inputs[key]; ok {
		return strings.TrimSpace(in.Value())
	}
	return f.values[key]
}

// name is the table's name: the edited one, or the one typed.
func (f *settingsForm) name() string {
	if f.editing != "" {
		return f.editing
	}
	return f.value("name")
}

// apply writes the form's settings into table: see config.WriteRows.
func (f *settingsForm) apply(d *config.Doc, table []string) error {
	return config.WriteRows(d, table, f.value, f.off)
}

// set reports whether the form sets anything: a table with nothing in it
// would not be written.
func (f *settingsForm) set() bool {
	return slices.ContainsFunc(config.Rows, func(s config.Row) bool { return f.value(s.Key) != "" })
}

func (f *settingsForm) lines(room, width int) []string {
	head := []string{headStyle.Render(f.title), ""}
	if f.from != "" {
		head = []string{headStyle.Render(f.title), faint.Render("● marks a setting of this shuttle's own; emptied, the profile's applies again"), ""}
	}
	var foot []string
	switch {
	case f.busy != "":
		foot = []string{"", faint.Render(f.busy)}
	case f.result != "" && f.failed:
		foot = []string{""}
		for _, l := range strings.Split(ansi.Wordwrap(f.result, max(width-2, 20), " "), "\n") {
			foot = append(foot, badStyle.Render(l))
		}
	case f.result != "":
		foot = []string{"", goodStyle.Render(f.result)}
	}
	rows := max(room-len(head)-len(foot), 1)
	first := min(max(f.focus-rows/2, 0), max(len(f.rows)-rows, 0))
	lines := head
	for i := first; i < len(f.rows) && i < first+rows; i++ {
		lines = append(lines, f.row(i))
	}
	return append(lines, foot...)
}

func (f *settingsForm) row(i int) string {
	key := f.rows[i]
	label, hint := "Name", "letters, digits, . _ -"
	s := config.RowOf(key)
	if s != nil {
		label, hint = s.Label, s.Hint
	}
	var value string
	switch {
	case s != nil && s.Choices != nil:
		value = f.values[key]
		if i == f.focus {
			value = keyStyle.Render("‹ " + f.orBase(s, value) + " ›")
		}
	default:
		value = f.inputs[key].View()
	}
	if s != nil && f.value(key) == "" && i != f.focus {
		value = faint.Render("not set: " + s.Default)
		if from := config.RawText(config.Lookup(f.base, key)); from != "" {
			value = faint.Render("from " + f.from + ": " + from)
		}
	}
	mark := "  "
	if i == f.focus {
		mark = "› "
	}
	if s != nil && f.from != "" && f.value(key) != "" {
		mark = map[bool]string{true: "›●", false: " ●"}[i == f.focus]
	}
	if why := f.off(key); why != "" {
		// Greyed out, saying why; a value set is shown, and not saved.
		if v := f.value(key); v != "" {
			why = v + " · " + why + ", not saved"
		}
		return mark + faint.Render(pad(label, 15)+why)
	}
	line := mark + pad(label, 15) + pad(value, 38)
	if i == f.focus {
		line += faint.Render(hint)
	}
	return line
}

// orBase is a choice's value, or what applies when it is not set.
func (f *settingsForm) orBase(s *config.Row, value string) string {
	switch {
	case value != "":
		return value
	case config.RawText(config.Lookup(f.base, s.Key)) != "":
		return "from " + f.from + ": " + config.RawText(config.Lookup(f.base, s.Key))
	}
	return "not set: " + s.Default
}

var errNothingSet = errors.New("set at least one setting: a profile with none would not be written")
