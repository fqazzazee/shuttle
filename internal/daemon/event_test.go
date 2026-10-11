// SPDX-License-Identifier: GPL-2.0-or-later

package daemon

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

func (f *fixture) onEvent(e *Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, *e)
}

func (f *fixture) allEvents() []Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.events)
}

// eventsOf returns the events of one kind sent so far.
func (f *fixture) eventsOf(kind EventKind) []Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Event
	for i := range f.events {
		if f.events[i].Kind == kind {
			out = append(out, f.events[i])
		}
	}
	return out
}

// syncNow runs s once and waits until the run has been reported, by which
// time its events have been sent.
func (f *fixture) syncNow() Status {
	f.t.Helper()
	f.mu.Lock()
	want := f.ended + 1
	f.mu.Unlock()
	if err := f.d.SyncNow("s"); err != nil {
		f.t.Fatal(err)
	}
	f.waitFor("s", "the run to be reported", func(Status) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.ended >= want
	})
	return f.d.Statuses()[0]
}

func TestCompletedRuns(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	f.start()
	f.syncNow()
	f.syncNow() // nothing to do, but asked for: still worth saying it is done
	got := f.eventsOf(EventCompleted)
	if len(got) != 2 || got[0].Copied != 1 || got[1].Copied != 0 {
		t.Errorf("completed events = %+v, want one for each run asked for", got)
	}
}

// Runs nobody asked for are announced only when they changed something.
func TestQuietAutomaticRunsAreNotAnnounced(t *testing.T) {
	fast(t)
	f := newFixture(t, "push", `triggers = ["schedule"]
interval = "1m"`)
	write(t, f.local, "a.txt", "a")
	f.build()
	f.d.runners["s"].shuttle.Settings.Interval = 100 * time.Millisecond
	f.run()
	f.waitFor("s", "four scheduled runs", func(Status) bool { return f.runCount() >= 4 })
	if got := f.eventsOf(EventCompleted); len(got) != 1 || got[0].Copied != 1 {
		t.Errorf("completed events = %+v, want one, for the run that copied", got)
	}
}

func TestCompletedSwitch(t *testing.T) {
	for _, tc := range []struct {
		extra string
		want  int
	}{
		{"", 0}, // off by default for real-time shuttles
		{"notify = { completed = true }", 1},
	} {
		t.Run(strconv.Quote(tc.extra), func(t *testing.T) {
			f := realtimeFixture(t, "push", tc.extra)
			write(t, f.local, "new.txt", "n")
			f.waitFor("s", "the change to be pushed", func(Status) bool { return exists(f.mount, "new.txt") })
			quiet()
			if got := f.eventsOf(EventCompleted); len(got) != tc.want {
				t.Errorf("completed events = %+v, want %d", got, tc.want)
			}
		})
	}
}

// An error that persists is announced once; after a good run, again.
func TestErrorsAreAnnouncedOnce(t *testing.T) {
	f := newFixture(t, "push", "")
	away := f.local + ".away"
	os.Rename(f.local, away)
	f.start()
	f.syncNow()
	f.syncNow()
	if got := f.eventsOf(EventError); len(got) != 1 || got[0].Shuttle != "s" || got[0].Error == "" {
		t.Fatalf("error events = %+v, want one", got)
	}
	os.Rename(away, f.local)
	if st := f.syncNow(); st.State != StateIdle {
		t.Fatalf("status = %+v", st)
	}
	os.Rename(f.local, away)
	f.syncNow()
	if got := f.eventsOf(EventError); len(got) != 2 {
		t.Errorf("error events = %+v, want a second one after the good run", got)
	}
}

func TestSwitchesOff(t *testing.T) {
	f := newFixture(t, "push", "notify = { errors = false, completed = false }")
	f.start()
	f.syncNow()
	os.Remove(f.local)
	f.syncNow()
	if len(f.allEvents()) != 0 {
		t.Errorf("events = %+v, want none with the switches off", f.allEvents())
	}
}

// conflict makes a.txt differ on both sides of a synced shuttle s.
func conflict(t *testing.T, f *fixture) {
	t.Helper()
	write(t, f.local, "a.txt", "local edit")
	write(t, f.mount, "a.txt", "remote edit!")
	later := time.Now().Add(time.Minute)
	os.Chtimes(filepath.Join(f.mount, "a.txt"), later, later)
}

// A held conflict comes back every run until resolved: one notification.
func TestHeldConflictsAreAnnouncedOnce(t *testing.T) {
	f := newFixture(t, "sync", `conflicts = "ask"`)
	write(t, f.local, "a.txt", "base")
	f.start()
	f.syncNow()
	conflict(t, f)
	f.syncNow()
	f.syncNow()
	got := f.eventsOf(EventConflicts)
	if len(got) != 1 || len(got[0].Conflicts) != 1 || got[0].Conflicts[0].Outcome != "held" {
		t.Errorf("conflict events = %+v, want one with the held conflict", got)
	}
}

func TestSettledConflictsAreAnnounced(t *testing.T) {
	f := newFixture(t, "sync", "")
	write(t, f.local, "a.txt", "base")
	f.start()
	f.syncNow()
	conflict(t, f)
	f.syncNow()
	got := f.eventsOf(EventConflicts)
	if len(got) != 1 || len(got[0].Conflicts) != 1 || got[0].Conflicts[0].Path != "a.txt" {
		t.Errorf("conflict events = %+v, want the kept-both conflict", got)
	}
}

