// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/config"
)

// setting is one row of the settings form: a config key, "group.key" for
// one inside versioning, bandwidth or notify.
type setting struct {
	key, label string
	choices    []string // a choice among these; nil: typed text
	list       bool     // typed as a comma-separated list
	number     bool
	def        string // what applies when it is not set
	hint       string
	// needs says why the setting does not apply, given the effective value
	// of the others; "" when it does. nil: it always applies.
	needs func(get func(key string) string) string
}

// withTrigger: the setting applies only with that trigger.
func withTrigger(trigger string) func(func(string) string) string {
	return func(get func(string) string) string {
		if slices.Contains(listItems(get("triggers")), trigger) {
			return ""
		}
		return "only with the " + trigger + " trigger"
	}
}

// whenIs: the setting applies only while key is one of values. An unknown
// value (no mode anywhere yet) leaves it applying: better than guessing.
func whenIs(key, what string, values ...string) func(func(string) string) string {
	return func(get func(string) string) string {
		if v := get(key); v == "" || slices.Contains(values, v) {
			return ""
		}
		return "only " + what
	}
}

// listItems splits a list as typed: "realtime, schedule"; "none" is empty.
func listItems(text string) []string {
	var items []string
	for _, x := range strings.Split(text, ",") {
		if x = strings.TrimSpace(x); x != "" && x != "none" {
			items = append(items, x)
		}
	}
	return items
}

// defaults are the built-in values the dependencies between settings look
// at, as typed; config.Defaults is where they come from.
var defaults = func() map[string]string {
	d := config.Defaults()
	return map[string]string{
		"conflicts": string(d.Conflicts), "deletions": string(d.Deletions),
		"versioning.kind": string(d.Versioning.Kind),
	}
}()

