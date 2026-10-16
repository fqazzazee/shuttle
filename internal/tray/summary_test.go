// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tray

import (
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/daemon"
)

func TestSummary(t *testing.T) {
	st := func(name string, s daemon.State) daemon.Status { return daemon.Status{Name: name, State: s} }
	failed := st("photos", daemon.StateError)
	failed.Error = "share nas: unreachable"
	for _, tt := range []struct {
		name      string
		running   bool
		all       []daemon.Status
		pausedAll bool
		configErr string
		look      look
		words     string
	}{
		{"no daemon", false, nil, false, "", lookStopped, "the daemon is not running"},
		{"none yet", true, nil, false, "", lookIdle, "no shuttles yet"},
		{"idle", true, []daemon.Status{st("docs", daemon.StateIdle)}, false, "", lookIdle, "up to date"},
		{"syncing", true, []daemon.Status{st("docs", daemon.StateRunning), st("music", daemon.StateIdle)}, false, "",
			lookSyncing, "syncing docs"},
		{"all paused", true, []daemon.Status{st("docs", daemon.StatePaused)}, true, "", lookPaused, "paused"},
		{"one paused", true, []daemon.Status{st("docs", daemon.StatePaused), st("music", daemon.StateIdle)}, false, "",
			lookIdle, "some shuttles paused"},
		{"a failed run", true, []daemon.Status{st("docs", daemon.StateRunning), failed}, false, "",
			lookError, "photos: share nas: unreachable"},
		{"a broken config", true, []daemon.Status{st("docs", daemon.StateIdle)}, false, "line 3: bad\nmore",
			lookError, "does not load: line 3: bad"},
	} {
		l, words := summary(tt.running, tt.all, tt.pausedAll, tt.configErr)
		if l != tt.look || !strings.Contains(words, tt.words) || strings.Contains(words, "\n") || len([]rune(words)) > tipMax {
			t.Errorf("%s: %d %q; want %d and %q", tt.name, l, words, tt.look, tt.words)
		}
	}
}
