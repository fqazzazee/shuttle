// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
)

// The tabs, in order: keys 1 to 5 choose them.
const (
	tabShuttles = iota
	tabConflicts
	tabActivity
	tabProfiles
	tabShares
)

var tabNames = []string{"Shuttles", "Conflicts", "Activity", "Profiles", "Shares"}

type model struct {
	ctx   context.Context
	c     Client
	o     Options
	now   func() time.Time
	every time.Duration // between refreshes; 0 in tests, which refresh by hand

	width, height int
	tab           int
	help          bool
	helpScroll    int // the first line of the help shown

	loaded    bool  // the daemon has answered once
	down      error // why the daemon cannot be reached; nil when it can
	shuttles  []daemon.Status
	selected  string // the selected shuttle, by name: it stays put as the list changes
	pausedAll bool
	configErr string
	frame     int    // the spinners' frame
	spinning  bool   // a spinMsg is on its way
	message   string // what the last action did, or why it failed
	failed    bool
	messageAt time.Time

	conflictSel string          // the selected row of the Conflicts tab, by item key
	resolving   map[string]bool // rows resolved, until they are gone from the list
	question    string          // a question waiting for y or n
	then        func() tea.Cmd  // what y does

	filter      string           // the Activity tab's shuttle; "" for all
	entries     []activity.Entry // newest first; nil until the daemon answers
	scroll      int              // the first entry shown
	activityErr error

	// The Shares tab: the config file as read, and the form when open.
	cfgLoaded bool
	cfgData   []byte
	cfg       *config.Config
	cfgErr    error
	shareSel  string
	form      *shareForm
	tested    map[string]testResult // each share's last test
	disks     disksMsg              // the room of the folders here, by folder
	measuring bool                  // disks are being asked

	// The Profiles tab.
	profileSel string
	settings   *settingsForm

	rates map[string][]rateSample // each running shuttle's, for its sparkline

	wizard  *wizard              // the shuttle editor, on the Shuttles tab
	changes <-chan daemon.Status // nil until subscribed
}

func newModel(ctx context.Context, c Client, o Options) *model {
	return &model{ctx: ctx, c: c, o: o, tab: o.Tab, now: time.Now, every: refreshEvery, width: 80, height: 24,
		resolving: map[string]bool{}, tested: map[string]testResult{}, rates: map[string][]rateSample{}}
}

// Messages.
type (
	refreshedMsg struct {
		shuttles  []daemon.Status
		pausedAll bool
		configErr string
		err       error
	}
	subscribedMsg struct {
		changes <-chan daemon.Status
		err     error
	}
	changedMsg daemon.Status
	tickMsg    struct{}
	spinMsg    struct{}
	doneMsg    struct {
		text string
		err  error
	}
)

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), m.subscribe(), m.tick(), m.loadConfig(), m.toTab(m.tab))
}

func (m *model) call() (context.Context, context.CancelFunc) {
	return context.WithTimeout(m.ctx, callTimeout)
}

func (m *model) refresh() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.call()
		defer cancel()
		all, err := m.c.Shuttles(ctx)
		if err != nil {
			return refreshedMsg{err: err}
		}
		_, paused, err := m.c.State(ctx)
		if err != nil {
			return refreshedMsg{err: err}
		}
		msg, err := m.c.ConfigError(ctx)
		return refreshedMsg{shuttles: all, pausedAll: paused, configErr: msg, err: err}
	}
}

func (m *model) subscribe() tea.Cmd {
	return func() tea.Msg {
		ch, err := m.c.Changes(m.ctx)
		return subscribedMsg{ch, err}
	}
}

// next waits for the next status change.
func next(ch <-chan daemon.Status) tea.Cmd {
	return func() tea.Msg {
		st, ok := <-ch
		if !ok {
			return nil
		}
		return changedMsg(st)
	}
}

func (m *model) tick() tea.Cmd {
	if m.every == 0 {
		return nil
	}
	return tea.Tick(m.every, func(time.Time) tea.Msg { return tickMsg{} })
}

// spin turns the spinners while a shuttle shows one, and blinks the bus's
// lights, slower, while one runs and the bus shows; it stops when nothing
// moves. A run that only copies shows a bar, which needs no turning.
func (m *model) spin() tea.Cmd {
	if m.spinning || m.every == 0 {
		return nil
	}
	for i := range m.shuttles {
		if spins(&m.shuttles[i].Progress) {
			m.spinning = true
			return tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{} })
		}
	}
	if m.tall() && m.running() {
		m.spinning = true
		// On the clock's half seconds, as the lights turn: every tick
		// turns them.
		return tea.Every(blinkEvery, func(time.Time) tea.Msg { return spinMsg{} })
	}
	return nil
}

