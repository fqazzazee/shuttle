// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package notice

import (
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

func TestOf(t *testing.T) {
	conflicts := []engine.Issue{
		{Path: "a.txt", Outcome: "held"},
		{Path: "b.txt", Outcome: "kept both: the nas copy is now b (nas).txt"},
		{Path: "c.txt", Outcome: "held"},
		{Path: "d.txt", Outcome: "held"},
	}
	for _, tc := range []struct {
		e                              daemon.Event
		key, kind, summary, body, open string
	}{
		{daemon.Event{Kind: daemon.EventError, Shuttle: "docs", Error: "2 paths could not be synced",
			Errors: []engine.Issue{{Path: "x", Reason: "permission denied"}, {Path: "y", Reason: "busy"}}},
			"error/docs", Errors, "docs: sync failed", "2 paths could not be synced\nx: permission denied\ny: busy", ""},
		{daemon.Event{Kind: daemon.EventConflicts, Shuttle: "docs", Conflicts: conflicts},
			"", Conflicts, "docs: 4 conflicts", "a.txt: waiting for you\nb.txt: kept both: the nas copy is now b (nas).txt\nc.txt: waiting for you\nand 1 more", "conflicts"},
		{daemon.Event{Kind: daemon.EventConflicts, Shuttle: "docs", Conflicts: conflicts[1:2]},
			"", Conflicts, "docs: 1 conflict", "b.txt: kept both", ""},
		{daemon.Event{Kind: daemon.EventDeletionsHeld, Shuttle: "docs", HeldDeletions: 15},
			"deletions/docs", Conflicts, "docs: 15 deletions held back", "More files would be deleted", "conflicts"},
		{daemon.Event{Kind: daemon.EventCompleted, Shuttle: "docs", Copied: 3, Deleted: 1},
			"completed/docs", Completed, "docs: synced", "Copied 3 files and deleted 1.", ""},
		{daemon.Event{Kind: daemon.EventCompleted, Shuttle: "docs", Copied: 1},
			"completed/docs", Completed, "docs: synced", "Copied 1 file.", ""},
		{daemon.Event{Kind: daemon.EventCompleted, Shuttle: "docs"},
			"completed/docs", Completed, "docs: synced", "Nothing to do: already in step.", ""},
		{daemon.Event{Kind: daemon.EventShareLost, Share: "nas", Shuttles: []string{"docs", "music", "photos"}},
			"share/nas", Share, "Share nas lost", "Waiting for it: docs, music and photos.", ""},
		{daemon.Event{Kind: daemon.EventConfigError, Error: `shuttles.docs.mode: "both" must be one of push, pull, sync`},
			"config", Errors, "shuttle: config not loaded", "shuttles.docs.mode: \"both\" must be one of push, pull, sync\nThe previous settings stay in use", ""},
		{daemon.Event{Kind: daemon.EventShareConnected, Share: "nas", Shuttles: []string{"docs"}},
			"share/nas", Share, "Share nas connected", "Can sync again: docs.", ""},
	} {
		t.Run(string(tc.e.Kind), func(t *testing.T) {
			n := Of(&tc.e)
			if n.Key != tc.key || n.Kind != tc.kind || n.Summary != tc.summary || !strings.HasPrefix(n.Body, tc.body) || n.Open != tc.open {
				t.Errorf("Of = %+v\nwant key %q, kind %q, summary %q, body %q, open %q", n, tc.key, tc.kind, tc.summary, tc.body, tc.open)
			}
		})
	}
}

func TestClipKeepsTheFileName(t *testing.T) {
	long := strings.Repeat("deep/", 40) + "report.odt"
	if got := clip(long); len([]rune(got)) != 80 || !strings.HasSuffix(got, "/report.odt") || !strings.HasPrefix(got, "…") {
		t.Errorf("clip = %q", got)
	}
}
