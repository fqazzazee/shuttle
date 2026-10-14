// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package notify

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/dbustest"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/notify/notifytest"
)

func TestMessage(t *testing.T) {
	conflicts := []engine.Issue{
		{Path: "a.txt", Outcome: "held"},
		{Path: "b.txt", Outcome: "kept both: the nas copy is now b (nas).txt"},
		{Path: "c.txt", Outcome: "held"},
		{Path: "d.txt", Outcome: "held"},
	}
	for _, tc := range []struct {
		e                      daemon.Event
		key, summary, body, ct string
	}{
		{daemon.Event{Kind: daemon.EventError, Shuttle: "docs", Error: "2 paths could not be synced",
			Errors: []engine.Issue{{Path: "x", Reason: "permission denied"}, {Path: "y", Reason: "busy"}}},
			"error/docs", "docs: sync failed", "2 paths could not be synced\nx: permission denied\ny: busy", "transfer.error"},
		{daemon.Event{Kind: daemon.EventConflicts, Shuttle: "docs", Conflicts: conflicts},
			"", "docs: 4 conflicts", "a.txt: waiting for you\nb.txt: kept both: the nas copy is now b (nas).txt\nc.txt: waiting for you\nand 1 more", "transfer"},
		{daemon.Event{Kind: daemon.EventDeletionsHeld, Shuttle: "docs", HeldDeletions: 15},
			"deletions/docs", "docs: 15 deletions held back", "More files would be deleted", "transfer"},
		{daemon.Event{Kind: daemon.EventCompleted, Shuttle: "docs", Copied: 3, Deleted: 1},
			"completed/docs", "docs: synced", "Copied 3 files and deleted 1.", "transfer.complete"},
		{daemon.Event{Kind: daemon.EventCompleted, Shuttle: "docs", Copied: 1},
			"completed/docs", "docs: synced", "Copied 1 file.", "transfer.complete"},
		{daemon.Event{Kind: daemon.EventCompleted, Shuttle: "docs"},
			"completed/docs", "docs: synced", "Nothing to do: already in step.", "transfer.complete"},
		{daemon.Event{Kind: daemon.EventShareLost, Share: "nas", Shuttles: []string{"docs", "music", "photos"}},
			"share/nas", "Share nas lost", "Waiting for it: docs, music and photos.", "network.disconnected"},
		{daemon.Event{Kind: daemon.EventConfigError, Error: `shuttles.docs.mode: "both" must be one of push, pull, sync`},
			"config", "shuttle: config not loaded", "shuttles.docs.mode: \"both\" must be one of push, pull, sync\nThe previous settings stay in use", "transfer.error"},
		{daemon.Event{Kind: daemon.EventShareConnected, Share: "nas", Shuttles: []string{"docs"}},
			"share/nas", "Share nas connected", "Can sync again: docs.", "network.connected"},
	} {
		t.Run(string(tc.e.Kind), func(t *testing.T) {
			m := message(&tc.e)
			if m.key != tc.key || m.summary != tc.summary || !strings.HasPrefix(m.body, tc.body) || m.category != tc.ct || m.icon == "" {
				t.Errorf("message = %+v\nwant key %q, summary %q, body %q, category %q", m, tc.key, tc.summary, tc.body, tc.ct)
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

// syncBuffer is a log the test reads while the notifier writes it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// running starts a Notifier on a private bus and returns it with a function
// that opens more connections to that bus, and its log.
func running(t *testing.T) (*Notifier, func() *dbus.Conn, *syncBuffer) {
	t.Helper()
	connect := dbustest.Bus(t)
	log := &syncBuffer{}
	n := New(slog.New(slog.NewTextHandler(log, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.Run(ctx, connect()); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return n, connect, log
}

func waitNotes(t *testing.T, srv *notifytest.Server, n int) []notifytest.Note {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(srv.Notes()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("got %d notifications, want %d: %+v", len(srv.Notes()), n, srv.Notes())
		}
		time.Sleep(10 * time.Millisecond)
	}
	return srv.Notes()
}

func TestNotesReachTheServer(t *testing.T) {
	n, connect, _ := running(t)
	srv, err := notifytest.Serve(connect(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	n.Send(&daemon.Event{Kind: daemon.EventError, Shuttle: "docs", Error: "boom"})
	n.Send(&daemon.Event{Kind: daemon.EventError, Shuttle: "docs", Error: "boom again"})
	n.Send(&daemon.Event{Kind: daemon.EventConflicts, Shuttle: "docs", Conflicts: []engine.Issue{{Path: "a<b>&c", Outcome: "held"}}})
	notes := waitNotes(t, srv, 3)
	if a := notes[0]; a.AppName != "shuttle" || a.Summary != "docs: sync failed" || a.Body != "boom" ||
		a.Category != "transfer.error" || a.Urgency != 1 || a.ReplacesID != 0 {
		t.Errorf("first note = %+v", a)
	}
	if b := notes[1]; b.ReplacesID != notes[0].ID {
		t.Errorf("the second error replaces %d, want the first one's id %d", b.ReplacesID, notes[0].ID)
	}
	if c := notes[2]; c.ReplacesID != 0 || c.Body != "a<b>&c: waiting for you" {
		t.Errorf("conflict note = %+v; want a note of its own, unescaped for a plain-text server", c)
	}
}

// File names come from the share: for a server that reads markup, they are
// escaped.
func TestBodyIsEscapedForMarkup(t *testing.T) {
	n, connect, _ := running(t)
	srv, err := notifytest.Serve(connect(), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	n.Send(&daemon.Event{Kind: daemon.EventConflicts, Shuttle: "docs", Conflicts: []engine.Issue{{Path: "<b>x</b>&y", Outcome: "held"}}})
	if got := waitNotes(t, srv, 1)[0].Body; got != "&lt;b&gt;x&lt;/b&gt;&amp;y: waiting for you" {
		t.Errorf("body = %q", got)
	}
}

// Without a notification server the daemon carries on, says so once, and
// notifies again once a server appears.
func TestNoServer(t *testing.T) {
	n, connect, log := running(t)
	for range 3 {
		n.Send(&daemon.Event{Kind: daemon.EventCompleted, Shuttle: "docs"})
	}
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(log.String(), "cannot show desktop notifications") {
		if time.Now().After(deadline) {
			t.Fatalf("log = %q", log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	srv, err := notifytest.Serve(connect(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	n.Send(&daemon.Event{Kind: daemon.EventCompleted, Shuttle: "docs", Copied: 1})
	if got := waitNotes(t, srv, 1); got[0].Body != "Copied 1 file." {
		t.Errorf("notes = %+v", got)
	}
	if c := strings.Count(log.String(), "cannot show desktop notifications"); c != 1 {
		t.Errorf("logged the missing server %d times, want once:\n%s", c, log.String())
	}
	if !strings.Contains(log.String(), "desktop notifications work again") {
		t.Errorf("log = %q", log.String())
	}
}

// Send never blocks, even with nothing showing the queue.
func TestSendNeverBlocks(t *testing.T) {
	n := New(nil)
	done := make(chan struct{})
	go func() {
		for range queueSize * 2 {
			n.Send(&daemon.Event{Kind: daemon.EventCompleted})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked")
	}
}

// Notifications that wait for the user have an "Open shuttle" button,
// which opens the TUI on the Conflicts tab.
func TestOpenShuttleButton(t *testing.T) {
	connect := dbustest.Bus(t)
	opened := make(chan string, 4)
	n := New(nil)
	n.Open = func(tab string) { opened <- tab }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.Run(ctx, connect()); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	srv, err := notifytest.Serve(connect(), false, nil)
	if err != nil {
		t.Fatal(err)
	}

	n.Send(&daemon.Event{Kind: daemon.EventConflicts, Shuttle: "docs", Conflicts: []engine.Issue{{Path: "a", Outcome: "held"}}})
	n.Send(&daemon.Event{Kind: daemon.EventConflicts, Shuttle: "docs", Conflicts: []engine.Issue{{Path: "b", Outcome: "kept both: ..."}}})
	n.Send(&daemon.Event{Kind: daemon.EventDeletionsHeld, Shuttle: "docs", HeldDeletions: 12})
	n.Send(&daemon.Event{Kind: daemon.EventError, Shuttle: "docs", Error: "boom"})
	notes := waitNotes(t, srv, 4)
	button := []string{"open", "Open shuttle"}
	for i, want := range [][]string{button, {}, button, {}} {
		if got := notes[i].Actions; strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: actions %q, want %q", notes[i].Summary, got, want)
		}
	}

	click := func(id uint32, action string) {
		t.Helper()
		if err := srv.Invoke(id, action); err != nil {
			t.Fatal(err)
		}
	}
	click(notes[1].ID, "open")    // no button: nothing
	click(notes[0].ID, "default") // the notification itself: nothing
	click(notes[0].ID, "open")
	select {
	case tab := <-opened:
		if tab != "conflicts" {
			t.Errorf("opened %q", tab)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the button opened nothing")
	}
	click(notes[2].ID, "open")
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("held deletions' button opened nothing")
	}
	select {
	case tab := <-opened:
		t.Errorf("opened %q for a click that should open nothing", tab)
	case <-time.After(100 * time.Millisecond):
	}
}
