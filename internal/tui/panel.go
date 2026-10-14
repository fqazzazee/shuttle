// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/present"
)

// panelLabel is the width of the panel's labels.
const panelLabel = 16

// panelMost is the most lines a wrapped value takes: an error message can
// run long, and the table above needs the room more.
const panelMost = 3

// panel is the selected shuttle in full, under the table: where its
// folders are and how much room each side has, when it runs, what its last
// run did, and the whole of what the DETAILS column cuts short. Nil
// without a selected shuttle. The folders and triggers come from the
// config file, so they are left out while it cannot be read.
func (m *model) panel() []string {
	st := m.current()
	if st == nil {
		return nil
	}
	title := titleStyle.Render(st.Name) + faint.Render("  "+present.ModeArrow(st.Mode)+" "+string(st.Mode))
	var rows [][2]string
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, [2]string{label, value})
		}
	}
	s := m.configShuttle(st.Name)
	if s != nil {
		if s.Profile != "" {
			title += faint.Render(" · profile " + s.Profile)
		}
		add("This computer", withSpace(m.homeText(s.Local), m.disks[s.Local]))
		add("The share", withSpace(s.Share+":"+remotePath(s.Remote), m.shareSpace(s.Share)))
		add("Syncs", selection(&s.Settings))
	}
	now := m.now()
	switch {
	case st.State == daemon.StateDisabled:
		add("Runs", "never: it is disabled (d turns it on)")
	case s != nil:
		add("Runs", joinNonEmpty(", ", runs(&s.Settings), nextText(st, now)))
	}
	if !st.LastRun.IsZero() {
		add("Last run", present.Ago(st.LastRun, now)+lastRunText(st))
	}
	width := max(m.width-2-panelLabel, 20)
	add("Now", present.Progress(&st.Progress))
	add("Rate", m.rateLine(st.Name, width))
	if a := present.Attention(st); a != "" {
		style := heldStyle
		if st.State == daemon.StateError || st.State == daemon.StateOffline {
			style = badStyle
		}
		add("Details", style.Render(a))
	}

	lines := []string{faint.Render(strings.Repeat("─", max(m.width, 1))), title}
	for _, r := range rows {
		wrapped := strings.Split(ansi.Wrap(r[1], width, " "), "\n")
		if len(wrapped) > panelMost {
			wrapped = append(wrapped[:panelMost-1], ansi.Truncate(wrapped[panelMost-1], width-1, "")+"…")
		}
		for i, w := range wrapped {
			label := ""
			if i == 0 {
				label = r[0]
			}
			lines = append(lines, "  "+faint.Render(pad(label, panelLabel))+w)
		}
	}
	return lines
}

// homeText is a folder here with the home as "~", as the config has it.
func (m *model) homeText(dir string) string {
	if m.o.Home == "" {
		return dir
	}
	if dir == m.o.Home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(dir, m.o.Home+"/"); ok {
		return "~/" + rest
	}
	return dir
}

// remotePath is a folder in a share as a path from its root: "/docs".
func remotePath(p string) string {
	if p == "." || p == "" {
		return "/"
	}
	return "/" + strings.TrimPrefix(p, "/")
}

// withSpace is a folder and, once known, how much room its side has.
func withSpace(dir string, sp fsys.Space) string {
	if sp.Total <= 0 {
		return dir
	}
	room := spaceText(sp)
	if low(sp) {
		room = heldStyle.Render(room)
	}
	return dir + faint.Render("  ·  ") + room
}

// selection says what of its folder a shuttle syncs.
func selection(s *config.Settings) string {
	switch {
	case len(s.Include) > 0:
		return "only " + strings.Join(s.Include, ", ")
	case len(s.Exclude) > 0:
		return "all but " + strings.Join(s.Exclude, ", ")
	}
	return "everything"
}

// runs says what starts a shuttle's runs, besides s.
func runs(s *config.Settings) string {
	if len(s.Triggers) == 0 {
		return "only when you ask (s)"
	}
	var parts []string
	for _, t := range s.Triggers {
		switch t {
		case config.TriggerRealtime:
			parts = append(parts, "as files change")
		case config.TriggerShareAvailable:
			parts = append(parts, "when the share comes online")
		case config.TriggerSchedule:
			if s.Interval > 0 {
				parts = append(parts, "every "+durationText(s.Interval))
			}
			if len(s.At) > 0 {
				at := make([]string, len(s.At))
				for i, t := range s.At {
					at[i] = t.String()
				}
				parts = append(parts, "at "+strings.Join(at, ", "))
			}
		}
	}
	return strings.Join(parts, ", ")
}

// nextText is when the schedule runs a shuttle next, unless it is running.
func nextText(st *daemon.Status, now time.Time) string {
	if st.State == daemon.StateRunning {
		return ""
	}
	if next := present.Until(st.NextRun, now); next != "" {
		return "next " + next
	}
	return ""
}

// lastRunText is what the last run did, after when it ended.
func lastRunText(st *daemon.Status) string {
	if st.Error != "" {
		return ": it failed"
	}
	if st.Copied == 0 && st.Deleted == 0 {
		return ": nothing to do"
	}
	var parts []string
	if st.Copied > 0 {
		parts = append(parts, fmt.Sprintf("%d copied", st.Copied))
	}
	if st.Deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", st.Deleted))
	}
	return ": " + strings.Join(parts, ", ")
}

// durationText is an interval as the config writes it: "90m", "1h", "2d".
func durationText(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