func TestHeldDeletionsAreAnnouncedOnce(t *testing.T) {
	f := newFixture(t, "sync", "")
	for i := range 20 {
		write(t, f.local, fmt.Sprintf("f%d", i), "x")
	}
	f.start()
	f.syncNow()
	for i := range 15 { // 75%: the guard holds them
		os.Remove(filepath.Join(f.local, fmt.Sprintf("f%d", i)))
	}
	f.syncNow()
	f.syncNow()
	if got := f.eventsOf(EventDeletionsHeld); len(got) != 1 || got[0].HeldDeletions != 15 {
		t.Errorf("held deletion events = %+v, want one for 15", got)
	}
}

// twoShuttles has shuttles a and b in their own folders of one share.
func twoShuttles(t *testing.T, notify string) (*fixture, string) {
	t.Helper()
	fast(t)
	f := newFixture(t, "push", "")
	share := t.TempDir()
	doc := fmt.Sprintf("[shares.nas]\ntype = \"path\"\npath = %q\n", share)
	for _, n := range []string{"a", "b"} {
		os.Mkdir(filepath.Join(share, n), 0o755)
		doc += fmt.Sprintf("[shuttles.%s]\nmode = \"push\"\nlocal = %q\nremote = \"nas:/%s\"\n%s\n", n, t.TempDir(), n, notify)
	}
	f.writeConfig(doc)
	f.start()
	for _, n := range []string{"a", "b"} {
		f.waitFor(n, "the first probe", func(st Status) bool { return st.Share == ShareOnline })
	}
	return f, share
}

func (f *fixture) waitEvents(kind EventKind, n int) []Event {
	f.t.Helper()
	f.waitFor("a", fmt.Sprintf("%d %s events", n, kind), func(Status) bool { return len(f.eventsOf(kind)) >= n })
	return f.eventsOf(kind)
}

// A share is lost when no shuttle reaches it any more, and announced once
// for all its shuttles; seeing it at start is not news.
func TestShareEvents(t *testing.T) {
	f, share := twoShuttles(t, "")
	b := filepath.Join(share, "b")
	os.Rename(b, b+".away") // only b's folder: the share itself is still there
	f.waitFor("b", "b's folder missing", func(st Status) bool { return st.Share == ShareOffline })
	time.Sleep(4 * probeEvery)
	if len(f.allEvents()) != 0 {
		t.Fatalf("events = %+v, want none while a still reaches the share", f.allEvents())
	}

	os.Rename(share, share+".away") // the whole share
	lost := f.waitEvents(EventShareLost, 1)
	if lost[0].Share != "nas" || !slices.Equal(lost[0].Shuttles, []string{"a", "b"}) {
		t.Errorf("share lost = %+v", lost[0])
	}
	os.Rename(share+".away", share)
	f.waitEvents(EventShareConnected, 1)
	time.Sleep(4 * probeEvery)
	if got := len(f.eventsOf(EventShareLost)) + len(f.eventsOf(EventShareConnected)); got != 2 {
		t.Errorf("%d share events, want one lost and one connected", got)
	}
}

func TestShareSwitchOff(t *testing.T) {
	f, share := twoShuttles(t, "notify = { share = false }")
	os.Rename(share, share+".away")
	f.waitFor("a", "the share lost", func(st Status) bool { return st.Share == ShareOffline })
	f.waitFor("b", "the share lost", func(st Status) bool { return st.Share == ShareOffline })
	time.Sleep(4 * probeEvery)
	if len(f.allEvents()) != 0 {
		t.Errorf("events = %+v, want none with notify.share off", f.allEvents())
	}
}

// A run that cannot reach the share marks it offline, and says so, without
// waiting for the next probe.
func TestOfflineRunMarksTheShareOffline(t *testing.T) {
	fast(t)
	probeEvery = time.Hour // only the probe at start
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, "push", "")
	f.writeConfig(fmt.Sprintf(`
[shares.nas]
type = "smb"
host = "127.0.0.1"
port = %d
share = "x"

[shuttles.s]
mode = "push"
local = %q
remote = "nas:"
`, ln.Addr().(*net.TCPAddr).Port, f.local))
	f.start()
	f.waitFor("s", "the probe at start", func(st Status) bool { return st.Share == ShareOnline })
	ln.Close() // the server goes away

	if st := f.syncNow(); st.State != StateOffline || st.Share != ShareOffline {
		t.Errorf("status = %+v, want the share offline at once", st)
	}
	if got := f.eventsOf(EventShareLost); len(got) != 1 {
		t.Errorf("share lost events = %+v, want one", got)
	}
}

// Whoever sees a run over, through Statuses or OnChange, can count on its
// events having been sent: tests and the bus rely on it.
func TestARunEndsAfterItsEvents(t *testing.T) {
	f := newFixture(t, "push", "")
	f.start()
	stop := make(chan struct{})
	broken := make(chan string, 1)
	go func() {
		var seen time.Time
		for {
			select {
			case <-stop:
				return
			default:
			}
			st := f.d.Statuses()[0]
			if st.State == StateRunning || !st.LastRun.After(seen) {
				continue
			}
			seen = st.LastRun
			// Every run here is asked for, so each sends "completed".
			ended := len(f.eventsOf(EventCompleted))
			f.mu.Lock()
			runs := f.runs
			f.mu.Unlock()
			if ended < runs {
				select {
				case broken <- fmt.Sprintf("run %d seen over with %d completed events", runs, ended):
				default:
				}
			}
		}
	}()
	for range 200 {
		f.syncNow()
	}
	close(stop)
	select {
	case msg := <-broken:
		t.Error(msg)
	default:
	}
}
