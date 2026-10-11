// SPDX-License-Identifier: GPL-2.0-or-later

// Package state stores what each file of a shuttle looked like on both sides
// when it was last synced. It is the common ancestor of a two-way sync: it is
// what tells "deleted here" apart from "new over there", and "changed on both
// sides" apart from "changed on one".
//
// Crash safety rests on one rule the engine must follow: a record is written
// only after the file operation it describes has completed. A record may
// therefore be missing (a crash lost it), but never ahead of the files. A
// missing record looks like a file present on both sides with no history,
// which the engine resolves by comparing the two copies, never by deleting.
// So records can be committed in batches rather than one fsync per file.
//
// This package must stay free of desktop-specific imports; see the
// portability rule in SPEC.md.
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"

	"github.com/fqazzazee/shuttle/internal/fsys"
)

// schemaVersion is bumped whenever the stored format changes incompatibly.
const schemaVersion = 1

var (
	bucketFiles = []byte("files")
	bucketMeta  = []byte("meta")

	keySchema       = []byte("schema")
	keyLastComplete = []byte("last_complete")
)

var (
	// ErrLocked means another process has the shuttle's state open.
	ErrLocked = errors.New("state database is in use by another shuttle process")
	// ErrNewerSchema means the database was written by a newer shuttle.
	ErrNewerSchema = errors.New("state database was written by a newer version of shuttle")
)

// lockTimeout is how long Open waits for another process to let go.
var lockTimeout = time.Second

// Side is one side of a file as it was when last synced.
type Side struct {
	Size    int64
	ModTime time.Time
}

// Record is the last synced state of one path.
type Record struct {
	Path   string    // relative, slash-separated; never "."
	Kind   fsys.Kind // fsys.KindFile or fsys.KindDir
	Local  Side
	Remote Side
	Hash   []byte    // SHA-256 of the content when synced; nil if not known
	Synced time.Time // when the record was written
}

// stored is Record's on-disk form. Times are Unix nanoseconds, so they round
// trip exactly and carry no time zone. They are pointers because 0 is a real
// modification time (the epoch, used by reproducible builds), distinct from
// "not set".
type stored struct {
	Kind    fsys.Kind `json:"k"`
	LSize   int64     `json:"ls,omitempty"`
	LMod    *int64    `json:"lm,omitempty"`
	RSize   int64     `json:"rs,omitempty"`
	RMod    *int64    `json:"rm,omitempty"`
	Hash    []byte    `json:"h,omitempty"`
	SyncedN *int64    `json:"t,omitempty"`
}

// DB is one shuttle's state database. It is safe for concurrent use.
type DB struct {
	db *bolt.DB
}

// Open opens or creates the state database at path. Only one process may
// have a given database open; a second Open fails with ErrLocked.
func Open(path string) (*DB, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: lockTimeout})
	if errors.Is(err, bolterrors.ErrTimeout) {
		return nil, fmt.Errorf("%s: %w", path, ErrLocked)
	}
	if err != nil {
		return nil, fmt.Errorf("open state %s: %w", path, err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(bucketMeta)
		if err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketFiles); err != nil {
			return err
		}
		v := meta.Get(keySchema)
		if v == nil {
			return meta.Put(keySchema, []byte(strconv.Itoa(schemaVersion)))
		}
		n, err := strconv.Atoi(string(v))
		if err != nil {
			return fmt.Errorf("unreadable schema version %q", v)
		}
		if n > schemaVersion {
			return fmt.Errorf("%w (schema %d, this version reads %d)", ErrNewerSchema, n, schemaVersion)
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open state %s: %w", path, err)
	}
	return &DB{db: db}, nil
}

// Close closes the database and releases its lock.
func (d *DB) Close() error {
	return d.db.Close()
}

// Get returns the record for path, and whether there is one.
func (d *DB) Get(path string) (Record, bool, error) {
	var (
		r     Record
		found bool
	)
	err := d.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketFiles).Get([]byte(path))
		if v == nil {
			return nil
		}
		found = true
		var err error
		r, err = decode(path, v)
		return err
	})
	return r, found && err == nil, err
}

