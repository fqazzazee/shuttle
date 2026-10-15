// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package mobile

import "testing"

// The wizard's marks, as the TUI's tree makes them: a folder marked, a file
// in it unmarked, which marks the rest of the folder instead.
func TestPicker(t *testing.T) {
	p := NewPicker("/top")
	for dir, json := range map[string]string{
		"/top":   `[{"Name":"a","Dir":true},{"Name":"b","Dir":true},{"Name":"z.txt","Dir":false}]`,
		"/top/a": `[{"Name":"c.txt","Dir":false},{"Name":"d.txt","Dir":false}]`,
	} {
		if err := p.Listed(dir, json); err != nil {
			t.Fatal(err)
		}
	}
	if p.Synced("/top/a") != SyncedAll || p.MarksJSON() != "[]" {
		t.Errorf("syncing everything: %d, %s", p.Synced("/top/a"), p.MarksJSON())
	}
	p.SetMode(PickInclude)
	if p.Synced("/top/a") != SyncedNone {
		t.Errorf("only what is marked, nothing marked: a is %d", p.Synced("/top/a"))
	}
	p.Toggle("/top/a")
	p.Toggle("/top/b")
	p.Toggle("/top/a/c.txt")
	if got := p.MarksJSON(); got != `["a/d.txt","b/"]` {
		t.Errorf("marks = %s", got)
	}
	if p.Synced("/top/a") != SyncedSome || p.Synced("/top/a/d.txt") != SyncedAll || p.Synced("/top/z.txt") != SyncedNone {
		t.Errorf("a %d, a/d.txt %d, z.txt %d", p.Synced("/top/a"), p.Synced("/top/a/d.txt"), p.Synced("/top/z.txt"))
	}
	if got := p.SelectionJSON(); got != `{"Include":["a/d.txt","b"],"Exclude":null}` {
		t.Errorf("selection = %s", got)
	}
	p.SetMode(PickExclude)
	if got := p.SelectionJSON(); got != `{"Include":null,"Exclude":["a/d.txt","b"]}` || p.Mode() != PickExclude {
		t.Errorf("the same marks, left out: %s", got)
	}

	// Another folder: the marks are kept, and do not count there.
	p.SetRoot("/top/a")
	if got := p.MarksJSON(); got != `["d.txt"]` || p.Root() != "/top/a" {
		t.Errorf("marks inside a = %s", got)
	}
	if err := p.Listed("/x", "not json"); err == nil {
		t.Error("a listing that is not JSON taken")
	}
}
