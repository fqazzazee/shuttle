// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package activity keeps a shuttle's activity log: what its runs did, file by
// file, and what went wrong. Each shuttle has a file of its own, one JSON
// object per line, appended as things happen and trimmed to the retention
// limits. Anyone can read it, with or without the daemon.
//
// This package is part of the portable core: no desktop dependencies.
package activity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Kind is what an entry is about.
type Kind string

const (
	KindRun      Kind = "run"      // a run ended; Text sums it up
	KindDone     Kind = "done"     // a file or folder was copied, created or deleted
	KindSkipped  Kind = "skipped"  // left for the next run: it changed during this one
	KindConflict Kind = "conflict" // Text says what was done about it
	KindHeld     Kind = "held"     // deletions the deletion guard held back
	KindLeftOut  Kind = "left-out" // a name the other side cannot hold: never synced
	KindError    Kind = "error"
)

// Entry is one line of the log.
type Entry struct {
	Time    time.Time `json:"time"`
	Shuttle string    `json:"-"` // set by the reader, from the log's name
	Kind    Kind      `json:"kind"`
	Path    string    `json:"path,omitempty"`
	Text    string    `json:"text"`
}

// The log keeps entries for MaxAge, and at most MaxEntries of them:
// whichever keeps less.
const (
	MaxAge     = 30 * 24 * time.Hour
	MaxEntries = 10_000
)

// Log is a shuttle's activity log, open for adding entries.
type Log struct {
	mu   sync.Mutex
	path string
	f    *os.File
	n    int // entries in the file
	max  int // MaxEntries; smaller in tests
}

// Open opens the log at path, creating it if needed.
func Open(path string) (*Log, error) {
	l := &Log{path: path, max: MaxEntries}
	entries, err := read(path)
	if err != nil {
		return nil, err
	}
	l.n = len(entries)
	if err := l.open(); err != nil {
		return nil, err
	}
	// A line cut short by a crash is ended, so the next entry starts on a
	// line of its own.
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 && b[len(b)-1] != '\n' { //nolint:gosec // the daemon's own state file
		if _, err := l.f.Write([]byte{'\n'}); err != nil {
			l.f.Close()
			return nil, err
		}
	}
	return l, nil
}

func (l *Log) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	l.f = f
	return nil
}

// Close closes the log.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

// Add appends e. A log that has grown to twice its limit within a run, as
// a first sync of many files can make it, is trimmed at once.
func (l *Log) Add(e *Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.f.Write(append(line, '\n')); err != nil {
		return err
	}
	l.n++
	if l.n >= 2*l.max {
		return l.trim(time.Time{})
	}
	return nil
}

// Trim drops the entries older than MaxAge before now, and the oldest
// beyond MaxEntries. It rewrites the file only when there is something to
// drop.
func (l *Log) Trim(now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.trim(now.Add(-MaxAge))
}

func (l *Log) trim(before time.Time) error {
	entries, err := read(l.path)
	if err != nil {
		return err
	}
	keep := entries
	for len(keep) > 0 && keep[0].Time.Before(before) {
		keep = keep[1:]
	}
	if len(keep) > l.max {
		keep = keep[len(keep)-l.max:]
	}
	if len(keep) == len(entries) {
		return nil
	}
	var buf bytes.Buffer
	for i := range keep {
		line, err := json.Marshal(&keep[i])
		if err != nil {
			return err
		}
		buf.Write(append(line, '\n'))
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".activity.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone after the rename; cleans up after a failure
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	// Closed first: Windows does not replace a file that is open. Opened
	// again either way, so a failed rename leaves the old log to add to.
	l.f.Close()
	if err := os.Rename(tmp.Name(), l.path); err != nil {
		return errors.Join(err, l.open())
	}
	l.n = len(keep)
	return l.open()
}

// Read returns the newest entries of the log at path, newest first, at most
// limit of them (0: all). A log that does not exist is empty. shuttle names
// the entries.
func Read(path, shuttle string, limit int) ([]Entry, error) {
	entries, err := read(path)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	slices.Reverse(entries)
	for i := range entries {
		entries[i].Shuttle = shuttle
	}
	return entries, nil
}

// read returns every entry of the log at path, oldest first. A line that
// does not parse, as the last one can after a crash, is skipped.
func read(path string) ([]Entry, error) {
	f, err := os.Open(path) //nolint:gosec // the daemon's own state file
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Merge merges logs read with Read into one, newest first, at most limit
// entries (0: all).
func Merge(limit int, logs ...[]Entry) []Entry {
	all := slices.Concat(logs...)
	slices.SortStableFunc(all, func(a, b Entry) int { return b.Time.Compare(a.Time) })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}