// do runs an action against the daemon and reports it as done.
func (m *model) do(text string, f func(ctx context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.call()
		defer cancel()
		return doneMsg{text, f(ctx)}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case refreshedMsg:
		before := m.itemIndex()
		defer m.listChanged(before)
		m.loaded = true
		if msg.err != nil {
			m.down, m.shuttles = msg.err, nil
			break
		}
		m.down = nil
		m.shuttles, m.pausedAll, m.configErr = msg.shuttles, msg.pausedAll, msg.configErr
		// The signals' samples come first; a refresh fills in without them.
		for i := range m.shuttles {
			m.sample(&m.shuttles[i], 2*sampleEvery)
		}
		m.keepSelection()
		cmd := m.spin()
		return m, cmd
	case subscribedMsg:
		if msg.err == nil {
			m.changes = msg.changes
			return m, next(m.changes)
		}
	case changedMsg:
		before := m.itemIndex()
		defer m.listChanged(before)
		st := daemon.Status(msg)
		m.changed(&st)
		return m, tea.Batch(next(m.changes), m.spin())
	case spinMsg:
		m.spinning = false
		m.frame++
		cmd := m.spin()
		return m, cmd
	case tickMsg:
		cmds := []tea.Cmd{m.refresh(), m.tick(), m.measureDisks()}
		if m.tab == tabActivity {
			cmds = append(cmds, m.loadActivity())
		}
		if m.form == nil && m.settings == nil && m.wizard == nil {
			cmds = append(cmds, m.loadConfig())
		}
		if m.changes == nil {
			cmds = append(cmds, m.subscribe())
		}
		return m, tea.Batch(cmds...)
	case activityMsg:
		m.gotActivity(msg)
	case configMsg:
		m.gotConfig(msg)
		cmd := m.measureDisks()
		return m, cmd
	case disksMsg:
		m.disks, m.measuring = msg, false
	case openedMsg:
		cmd := m.opened(msg)
		return m, cmd
	case treeListedMsg:
		if w := m.wizard; w != nil {
			// Each tree takes only its own listings.
			for _, t := range []*tree{w.remote, w.local} {
				if t != nil {
					t.listed(msg)
				}
			}
		}
	case testedMsg:
		m.gotTest(msg)
	case savedMsg:
		cmd := m.gotSave(&msg)
		return m, cmd
	case tea.PasteMsg:
		var in *textinput.Model
		switch {
		case m.form != nil && m.tab == tabShares && m.form.busy == "":
			in = m.form.inputs[m.form.focus]
		case m.settings != nil && m.tab == tabProfiles && m.settings.busy == "":
			in = m.settings.inputs[m.settings.rows[m.settings.focus]]
		case m.wizard != nil && m.tab == tabShuttles && m.wizard.busy == "":
			in = m.wizard.input()
		}
		if in != nil {
			updated, cmd := in.Update(msg)
			*in = updated
			return m, cmd
		}
	case resolvedMsg:
		m.say(msg.text, msg.err != nil)
		if msg.err != nil {
			delete(m.resolving, msg.key)
			m.message = errorText(msg.err)
		}
		cmd := m.refresh()
		return m, cmd
	case doneMsg:
		m.say(msg.text, msg.err != nil)
		if msg.err != nil {
			m.message = errorText(msg.err)
		}
		cmd := m.refresh()
		return m, cmd
	case tea.KeyPressMsg:
		if m.form != nil && m.tab == tabShares && !m.help && m.question == "" && msg.String() != "ctrl+c" {
			cmd := m.formKey(msg)
			return m, cmd
		}
		if m.settings != nil && m.tab == tabProfiles && !m.help && m.question == "" && msg.String() != "ctrl+c" {
			cmd := m.settingsKey(msg)
			return m, cmd
		}
		if m.wizard != nil && m.tab == tabShuttles && !m.help && m.question == "" && msg.String() != "ctrl+c" {
			cmd := m.wizardKey(msg)
			return m, cmd
		}
		cmd := m.key(msg.String())
		return m, cmd
	}
	return m, nil
}

// changed puts a status from a signal in place, or adds a new shuttle.
func (m *model) changed(st *daemon.Status) {
	m.sample(st, sampleEvery/2)
	i := slices.IndexFunc(m.shuttles, func(s daemon.Status) bool { return s.Name == st.Name })
	if i >= 0 {
		m.shuttles[i] = *st
		return
	}
	m.shuttles = append(m.shuttles, *st)
	slices.SortFunc(m.shuttles, func(a, b daemon.Status) int { return strings.Compare(a.Name, b.Name) })
	m.keepSelection()
}

// listChanged keeps the selections where they belong after the list of
// shuttles changed.
func (m *model) listChanged(itemBefore int) {
	m.keepSelection()
	m.keepItemSelection(itemBefore)
	m.forgetResolved()
}