// All returns every record, keyed by path.
func (d *DB) All() (map[string]Record, error) {
	out := map[string]Record{}
	err := d.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketFiles).ForEach(func(k, v []byte) error {
			r, err := decode(string(k), v)
			if err != nil {
				return err
			}
			out[r.Path] = r
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Update runs fn in a single read-write transaction. Everything fn writes is
// committed together, or not at all if fn returns an error.
func (d *DB) Update(fn func(tx *Tx) error) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		return fn(&Tx{files: tx.Bucket(bucketFiles)})
	})
}

// Put stores one record in its own transaction.
func (d *DB) Put(r *Record) error {
	return d.Update(func(tx *Tx) error { return tx.Put(r) })
}

// Delete removes the record for path in its own transaction.
func (d *DB) Delete(path string) error {
	return d.Update(func(tx *Tx) error { return tx.Delete(path) })
}

// LastComplete returns when the last full run finished, or the zero time if
// none has. Zero means the next sync is a first sync.
func (d *DB) LastComplete() (time.Time, error) {
	var t time.Time
	err := d.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketMeta).Get(keyLastComplete)
		if v == nil {
			return nil
		}
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return fmt.Errorf("unreadable last_complete %q", v)
		}
		t = time.Unix(0, n)
		return nil
	})
	return t, err
}

// SetLastComplete records that a full run finished at t.
func (d *DB) SetLastComplete(t time.Time) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMeta).Put(keyLastComplete, []byte(strconv.FormatInt(t.UnixNano(), 10)))
	})
}

// Tx is a read-write transaction from DB.Update.
type Tx struct {
	files *bolt.Bucket
}

// Put stores r, replacing any record for the same path.
func (t *Tx) Put(r *Record) error {
	if r.Path == "." || !fsys.ValidPath(r.Path) {
		return fmt.Errorf("state: record path %q: %w", r.Path, fsys.ErrInvalidPath)
	}
	if r.Kind != fsys.KindFile && r.Kind != fsys.KindDir {
		return fmt.Errorf("state: record %q has kind %v; only files and dirs are synced", r.Path, r.Kind)
	}
	v, err := json.Marshal(stored{
		Kind:  r.Kind,
		LSize: r.Local.Size, LMod: unixNano(r.Local.ModTime),
		RSize: r.Remote.Size, RMod: unixNano(r.Remote.ModTime),
		Hash: r.Hash, SyncedN: unixNano(r.Synced),
	})
	if err != nil {
		return err
	}
	return t.files.Put([]byte(r.Path), v)
}

// Delete removes the record for path. A missing record is not an error.
func (t *Tx) Delete(path string) error {
	return t.files.Delete([]byte(path))
}

// DeleteTree removes the record for path and every record below it.
func (t *Tx) DeleteTree(path string) error {
	if err := t.files.Delete([]byte(path)); err != nil {
		return err
	}
	// Keys are sorted bytewise, so everything under "dir/" is contiguous.
	prefix := []byte(path + "/")
	c := t.files.Cursor()
	for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Seek(prefix) {
		if err := c.Delete(); err != nil {
			return err
		}
	}
	return nil
}

func decode(path string, v []byte) (Record, error) {
	var s stored
	if err := json.Unmarshal(v, &s); err != nil {
		return Record{}, fmt.Errorf("state: corrupt record for %q: %w", path, err)
	}
	return Record{
		Path:   path,
		Kind:   s.Kind,
		Local:  Side{Size: s.LSize, ModTime: fromUnixNano(s.LMod)},
		Remote: Side{Size: s.RSize, ModTime: fromUnixNano(s.RMod)},
		Hash:   s.Hash,
		Synced: fromUnixNano(s.SyncedN),
	}, nil
}

// unixNano maps the zero time to nil, so an unset time round trips as unset.
func unixNano(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	n := t.UnixNano()
	return &n
}

func fromUnixNano(n *int64) time.Time {
	if n == nil {
		return time.Time{}
	}
	return time.Unix(0, *n)
}
