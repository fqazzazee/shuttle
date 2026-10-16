// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package activity

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func open(t *testing.T) (*Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.activity")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, path
}

func add(t *testing.T, l *Log, at time.Time, kind Kind, path, text string) {
	t.Helper()
	if err := l.Add(&Entry{Time: at, Kind: kind, Path: path, Text: text}); err != nil {
		t.Fatal(err)
	}
}

func paths(es []Entry) string {
	s := ""
	for _, e := range es {
		s += e.Path + " "
	}
	return s
}

func TestAddAndRead(t *testing.T) {
	l, path := open(t)
	add(t, l, t0, KindDone, "a", "copied to the share")
	add(t, l, t0.Add(time.Second), KindError, "b", "could not copy: denied")
	add(t, l, t0.Add(2*time.Second), KindRun, "", "copied 1, 1 error")

	got, err := Read(path, "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Kind != KindRun || got[2].Path != "a" || got[1].Text != "could not copy: denied" {
		t.Errorf("entries = %+v, want newest first", got)
	}
	if got[0].Shuttle != "s" || !got[2].Time.Equal(t0) {
		t.Errorf("entry = %+v", got[2])
	}
	if got, _ := Read(path, "s", 2); paths(got) != " b " {
		t.Errorf("limit 2: %q, want the run and b", paths(got))
	}
	// Windows has no mode bits to check: the log is the user's by the
	// ACL of the folder it is in.
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
	if got, err := Read(filepath.Join(t.TempDir(), "none"), "x", 0); err != nil || len(got) != 0 {
		t.Errorf("a log that does not exist: %v, %v", got, err)
	}
}

// A line cut short by a crash is skipped, and the log carries on.
func TestTornLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.activity")
	if err := os.WriteFile(path, []byte(`{"time":"2026-10-01T12:00:00Z","kind":"done","path":"a","text":"x"}`+"\n"+`{"time":"2026-10-01T12:00:01Z","ki`), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	add(t, l, t0.Add(2*time.Second), KindDone, "b", "x")
	add(t, l, t0.Add(3*time.Second), KindDone, "c", "x")
	got, err := Read(path, "s", 0)
	if err != nil || paths(got) != "c b a " {
		t.Errorf("entries %q, %v; want c, b and a", paths(got), err)
	}
}

func TestTrimByAge(t *testing.T) {
	l, path := open(t)
	add(t, l, t0, KindDone, "old", "x")
	add(t, l, t0.Add(MaxAge-time.Hour), KindDone, "young", "x")
	add(t, l, t0.Add(MaxAge), KindDone, "new", "x")
	before := stat(path)
	if err := l.Trim(t0.Add(MaxAge + time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(path, "s", 0); paths(got) != "new young " {
		t.Errorf("after trimming: %q", paths(got))
	}
	// Still open for adding, to the new file.
	add(t, l, t0.Add(MaxAge+time.Hour), KindDone, "later", "x")
	if got, _ := Read(path, "s", 0); paths(got) != "later new young " {
		t.Errorf("after adding: %q", paths(got))
	}
	if after, _ := os.Stat(path); os.SameFile(before, after) {
		t.Error("the file was not rewritten")
	}
	// Nothing to drop: the file stays as it is.
	before = stat(path)
	if err := l.Trim(t0.Add(MaxAge + time.Hour)); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(path); !os.SameFile(before, after) {
		t.Error("the file was rewritten with nothing to drop")
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("left behind: %v", entries)
	}
}

func TestTrimByCount(t *testing.T) {
	l, path := open(t)
	l.max = 5
	for i := range 7 {
		add(t, l, t0.Add(time.Duration(i)*time.Second), KindDone, fmt.Sprint(i), "x")
	}
	if err := l.Trim(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(path, "s", 0); paths(got) != "6 5 4 3 2 " {
		t.Errorf("after trimming: %q", paths(got))
	}
	// Within a run, the log may grow to twice its limit, then is trimmed.
	for i := 7; i < 15; i++ {
		add(t, l, t0.Add(time.Duration(i)*time.Second), KindDone, fmt.Sprint(i), "x")
	}
	if got, _ := Read(path, "s", 0); len(got) > 2*l.max || got[0].Path != "14" {
		t.Errorf("after growing: %d entries, %q", len(got), paths(got))
	}
}

// A log opened again counts what it already holds.
func TestReopen(t *testing.T) {
	l, path := open(t)
	l.Close()
	for i := range 3 {
		l, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if l.n != i {
			t.Errorf("opened with %d entries, counted %d", i, l.n)
		}
		add(t, l, t0, KindDone, "x", "x")
		l.Close()
	}
}

func TestMerge(t *testing.T) {
	a := []Entry{{Time: t0.Add(3 * time.Second), Path: "a3"}, {Time: t0, Path: "a0"}}
	b := []Entry{{Time: t0.Add(2 * time.Second), Path: "b2"}, {Time: t0.Add(time.Second), Path: "b1"}}
	if got := paths(Merge(0, a, b)); got != "a3 b2 b1 a0 " {
		t.Errorf("merged %q", got)
	}
	if got := paths(Merge(2, a, b)); got != "a3 b2 " {
		t.Errorf("merged, limit 2: %q", got)
	}
}

// stat is path's FileInfo with its identity read now: on Windows,
// os.SameFile reads it only when first asked, by path, which after a
// rewrite finds the new file.
func stat(path string) os.FileInfo {
	fi, _ := os.Stat(path)
	os.SameFile(fi, fi)
	return fi
}