// keepSelection keeps the selected shuttle selected, or selects the first.
func (m *model) keepSelection() {
	if m.index() < 0 && len(m.shuttles) > 0 {
		m.selected = m.shuttles[0].Name
	}
}

// index is where the selected shuttle is in the list; -1 if nowhere.
func (m *model) index() int {
	return slices.IndexFunc(m.shuttles, func(s daemon.Status) bool { return s.Name == m.selected })
}

func (m *model) current() *daemon.Status {
	if i := m.index(); i >= 0 {
		return &m.shuttles[i]
	}
	return nil
}

func (m *model) key(k string) tea.Cmd {
	if m.question != "" && k != "ctrl+c" {
		return m.answer(k)
	}
	switch k {
	case "ctrl+c", "q":
		return tea.Quit
	case "?":
		m.help, m.helpScroll = !m.help, 0
		return nil
	case "esc":
		m.help = false
		return nil
	case "1", "2", "3", "4", "5":
		return m.toTab(int(k[0] - '1'))
	case "tab":
		return m.toTab((m.tab + 1) % len(tabNames))
	case "shift+tab":
		return m.toTab((m.tab + len(tabNames) - 1) % len(tabNames))
	case "P":
		paused := !m.pausedAll
		return m.do(map[bool]string{true: "paused all shuttles", false: "resumed all shuttles"}[paused],
			func(ctx context.Context) error { return m.c.SetPaused(ctx, "", paused) })
	}
	switch {
	case m.help:
		m.helpKey(k)
	case m.tab == tabShuttles:
		return m.shuttlesKey(k)
	case m.tab == tabConflicts:
		return m.conflictsKey(k)
	case m.tab == tabActivity:
		return m.activityKey(k)
	case m.tab == tabShares:
		return m.sharesKey(k)
	case m.tab == tabProfiles:
		return m.profilesKey(k)
	}
	return nil
}

// toTab shows tab t, and asks for what it shows if it is not kept current.
func (m *model) toTab(t int) tea.Cmd {
	m.tab, m.help = t, false
	switch t {
	case tabActivity:
		return m.loadActivity()
	case tabShares, tabProfiles:
		return m.loadConfig()
	}
	return nil
}

func (m *model) shuttlesKey(k string) tea.Cmd {
	switch k {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "home", "g":
		m.move(-len(m.shuttles))
	case "end", "G":
		m.move(len(m.shuttles))
	case "n":
		return m.openWizard(nil)
	}
	st := m.current()
	if st == nil {
		return nil
	}
	name := st.Name
	switch k {
	case "s":
		return m.do("syncing "+name, func(ctx context.Context) error { return m.c.SyncNow(ctx, name) })
	case "p":
		if m.pausedAll {
			m.say("All shuttles are paused: P resumes them.", true)
			return nil
		}
		paused := !st.Paused
		return m.do(map[bool]string{true: "paused ", false: "resumed "}[paused]+name,
			func(ctx context.Context) error { return m.c.SetPaused(ctx, name, paused) })
	case "e", "enter":
		if sh := m.configShuttle(name); sh != nil {
			return m.openWizard(sh)
		}
		m.say(name+" is not in the config file as it is now", true)
	case "x":
		m.ask("Delete the shuttle "+name+" from the config file? Its files stay where they are, on both sides.", func() tea.Cmd {
			return m.saveConfig("deleted the shuttle "+name, func(d *config.Doc) error {
				return d.DeleteTable([]string{"shuttles", name})
			})
		})
	case "d":
		on := st.State == daemon.StateDisabled
		return m.do(map[bool]string{true: "enabled ", false: "disabled "}[on]+name,
			func(ctx context.Context) error { return m.c.SetEnabled(ctx, name, on) })
	}
	return nil
}

// messageFor is how long the last action's message shows.
const messageFor = 5 * time.Second

func (m *model) say(text string, failed bool) {
	m.message, m.failed, m.messageAt = text, failed, m.now()
}

// configShuttle is the named shuttle as the config file has it.
func (m *model) configShuttle(name string) *config.Shuttle {
	if m.cfg == nil || m.cfgErr != nil {
		return nil
	}
	i := slices.IndexFunc(m.cfg.Shuttles, func(s config.Shuttle) bool { return s.Name == name })
	if i < 0 {
		return nil
	}
	return &m.cfg.Shuttles[i]
}

func (m *model) move(by int) {
	if len(m.shuttles) == 0 {
		return
	}
	i := min(max(m.index()+by, 0), len(m.shuttles)-1)
	m.selected = m.shuttles[i].Name
}

// errorText is an error as the TUI shows it: one line.
func errorText(err error) string {
	s := err.Error()
	if errors.Is(err, daemon.ErrConfig) {
		s = "the config file: " + s
	}
	return firstLine(s)
}