var settings = []setting{
	{key: "mode", label: "Mode", choices: []string{"push", "pull", "sync"}, def: "none: each shuttle must say",
		hint: "↑ push: this computer to the share; ↓ pull: the share to this computer; ⇅ sync: both ways"},
	{key: "triggers", label: "Triggers", list: true, def: "manual only",
		hint: "any of realtime, schedule, share-available"},
	{key: "interval", label: "Every", def: "-", hint: "with the schedule trigger: run this often, e.g. 6h",
		needs: withTrigger("schedule")},
	{key: "at", label: "At", list: true, def: "-", hint: "with the schedule trigger: times of day, e.g. 02:00, 14:30",
		needs: withTrigger("schedule")},
	{key: "poll_interval", label: "Poll every", def: "60s", hint: "with the realtime trigger: how often the share is checked for changes",
		needs: withTrigger("realtime")},
	{key: "conflicts", label: "Conflicts", choices: []string{"keep-both", "newest-wins", "ask"}, def: "keep-both",
		hint: "a file changed on both sides: keep both, take the newer, or ask you"},
	{key: "conflict_hold", label: "Ask holds", choices: []string{"file", "shuttle"}, def: "file",
		hint: "with ask: wait on the file alone, or on the whole shuttle", needs: whenIs("conflicts", "when conflicts are ask", "ask")},
	{key: "deletions", label: "Deletions", choices: []string{"additive", "mirror"}, def: "additive",
		hint:  "push and pull: additive never deletes on the other side; mirror deletes there too",
		needs: whenIs("mode", "for push and pull: sync always deletes on both sides", "push", "pull")},
	{key: "deletion_guard", label: "Guard", number: true, def: "50",
		hint: "hold deletions of more than this percent of the files at once; 0 is off",
		needs: func(get func(string) string) string {
			if get("mode") == "sync" || get("mode") == "" || get("deletions") == "mirror" {
				return ""
			}
			return "only when something is deleted: sync, or deletions mirror"
		}},
	{key: "versioning.kind", label: "Versions", choices: []string{"none", "trash", "simple", "staggered"}, def: "trash",
		hint: "what replaced and deleted files leave behind: none, all for a time, the last few, or thinned out"},
	{key: "versioning.keep", label: "Keep for", def: "30d", hint: "trash and staggered: how long, e.g. 30d",
		needs: whenIs("versioning.kind", "with trash or staggered versions", "trash", "staggered")},
	{key: "versioning.count", label: "Keep last", number: true, def: "-", hint: "simple: how many versions",
		needs: whenIs("versioning.kind", "with simple versions", "simple")},
	{key: "versioning.size_cap", label: "Size cap", def: "none", hint: "the most the versions may take, e.g. 10GB",
		needs: whenIs("versioning.kind", "while versions are kept", "trash", "simple", "staggered")},
	{key: "ignore", label: "Ignore", list: true, def: "nothing", hint: "patterns, e.g. *.tmp, node_modules/"},
	{key: "bandwidth.up", label: "Upload", def: "unlimited", hint: "e.g. 20MB/s"},
	{key: "bandwidth.down", label: "Download", def: "unlimited", hint: "e.g. 50MB/s"},
	{key: "notify.errors", label: "Tell errors", choices: []string{"on", "off"}, def: "on", hint: "desktop notifications"},
	{key: "notify.conflicts", label: "Tell conflicts", choices: []string{"on", "off"}, def: "on", hint: "desktop notifications"},
	{key: "notify.completed", label: "Tell done", choices: []string{"on", "off"}, def: "on, off for real-time",
		hint: "desktop notifications: a run that changed something"},
	{key: "notify.share", label: "Tell share", choices: []string{"on", "off"}, def: "on",
		hint: "desktop notifications: a share lost or back"},
}

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
	for _, s := range settings {
		f.rows = append(f.rows, s.key)
		f.values[s.key] = rawText(lookup(raw, s.key))
		if s.choices == nil {
			f.inputs[s.key] = newInput(f.values[s.key])
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

// lookup finds "group.key" in a raw table.
func lookup(raw map[string]any, key string) any {
	group, sub, ok := strings.Cut(key, ".")
	if !ok {
		return raw[key]
	}
	g, _ := raw[group].(map[string]any)
	return g[sub]
}

// rawText is a raw value as typed: lists joined with commas ("none" for an
// empty one), bools as on and off.
func rawText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case bool:
		return map[bool]string{true: "on", false: "off"}[v]
	case []any:
		if len(v) == 0 {
			return "none"
		}
		parts := make([]string, 0, len(v))
		for _, x := range v {
			parts = append(parts, fmt.Sprint(x))
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

func settingOf(key string) *setting {
	i := slices.IndexFunc(settings, func(s setting) bool { return s.key == key })
	if i < 0 {
		return nil
	}
	return &settings[i]
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
	if v := rawText(lookup(f.base, key)); v != "" {
		return v
	}
	return defaults[key]
}

// off says why the row for key does not apply as the form stands, "" when
// it does: an interval without the schedule trigger, say. Such a row is
// shown greyed out, the cursor passes it by, and it is not saved: the
// config refuses some of them (a count with trash versions) and ignores
// the rest.
func (f *settingsForm) off(key string) string {
	if s := settingOf(key); s != nil && s.needs != nil {
		return s.needs(f.effective)
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
	if s := settingOf(key); s != nil && s.choices != nil {
		// The choices, and "not set" before them.
		all := append([]string{""}, s.choices...)
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

// apply writes the form's settings into table: each one set is written, each
// one not set or greyed out is removed; versioning, bandwidth and notify as
// one inline table each.
func (f *settingsForm) apply(d *config.Doc, table []string) error {
	groups := map[string]config.Inline{}
	var order []string
	for _, s := range settings {
		text := f.value(s.key)
		if f.off(s.key) != "" {
			text = ""
		}
		var v any = text
		switch {
		case text == "":
		case s.list:
			items := []string{} // "none" is an empty list: no triggers, nothing ignored
			if text == "none" {
				v = items
				break
			}
			for _, x := range strings.Split(text, ",") {
				if x = strings.TrimSpace(x); x != "" {
					items = append(items, x)
				}
			}
			v = items
		case s.number:
			n, err := strconv.Atoi(text)
			if err != nil {
				return fmt.Errorf("%s must be a number, not %q", strings.ToLower(s.label), text)
			}
			v = n
		case s.choices != nil && (text == "on" || text == "off"):
			v = text == "on"
		}
		group, sub, inGroup := strings.Cut(s.key, ".")
		if inGroup {
			if !slices.Contains(order, group) {
				order = append(order, group)
			}
			if text != "" {
				groups[group] = append(groups[group], config.Field{Key: sub, Value: v})
			}
			continue
		}
		var err error
		if text == "" {
			err = d.Delete(table, s.key)
		} else {
			err = d.Set(table, s.key, v)
		}
		if err != nil {
			return err
		}
	}
	for _, g := range order {
		var err error
		if len(groups[g]) == 0 {
			err = d.Delete(table, g)
		} else {
			err = d.Set(table, g, groups[g])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// set reports whether the form sets anything: a table with nothing in it
// would not be written.
func (f *settingsForm) set() bool {
	return slices.ContainsFunc(settings, func(s setting) bool { return f.value(s.key) != "" })
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
	s := settingOf(key)
	if s != nil {
		label, hint = s.label, s.hint
	}
	var value string
	switch {
	case s != nil && s.choices != nil:
		value = f.values[key]
		if i == f.focus {
			value = keyStyle.Render("‹ " + f.orBase(s, value) + " ›")
		}
	default:
		value = f.inputs[key].View()
	}
	if s != nil && f.value(key) == "" && i != f.focus {
		value = faint.Render("not set: " + s.def)
		if from := rawText(lookup(f.base, key)); from != "" {
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
func (f *settingsForm) orBase(s *setting, value string) string {
	switch {
	case value != "":
		return value
	case rawText(lookup(f.base, s.key)) != "":
		return "from " + f.from + ": " + rawText(lookup(f.base, s.key))
	}
	return "not set: " + s.def
}

var errNothingSet = errors.New("set at least one setting: a profile with none would not be written")
