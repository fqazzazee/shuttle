// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tray

import (
	"strings"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/present"
)

// summary is the icon's look and its words for the daemon's shuttles, all
// paused or not, with its config error if any; or, with running false, for
// no daemon to ask.
func summary(running bool, all []daemon.Status, pausedAll bool, configErr string) (look, string) {
	if !running {
		return lookStopped, "shuttle: the daemon is not running"
	}
	if configErr != "" {
		return lookError, "shuttle: the config file does not load: " + firstLine(configErr)
	}
	switch daemon.Overall(all, pausedAll, configErr) {
	case daemon.StateError, daemon.StateHeld, daemon.StateOffline:
		for i := range all {
			st := &all[i]
			switch st.State {
			case daemon.StateError, daemon.StateHeld, daemon.StateOffline:
				what := present.Attention(st)
				if what == "" {
					what = string(st.State)
				}
				return lookError, "shuttle: " + st.Name + ": " + firstLine(what)
			}
		}
		return lookError, "shuttle: a shuttle needs you"
	case daemon.StateRunning:
		var names []string
		for i := range all {
			if all[i].State == daemon.StateRunning {
				names = append(names, all[i].Name)
			}
		}
		return lookSyncing, "shuttle: syncing " + strings.Join(names, ", ")
	case daemon.StatePaused:
		if pausedAll {
			return lookPaused, "shuttle: paused"
		}
		return lookIdle, "shuttle: up to date; some shuttles paused"
	}
	if len(all) == 0 {
		return lookIdle, "shuttle: no shuttles yet"
	}
	return lookIdle, "shuttle: up to date"
}

// tipMax is how long a notification area icon's tooltip may be, less one.
const tipMax = 127

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > tipMax-40 {
		s = string(r[:tipMax-41]) + "…"
	}
	return s
}
