// SPDX-License-Identifier: GPL-2.0-or-later

package state

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/fqazzazee/shuttle/internal/fsys"
)

func init() {
	// Keep the lock test quick.
	lockTimeout = 50 * time.Millisecond
}

func openTemp(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shuttle.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func file(path string, size int64) *Record {
	return &Record{
		Path:   path,
		Kind:   fsys.KindFile,
		Local:  Side{Size: size, ModTime: time.Unix(1_700_000_000, 123_456_789)},
		Remote: Side{Size: size, ModTime: time.Unix(1_700_000_000, 0)},
		Hash:   bytes.Repeat([]byte{0xab}, 32),
		Synced: time.Unix(1_700_000_100, 42),
	}
}

func recordsEqual(a, b *Record) bool {
	return a.Path == b.Path && a.Kind == b.Kind &&
		a.Local.Size == b.Local.Size && a.Local.ModTime.Equal(b.Local.ModTime) &&
		a.Remote.Size == b.Remote.Size && a.Remote.ModTime.Equal(b.Remote.ModTime) &&
		bytes.Equal(a.Hash, b.Hash) && a.Synced.Equal(b.Synced)
}

func TestPutGetRoundTrip(t *testing.T) {
	db, _ := openTemp(t)
	want := file("dir/report.pdf", 4096)
	if err := db.Put(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := db.Get("dir/report.pdf")
	if err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if !recordsEqual(&got, want) {
		t.Errorf("got  %+v\nwant %+v", got, *want)
	}
	if _, ok, err := db.Get("missing"); ok || err != nil {
		t.Errorf("Get(missing) = %v, %v; want not found, no error", ok, err)
	}
}

// The epoch is a real modification time and must not read back as "unset",
// or every such file would look changed on every run.
func TestEpochAndZeroTimesStayDistinct(t *testing.T) {
	db, _ := openTemp(t)
	epoch := time.Unix(0, 0)
	r := &Record{Path: "built", Kind: fsys.KindFile, Local: Side{ModTime: epoch}}
	if err := db.Put(r); err != nil {
		t.Fatal(err)
	}
	got, _, err := db.Get("built")
	if err != nil {
		t.Fatal(err)
	}
	if got.Local.ModTime.IsZero() || !got.Local.ModTime.Equal(epoch) {
		t.Errorf("local mtime = %v, want the epoch", got.Local.ModTime)
	}
	if !got.Remote.ModTime.IsZero() || !got.Synced.IsZero() {
		t.Errorf("unset times came back as %v / %v, want zero", got.Remote.ModTime, got.Synced)
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	db, path := openTemp(t)
	if err := db.Put(file("a", 1)); err != nil {
		t.Fatal(err)
	}
	done := time.Unix(1_700_000_200, 7)
	if err := db.SetLastComplete(done); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if _, ok, _ := db2.Get("a"); !ok {
		t.Error("record lost across reopen")
	}
	if got, _ := db2.LastComplete(); !got.Equal(done) {
		t.Errorf("LastComplete = %v, want %v", got, done)
	}
}

func TestLastCompleteZeroOnFreshDB(t *testing.T) {
	db, _ := openTemp(t)
	got, err := db.LastComplete()
	if err != nil || !got.IsZero() {
		t.Errorf("LastComplete = %v, %v; want zero time (first sync)", got, err)
	}
}

func TestAll(t *testing.T) {
	db, _ := openTemp(t)
	for _, p := range []string{"b", "a", "a/x"} {
		if err := db.Put(file(p, 1)); err != nil {
			t.Fatal(err)
		}
	}
	all, err := db.All()
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(all)); !slices.Equal(got, []string{"a", "a/x", "b"}) {
		t.Errorf("All keys = %v", got)
	}
	if all["a/x"].Path != "a/x" {
		t.Errorf("record Path = %q, want it filled from the key", all["a/x"].Path)
	}
}

func TestDeleteTreeStopsAtPrefixBoundary(t *testing.T) {
	db, _ := openTemp(t)
	// "a-b" and "a.txt" sort between "a" and "a/..." bytewise, and "ab" after;
	// none of them is under a/.
	for _, p := range []string{"a", "a/1", "a/sub/2", "a-b", "a.txt", "ab", "b"} {
		r := file(p, 1)
		if p == "a" || p == "a/sub" {
			r.Kind = fsys.KindDir
		}
		if err := db.Put(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Update(func(tx *Tx) error { return tx.DeleteTree("a") }); err != nil {
		t.Fatal(err)
	}
	all, err := db.All()
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(all)); !slices.Equal(got, []string{"a-b", "a.txt", "ab", "b"}) {
		t.Errorf("after DeleteTree(a): %v", got)
	}
}

func TestUpdateIsAtomic(t *testing.T) {
	db, _ := openTemp(t)
	if err := db.Put(file("keep", 1)); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err := db.Update(func(tx *Tx) error {
		if err := tx.Put(file("new", 1)); err != nil {
			return err
		}
		if err := tx.Delete("keep"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Update error = %v, want boom", err)
	}
	if _, ok, _ := db.Get("new"); ok {
		t.Error("a write from a failed transaction was committed")
	}
	if _, ok, _ := db.Get("keep"); !ok {
		t.Error("a delete from a failed transaction was committed")
	}
}

func TestPutRejectsBadRecords(t *testing.T) {
	db, _ := openTemp(t)
	for _, r := range []*Record{
		{Path: "../escape", Kind: fsys.KindFile},
		{Path: ".", Kind: fsys.KindDir},
		{Path: "", Kind: fsys.KindFile},
		{Path: "link", Kind: fsys.KindOther},
	} {
		if err := db.Put(r); err == nil {
			t.Errorf("Put(%q, %v) succeeded", r.Path, r.Kind)
		}
	}
}

func TestSecondOpenIsLocked(t *testing.T) {
	_, path := openTemp(t)
	if _, err := Open(path); !errors.Is(err, ErrLocked) {
		t.Errorf("second Open error = %v, want ErrLocked", err)
	}
}

func TestNewerSchemaRefused(t *testing.T) {
	db, path := openTemp(t)
	err := db.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMeta).Put(keySchema, []byte(strconv.Itoa(schemaVersion+1)))
	})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, err := Open(path); !errors.Is(err, ErrNewerSchema) {
		t.Errorf("Open error = %v, want ErrNewerSchema", err)
	}
}

func TestCorruptRecordIsAnError(t *testing.T) {
	db, _ := openTemp(t)
	err := db.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketFiles).Put([]byte("bad"), []byte("{not json"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.All(); err == nil {
		t.Error("All succeeded over a corrupt record; a silently skipped record would look like a deletion")
	}
	if _, _, err := db.Get("bad"); err == nil {
		t.Error("Get succeeded on a corrupt record")
	}
}

func TestNotADatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.db")
	if err := writeFile(path, bytes.Repeat([]byte("x"), 8192)); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("Open of a non-database file succeeded")
	}
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// Per-file commits fsync once per record; the engine batches instead (see the
// package comment for why that is safe). These show the difference.
func BenchmarkPutOnePerTx(b *testing.B) {
	db, err := Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	for i := 0; b.Loop(); i++ {
		if err := db.Put(file("dir/file-"+strconv.Itoa(i), 1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPutBatched1000(b *testing.B) {
	db, err := Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	i := 0
	for b.Loop() {
		err := db.Update(func(tx *Tx) error {
			for range 1000 {
				i++
				if err := tx.Put(file("dir/file-"+strconv.Itoa(i), 1)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(i), "ns/record")
}

func TestBind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Put(&Record{Path: "a.txt", Kind: fsys.KindFile}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetLastComplete(time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		pair      string
		wantReset bool
	}{
		{"one", false}, // a database from before pairs: kept as is
		{"one", false},
		{"two", true},
		{"two", false},
	} {
		reset, err := db.Bind(step.pair)
		if err != nil || reset != step.wantReset {
			t.Fatalf("Bind(%q) = %v, %v; want reset %v", step.pair, reset, err, step.wantReset)
		}
	}
	all, err := db.All()
	if err != nil || len(all) != 0 {
		t.Errorf("after a reset: records %v, %v; want none", all, err)
	}
	if last, _ := db.LastComplete(); !last.IsZero() {
		t.Errorf("after a reset: last complete %v, want none (a first sync)", last)
	}
}
