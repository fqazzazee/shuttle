// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

const commented = `# My shuttles. Edited by hand, and by the TUI.

[shares.nas]
type = "smb"   # the Synology
host = "nas.example"
share = "home"
user = "user"

# Things that sync all the time.
[profiles.live]
mode = "sync"
triggers = ["realtime"]

[shuttles.docs]
profile = "live"
local = "~/Documents"   # not the Nextcloud one
remote = "nas:/docs"

# Photos go one way.
[shuttles.photos]
mode = "push"
local = "~/Pictures"
remote = "nas:/photos"
`

func edit(t *testing.T, doc string, ops ...func(*Doc) error) string {
	t.Helper()
	d, err := ParseDoc([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if err := op(d); err != nil {
			t.Fatal(err)
		}
	}
	return string(d.Bytes())
}

func set(table, key string, v any) func(*Doc) error {
	return func(d *Doc) error { return d.Set(strings.Split(table, "/"), key, v) }
}

func del(table, key string) func(*Doc) error {
	return func(d *Doc) error { return d.Delete(strings.Split(table, "/"), key) }
}

func delTable(table string) func(*Doc) error {
	return func(d *Doc) error { return d.DeleteTable(strings.Split(table, "/")) }
}

// The edits the TUI makes, on a hand-written file: exactly the changed
// lines change.
func TestEdits(t *testing.T) {
	for _, tc := range []struct {
		name string
		ops  []func(*Doc) error
		old  string // replaced in commented by...
		new  string // ...this, giving the expected result
	}{
		{"change a value, keeping its comment", []func(*Doc) error{set("shuttles/docs", "local", "~/Docs")},
			`local = "~/Documents"   # not the Nextcloud one`, `local = "~/Docs"   # not the Nextcloud one`},
		{"add a key after the table's keys", []func(*Doc) error{set("shuttles/docs", "enabled", false)},
			"remote = \"nas:/docs\"\n", "remote = \"nas:/docs\"\nenabled = false\n"},
		{"add an inline table", []func(*Doc) error{set("shuttles/photos", "versioning", Inline{{"kind", "trash"}, {"keep", "30d"}})},
			"remote = \"nas:/photos\"\n", "remote = \"nas:/photos\"\nversioning = { kind = \"trash\", keep = \"30d\" }\n"},
		{"remove an override", []func(*Doc) error{del("shuttles/photos", "mode")},
			"mode = \"push\"\nlocal = \"~/Pictures\"", "local = \"~/Pictures\""},
		{"remove a table and its comment", []func(*Doc) error{delTable("shuttles/photos")},
			"\n# Photos go one way.\n[shuttles.photos]\nmode = \"push\"\nlocal = \"~/Pictures\"\nremote = \"nas:/photos\"\n", ""},
		{"remove a table in the middle", []func(*Doc) error{delTable("profiles/live")},
			"# Things that sync all the time.\n[profiles.live]\nmode = \"sync\"\ntriggers = [\"realtime\"]\n\n", ""},
		{"new table after its siblings", []func(*Doc) error{set("shares/usb", "type", "path"), set("shares/usb", "path", "/run/media/usb")},
			"user = \"user\"\n\n", "user = \"user\"\n\n[shares.usb]\ntype = \"path\"\npath = \"/run/media/usb\"\n\n"},
		{"new table with no siblings goes last", []func(*Doc) error{set("global/bandwidth", "up", "1MB/s")},
			"remote = \"nas:/photos\"\n", "remote = \"nas:/photos\"\n\n[global.bandwidth]\nup = \"1MB/s\"\n"},
		{"names that need quoting", []func(*Doc) error{set("shuttles/my.docs", "mode", "pull")},
			"remote = \"nas:/photos\"\n", "remote = \"nas:/photos\"\n\n[shuttles.\"my.docs\"]\nmode = \"pull\"\n"},
		{"strings are escaped", []func(*Doc) error{set("shuttles/docs", "local", "C:\\x \"y\"\n\x01\t")},
			`local = "~/Documents"`, `local = "C:\\x \"y\"\n\u0001\t"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(commented, tc.old) {
				t.Fatalf("test bug: %q not in the document", tc.old)
			}
			want := strings.Replace(commented, tc.old, tc.new, 1)
			if got := edit(t, commented, tc.ops...); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// Keys defined other ways than plain key = value lines.
func TestOtherDefinitions(t *testing.T) {
	for _, tc := range []struct {
		name, doc, want string
		ops             []func(*Doc) error
	}{
		{"a sub-table becomes a key", "[shuttles.d]\nmode = \"push\"\n\n# why\n[shuttles.d.notify]\nerrors = false\n\n[shares.x]\ntype = \"path\"\n",
			"[shuttles.d]\nmode = \"push\"\nnotify = { completed = true }\n\n[shares.x]\ntype = \"path\"\n",
			[]func(*Doc) error{set("shuttles/d", "notify", Inline{{"completed", true}})}},
		{"dotted keys are replaced in place", "[shuttles.d]\nnotify.errors = false # quiet\nnotify.share = false\nmode = \"push\"\n",
			"[shuttles.d]\nnotify = { completed = true }\nmode = \"push\"\n",
			[]func(*Doc) error{set("shuttles/d", "notify", Inline{{"completed", true}})}},
		{"a table made by dotted keys gets dotted keys", "[shuttles]\nd.mode = \"push\"\nd.local = \"~/d\"\n",
			"[shuttles]\nd.mode = \"push\"\nd.local = \"~/d\"\nd.remote = \"x:\"\n",
			[]func(*Doc) error{set("shuttles/d", "remote", "x:")}},
		{"a dotted value changes in place", "[shuttles]\nd.mode = \"push\" # was pull\n",
			"[shuttles]\nd.mode = \"sync\" # was pull\n",
			[]func(*Doc) error{set("shuttles/d", "mode", "sync")}},
		{"an inline table becomes a section", "[shuttles]\nd = { mode = \"push\", local = \"~/d\" }\n\n[shares.x]\ntype = \"path\"\n",
			"[shares.x]\ntype = \"path\"\n\n[shuttles.d]\nlocal = \"~/d\"\nmode = \"sync\"\n",
			[]func(*Doc) error{set("shuttles/d", "mode", "sync")}},
		{"CRLF stays CRLF", "[shuttles.d]\r\nmode = \"push\"\r\n",
			"[shuttles.d]\r\nmode = \"push\"\r\nlocal = \"~/d\"\r\n\r\n[shares.x]\r\ntype = \"path\"\r\n",
			[]func(*Doc) error{set("shuttles/d", "local", "~/d"), set("shares/x", "type", "path")}},
		{"no newline at the end", "[shuttles.d]\nmode = \"push\"",
			"[shuttles.d]\nmode = \"push\"\nlocal = \"~/d\"\n",
			[]func(*Doc) error{set("shuttles/d", "local", "~/d")}},
		{"an empty file", "", "[shuttles.d]\nmode = \"push\"\n",
			[]func(*Doc) error{set("shuttles/d", "mode", "push")}},
		{"a whole table replaced by a value", "[shuttles.d]\nmode = \"push\"\n[shuttles.e]\nmode = \"pull\"\n",
			"[shuttles.e]\nmode = \"pull\"\n\n[shuttles]\nd = \"oops\"\n",
			[]func(*Doc) error{set("shuttles", "d", "oops")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := edit(t, tc.doc, tc.ops...); got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

// docStyles are the same kind of config written in different ways, for the
// randomised test.
var docStyles = map[string]string{
	"commented": commented,
	"inline":    "[shuttles]\ndocs = { mode = \"push\", local = \"~/d\", notify = { errors = false } }\nphotos = { mode = \"pull\" }\n",
	"dotted":    "shares.nas.type = \"smb\"\n[shuttles]\ndocs.mode = \"push\" # c\ndocs.notify.errors = false\n",
	"subtables": "[shuttles.docs]\nmode = \"push\"\n\n[shuttles.docs.notify]\nerrors = false\n\n[shuttles.docs.versioning]\nkind = \"trash\"\n\n# end\n",
	"crlf":      strings.ReplaceAll(commented, "\n", "\r\n"),
	"noeol":     "[shuttles.docs]\nmode = \"push\"",
	"empty":     "",
}

// TestRandomEdits applies random edits to each style and checks every
// result against a model: the document decoded to maps, edited the same
// way. Whatever the layout, the TOML stays valid and means what was asked.
func TestRandomEdits(t *testing.T) {
	tables := [][]string{{"shuttles", "docs"}, {"shuttles", "photos"}, {"shuttles", "my.docs"}, {"shares", "nas"},
		{"shuttles", "docs", "notify"}, {"profiles", "live"}, {"shuttles"}}
	keys := []string{"mode", "local", "notify", "versioning", "enabled", "weird key", "errors", "docs"}
	values := []any{"push", "~/d", true, false, 3, []string{"*.tmp", ".git/"},
		Inline{{"kind", "trash"}, {"keep", "30d"}}, Inline{{"completed", false}}, Inline{}}
	for name, doc := range docStyles {
		t.Run(name, func(t *testing.T) {
			for seed := range uint64(40) {
				r := rand.New(rand.NewPCG(seed, 7))
				d, err := ParseDoc([]byte(doc))
				if err != nil {
					t.Fatal(err)
				}
				model := decode(t, d.Bytes())
				var steps []string
				for range 12 {
					table := tables[r.IntN(len(tables))]
					key := keys[r.IntN(len(keys))]
					var step string
					var opErr, modelErr error
					switch r.IntN(4) {
					case 0, 1:
						v := values[r.IntN(len(values))]
						step = fmt.Sprintf("Set(%q, %q, %v)", table, key, v)
						opErr, modelErr = d.Set(table, key, v), modelSet(model, table, key, v)
					case 2:
						step = fmt.Sprintf("Delete(%q, %q)", table, key)
						opErr, modelErr = d.Delete(table, key), modelDelete(model, append(slices.Clone(table), key))
					case 3:
						step = fmt.Sprintf("DeleteTable(%q)", table)
						opErr, modelErr = d.DeleteTable(table), modelDelete(model, table)
					}
					steps = append(steps, step)
					if (opErr != nil) != (modelErr != nil) {
						t.Fatalf("seed %d: %s: error %v, model says %v\nsteps: %s\ndoc:\n%s", seed, step, opErr, modelErr, strings.Join(steps, "; "), d.Bytes())
					}
					if got := decode(t, d.Bytes()); !reflect.DeepEqual(prune(got), prune(model)) {
						t.Fatalf("seed %d: after %s\ngot   %v\nmodel %v\nsteps: %s\ndoc:\n%s", seed, step, got, model, strings.Join(steps, "; "), d.Bytes())
					}
				}
			}
		})
	}
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid TOML: %v\n%s", err, b)
	}
	return m
}

var errNotTable = errors.New("not a table")

// walk returns the map at path, creating missing tables when create is set.
func walk(m map[string]any, path []string, create bool) (map[string]any, error) {
	for _, p := range path {
		next, ok := m[p]
		if !ok {
			if !create {
				return nil, nil
			}
			next = map[string]any{}
			m[p] = next
		}
		sub, ok := next.(map[string]any)
		if !ok {
			return nil, errNotTable
		}
		m = sub
	}
	return m, nil
}

func modelSet(m map[string]any, table []string, key string, v any) error {
	// Fail before changing anything, as Doc does.
	if _, err := walk(m, table, false); err != nil {
		return err
	}
	t, _ := walk(m, table, true)
	t[key] = normalize(v)
	return nil
}

func modelDelete(m map[string]any, path []string) error {
	parent, err := walk(m, path[:len(path)-1], false)
	if err != nil {
		return err
	}
	if parent != nil {
		delete(parent, path[len(path)-1])
	}
	return nil
}

// prune drops empty tables, which TOML does not keep apart from missing
// ones: deleting the last [profiles.x] leaves no profiles table at all.
func prune(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			if sub = prune(sub); len(sub) == 0 {
				continue
			}
			v = sub
		}
		out[k] = v
	}
	return out
}

// normalize is v as toml.Unmarshal would decode it.
func normalize(v any) any {
	switch v := v.(type) {
	case int:
		return int64(v)
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out
	case Inline:
		out := map[string]any{}
		for _, f := range v {
			out[f.Key] = normalize(f.Value)
		}
		return out
	}
	return v
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shuttle", "config.toml")
	good := []byte("[shares.m]\ntype = \"path\"\npath = \"/mnt\"\n")

	if err := Save(path, nil, good, dir); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	if err := Save(path, nil, good, dir); !errors.Is(err, ErrChanged) {
		t.Errorf("save over a file that appeared: %v, want ErrChanged", err)
	}
	if err := Save(path, []byte("something else"), good, dir); !errors.Is(err, ErrChanged) {
		t.Errorf("save over a changed file: %v, want ErrChanged", err)
	}
	bad := []byte("[shuttles.s]\nmode = \"both\"\n")
	if err := Save(path, good, bad, dir); err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Errorf("saving an invalid config: %v, want the config error", err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, good) {
		t.Error("a refused save changed the file")
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	better := append(slices.Clone(good), "user = \"x\"\n"...)
	if err := Save(path, good, better, dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, better) {
		t.Errorf("file = %q", b)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want the file's own 0640 kept", info.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("left behind: %v", entries)
	}
}

// Every config in testdata survives being read and written back unchanged.
func TestTestdataRoundTrips(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.toml")
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		d, err := ParseDoc(b)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !bytes.Equal(d.Bytes(), b) {
			t.Errorf("%s changed by a parse alone", f)
		}
	}
}
