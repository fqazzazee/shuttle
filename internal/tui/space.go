// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/present"
)

// disksMsg is how much room the folders on this computer have, by folder.
type disksMsg map[string]fsys.Space

// diskDirs are the folders on this computer whose room the Shares tab
// shows: the shuttles' folders here, the home for when there are none or
// none is there, and the folders of mounted shares.
func (m *model) diskDirs() []string {
	dirs := []string{m.o.Home}
	if m.cfg == nil {
		return dirs
	}
	for i := range m.cfg.Shuttles {
		dirs = append(dirs, m.cfg.Shuttles[i].Local)
	}
	for name := range m.cfg.Shares {
		if sh := m.cfg.Shares[name]; sh.Type == config.SharePath {
			dirs = append(dirs, sh.Path)
		}
	}
	slices.Sort(dirs)
	return slices.Compact(dirs)
}

// measureDisks asks how much room the folders here have, away from the
// screen: a folder on a mount that stopped answering blocks. Only one
// asking is out at a time, so a blocked one does not pile up others.
func (m *model) measureDisks() tea.Cmd {
	if m.measuring {
		return nil
	}
	m.measuring = true
	dirs := m.diskDirs()
	return func() tea.Msg {
		out := disksMsg{}
		for _, d := range dirs {
			if sp, err := fsys.LocalSpace(d); err == nil {
				out[d] = sp
			}
		}
		return out
	}
}

// shareSpace is how much room a share has, as last known: measured here
// for a mounted folder; otherwise from its last test or the last run of a
// shuttle on it, whichever is later. Zero when not known.
func (m *model) shareSpace(name string) fsys.Space {
	sh := m.cfg.Shares[name]
	if sh.Type == config.SharePath {
		return m.disks[sh.Path]
	}
	var sp fsys.Space
	var at time.Time
	if r, ok := m.tested[name]; ok && r.err == nil && r.space.Total > 0 {
		sp, at = r.space, r.at
	}
	for i := range m.shuttles {
		st := &m.shuttles[i]
		s := m.configShuttle(st.Name)
		if s != nil && s.Share == name && st.RemoteSpace.Total > 0 && st.LastRun.After(at) {
			sp, at = st.RemoteSpace, st.LastRun
		}
	}
	return sp
}

// hereLine is how much room this computer has where the shuttles keep
// their folders: one disk, or each with the shuttles on it.
func (m *model) hereLine() string {
	type disk struct {
		space fsys.Space
		names []string
	}
	var disks []disk
	add := func(sp fsys.Space, name string) {
		// Two folders with the same room, to the byte, are on one disk.
		i := slices.IndexFunc(disks, func(d disk) bool { return d.space == sp })
		if i < 0 {
			disks = append(disks, disk{space: sp})
			i = len(disks) - 1
		}
		if name != "" {
			disks[i].names = append(disks[i].names, name)
		}
	}
	if m.cfg != nil {
		for i := range m.cfg.Shuttles {
			if sp, ok := m.disks[m.cfg.Shuttles[i].Local]; ok {
				add(sp, m.cfg.Shuttles[i].Name)
			}
		}
	}
	if len(disks) == 0 {
		sp, ok := m.disks[m.o.Home]
		if !ok {
			return ""
		}
		add(sp, "")
	}
	parts := make([]string, len(disks))
	for i, d := range disks {
		parts[i] = present.Room(d.space)
		if len(disks) > 1 {
			parts[i] += " (" + strings.Join(d.names, ", ") + ")"
		}
		if present.Low(d.space) {
			parts[i] = heldStyle.Render(parts[i])
		}
	}
	return "This computer: " + strings.Join(parts, faint.Render(" · "))
}

// freeText is room for the Shares tab's FREE column: "1.2 TB of 4.0 TB",
// amber when less than a tenth is left; "—" when not known.
func freeText(sp fsys.Space) string {
	if sp.Total <= 0 {
		return "—"
	}
	s := present.Size(sp.Free) + " of " + present.Size(sp.Total)
	if present.Low(sp) {
		return heldStyle.Render(s)
	}
	return s
}
