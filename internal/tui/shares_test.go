// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/present"
	"github.com/fqazzazee/shuttle/internal/secrets"
	"github.com/fqazzazee/shuttle/internal/secrets/secretservice"
)

// keyring is a Store in memory.
type keyring struct {
	mu  sync.Mutex
	pw  map[string]string
	err error // what every call returns, when set
}

func (k *keyring) Get(_ context.Context, key secrets.Key) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.err != nil {
		return "", k.err
	}
	pw, ok := k.pw[key.Account()]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return pw, nil
}

func (k *keyring) Set(_ context.Context, key secrets.Key, pw string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.err != nil {
		return k.err
	}
	k.pw[key.Account()] = pw
	return nil
}

func (k *keyring) Delete(_ context.Context, key secrets.Key) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.pw, key.Account())
	return nil
}

// tests records the shares tested: "name password" lines; a share named
// "bad" fails.
type tests struct {
	mu   sync.Mutex
	seen []string
}

func (ts *tests) test(_ context.Context, sh *config.Share, password string, _ secrets.Store) (fsys.Space, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.seen = append(ts.seen, sh.Name+" "+password)
	if sh.Name == "bad" {
		return fsys.Space{}, errors.New("share bad: dial tcp: connection refused")
	}
	return fsys.Space{Free: 1_200_000_000_000, Total: 4_000_000_000_000}, nil
}

func testOptions(t *testing.T) Options {
	dir := t.TempDir()
	return Options{Config: filepath.Join(dir, "config.toml"), Home: dir,
		Store: &keyring{pw: map[string]string{}}, Test: (&tests{}).test}
}

const sharesConfig = `# Shares and the shuttles that use them.

[shares.nas]
type = "smb"   # the Synology
host = "nas.example"
share = "home"
user = "user"

[shares.usb]
type = "path"
path = "/mnt/usb"

[shuttles.docs]
mode = "push"
local = "/home/u/Documents"
remote = "nas:/docs"
`

// sharesTab starts on the Shares tab with doc as the config file.
func sharesTab(t *testing.T, doc string) (*model, *keyring, *tests) {
	t.Helper()
	o := testOptions(t)
	ts := &tests{}
	o.Test = ts.test
	if doc != "" {
		if err := os.WriteFile(o.Config, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := newModel(context.Background(), newFake(), o)
	m.every, m.now = 0, func() time.Time { return now }
	send(t, m, tea.WindowSizeMsg{Width: 140, Height: 20})
	run(t, m, m.Init())
	press(t, m, "5")
	return m, o.Store.(*keyring), ts
}

func configFile(t *testing.T, m *model) string {
	t.Helper()
	b, err := os.ReadFile(m.o.Config)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// typeIn types text into the focused field.
func typeIn(t *testing.T, m *model, text string) {
	t.Helper()
	for _, r := range text {
		send(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestSharesList(t *testing.T) {
	m, _, _ := sharesTab(t, sharesConfig)
	s := screen(m)
	for _, want := range []string{
		"NAME  TYPE  WHERE                        USED BY",
		"nas   smb   smb://user@nas.example/home  docs",
		"usb   path  /mnt/usb                     -",
		"n new  e edit  t test  x delete",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}

func TestAddAShare(t *testing.T) {
	m, kr, ts := sharesTab(t, sharesConfig)
	press(t, m, "n")
	if s := screen(m); !strings.Contains(s, "New share") || !strings.Contains(s, "› Name") {
		t.Fatalf("form:\n%s", s)
	}
	typeIn(t, m, "media")
	press(t, m, "tab", "tab") // type: smb as it is
	typeIn(t, m, "media.example")
	press(t, m, "tab")
	typeIn(t, m, "video")
	press(t, m, "tab")
	typeIn(t, m, "user")
	press(t, m, "tab")
	typeIn(t, m, "s3cret") // the password
	if s := screen(m); strings.Contains(s, "s3cret") {
		t.Errorf("the password shows:\n%s", s)
	}

	// Tried with what is typed, before anything is saved.
	send(t, m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if got := strings.Join(ts.seen, ","); got != "media s3cret" {
		t.Errorf("tested %q", got)
	}
	if s := screen(m); !strings.Contains(s, "it works") {
		t.Errorf("test result:\n%s", s)
	}
	if strings.Contains(configFile(t, m), "media") {
		t.Error("testing wrote the config")
	}

	send(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.form != nil {
		t.Fatalf("form still open: %s", m.form.result)
	}
	want := strings.Replace(sharesConfig, "path = \"/mnt/usb\"\n",
		"path = \"/mnt/usb\"\n\n[shares.media]\ntype = \"smb\"\nhost = \"media.example\"\nshare = \"video\"\nuser = \"user\"\n", 1)
	if got := configFile(t, m); got != want {
		t.Errorf("config:\n%s\nwant:\n%s", got, want)
	}
	if kr.pw["smb://user@media.example/video"] != "s3cret" {
		t.Errorf("keyring = %v", kr.pw)
	}
	if s := screen(m); !strings.Contains(s, "saved the share media") || !strings.Contains(s, "media  smb") {
		t.Errorf("after saving:\n%s", s)
	}
	if m.shareSel != "media" {
		t.Errorf("selected %q after adding media", m.shareSel)
	}
}

// An NFS share's IDs: 0 is root and is written; empty is the user's own,
// and is not.
func TestAddAnNFSShare(t *testing.T) {
	m, kr, _ := sharesTab(t, sharesConfig)
	press(t, m, "n")
	typeIn(t, m, "store")
	press(t, m, "tab", "right", "tab") // type: nfs3
	typeIn(t, m, "store.example")
	press(t, m, "tab")
	typeIn(t, m, "/volume1/store")
	press(t, m, "tab")
	if s := screen(m); !strings.Contains(s, "› User ID") || !strings.Contains(s, "the server believes it as sent") {
		t.Errorf("the user ID field:\n%s", s)
	}
	typeIn(t, m, "-1")
	send(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !strings.Contains(screen(m), `the user id must be a number from 0 to 4294967295, not "-1"`) {
		t.Errorf("a bad user ID:\n%s", screen(m))
	}
	send(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	send(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	typeIn(t, m, "0")
	press(t, m, "tab", "tab", "tab") // past the group ID and the port
	typeIn(t, m, "892")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // the mount port is the last field: saves
	if m.form != nil {
		t.Fatalf("form still open: %s", m.form.result)
	}
	want := strings.Replace(sharesConfig, "path = \"/mnt/usb\"\n",
		"path = \"/mnt/usb\"\n\n[shares.store]\ntype = \"nfs3\"\nhost = \"store.example\"\nmount_port = 892\nexport = \"/volume1/store\"\nuid = 0\n", 1)
	if got := configFile(t, m); got != want {
		t.Errorf("config:\n%s\nwant:\n%s", got, want)
	}
	if len(kr.pw) != 0 {
		t.Errorf("an NFS share wrote the keyring: %v", kr.pw)
	}

	// Back in the form, the IDs read as they were saved.
	m.shareSel = "store"
	press(t, m, "e")
	if got := m.form.inputs["uid"].Value() + "," + m.form.inputs["gid"].Value(); got != "0," {
		t.Errorf("uid,gid in the form = %q", got)
	}
}

func TestEditAShare(t *testing.T) {
	m, kr, _ := sharesTab(t, sharesConfig)
	kr.pw["smb://user@nas.example/home"] = "old-pw"
	press(t, m, "e")
	if s := screen(m); !strings.Contains(s, "The share nas") || !strings.Contains(s, "renaming is not supported") ||
		!strings.Contains(s, "› Type") {
		t.Fatalf("form:\n%s", s)
	}
	// Another user, no password typed: the stored one goes with it.
	press(t, m, "tab", "tab", "tab")
	for range 4 {
		send(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	typeIn(t, m, "other")
	press(t, m, "tab", "tab")
	typeIn(t, m, "CORP")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // to the port, the last field
	send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // saves
	if m.form != nil {
		t.Fatalf("form still open: %s", m.form.result)
	}
	want := strings.Replace(sharesConfig, `user = "user"`, `user = "other"`+"\ndomain = \"CORP\"", 1)
	if got := configFile(t, m); got != want {
		t.Errorf("config:\n%s\nwant:\n%s", got, want)
	}
	if kr.pw["smb://CORP;other@nas.example/home"] != "old-pw" {
		t.Errorf("keyring = %v", kr.pw)
	}

	// The type changed: the old type's keys go.
	press(t, m, "e")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	send(t, m, tea.KeyPressMsg{Code: tea.KeyRight}) // path
	press(t, m, "tab")
	typeIn(t, m, "~/mnt/nas")
	send(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	got := configFile(t, m)
	if !strings.Contains(got, "[shares.nas]\ntype = \"path\"   # the Synology\npath = \"~/mnt/nas\"\n") || strings.Contains(got, "nas.example") {
		t.Errorf("made a path share:\n%s", got)
	}
}

func TestFormRefuses(t *testing.T) {
	m, kr, _ := sharesTab(t, sharesConfig)
	before := configFile(t, m)

	press(t, m, "n")
	typeIn(t, m, "usb") // taken
	send(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.form == nil || !strings.Contains(screen(m), "there is a share named usb already") {
		t.Errorf("a taken name:\n%s", screen(m))
	}

	press(t, m, "esc", "n")
	typeIn(t, m, "x")
	press(t, m, "tab", "tab")
	typeIn(t, m, "host.example") // no share name
	press(t, m, "tab", "tab")
	typeIn(t, m, "u")
	press(t, m, "tab")
	typeIn(t, m, "pw")
	send(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !strings.Contains(screen(m), `shares.x: share is required for type "smb"`) {
		t.Errorf("a missing share name:\n%s", screen(m))
	}
	if len(kr.pw) != 0 {
		t.Errorf("the keyring was written for a share that cannot be saved: %v", kr.pw)
	}

	press(t, m, "tab", "tab") // from the password to the port
	typeIn(t, m, "44x")
	send(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !strings.Contains(screen(m), `the port must be a number, not "44x"`) {
		t.Errorf("a bad port:\n%s", screen(m))
	}

	// The keyring refuses: nothing is written.
	press(t, m, "esc", "n")
	typeIn(t, m, "y")
	press(t, m, "tab", "tab")
	typeIn(t, m, "h")
	press(t, m, "tab")
	typeIn(t, m, "s")
	press(t, m, "tab")
	typeIn(t, m, "u")
	press(t, m, "tab")
	typeIn(t, m, "pw")
	kr.err = secretservice.ErrLocked
	send(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !strings.Contains(screen(m), "the keyring is locked") {
		t.Errorf("a locked keyring:\n%s", screen(m))
	}
	// A long explanation wraps, its end in view.
	kr.err = errors.New(strings.Repeat("the server explains at length ", 10) + "THE END")
	send(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if s := screen(m); !strings.Contains(s, "THE END") {
		t.Errorf("a long error is cut short:\n%s", s)
	}
	if configFile(t, m) != before {
		t.Error("the config was written")
	}

	// Edited meanwhile: not overwritten.
	kr.err = nil
	if err := os.WriteFile(m.o.Config, []byte(before+"\n# a hand edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	send(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !strings.Contains(screen(m), "the config file changed meanwhile") || !strings.HasSuffix(configFile(t, m), "# a hand edit\n") {
		t.Errorf("a file changed meanwhile:\n%s", screen(m))
	}
}

func TestTestAndDeleteShares(t *testing.T) {
	doc := sharesConfig + "\n[shares.bad]\ntype = \"path\"\npath = \"/gone\"\n"
	m, _, ts := sharesTab(t, doc)
	press(t, m, "t") // bad, the first
	if s := screen(m); !strings.Contains(s, "bad: share bad: dial tcp: connection refused") || !strings.Contains(s, "failed, ") {
		t.Errorf("a failed test:\n%s", s)
	}
	press(t, m, "down", "t")
	if got := strings.Join(ts.seen, ","); got != "bad ,nas " {
		t.Errorf("tested %q: the stored password, not a typed one", got)
	}

	// A share in use cannot go.
	press(t, m, "x")
	if s := screen(m); !strings.Contains(s, "nas is used by docs") || m.question != "" {
		t.Errorf("deleting a share in use:\n%s", s)
	}
	// One unused can, after a yes.
	press(t, m, "g", "x")
	if !strings.Contains(screen(m), "│ Delete the share bad from the config file? │") {
		t.Fatalf("no question:\n%s", screen(m))
	}
	press(t, m, "y")
	if got := configFile(t, m); got != sharesConfig {
		t.Errorf("after deleting bad:\n%q", got)
	}
}

func TestSharesWithoutAConfig(t *testing.T) {
	m, _, _ := sharesTab(t, "")
	if s := screen(m); !strings.Contains(s, "No shares yet: n adds one.") || !strings.Contains(s, present.Bus[1]) {
		t.Errorf("no config:\n%s", s)
	}
	press(t, m, "n")
	typeIn(t, m, "usb")
	press(t, m, "tab")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyLeft}) // path
	press(t, m, "tab")
	typeIn(t, m, "/mnt/usb")
	send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := configFile(t, m); got != "[shares.usb]\ntype = \"path\"\npath = \"/mnt/usb\"\n" {
		t.Errorf("new config:\n%q", got)
	}

	m, _, _ = sharesTab(t, "[shares.x]\ntype = \"ftp\"\n")
	press(t, m, "n")
	if s := screen(m); !strings.Contains(s, "The config file has an error") || m.form != nil {
		t.Errorf("a broken config:\n%s", s)
	}
}

// The FREE column: a mounted folder's room as measured here; another
// share's from its last test or its shuttles' last run, whichever is later,
// amber when less than a tenth is left. Under the table, this computer's.
func TestSharesFreeSpace(t *testing.T) {
	m, _, _ := sharesTab(t, sharesConfig)
	if s := screen(m); !strings.Contains(s, "USED BY               FREE") || !strings.Contains(s, "docs                  —") {
		t.Fatalf("nothing known yet:\n%s", s)
	}
	send(t, m, disksMsg{
		"/mnt/usb":          {Free: 50_000_000_000, Total: 1_000_000_000_000},
		"/home/u/Documents": {Free: 120_300_000_000, Total: 511_000_000_000},
	})
	st := m.shuttles[0] // docs, on nas, ran 5m ago
	st.RemoteSpace = fsys.Space{Free: 300_000_000_000, Total: 4_000_000_000_000}
	send(t, m, changedMsg(st))
	press(t, m, "down") // usb: nas's amber is not hidden by the selection
	s := screen(m)
	for _, want := range []string{
		"docs                  300.0 GB of 4.0 TB",
		"-                     50.0 GB of 1.0 TB",
		"This computer: 120.3 GB free of 511.0 GB",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if !strings.Contains(m.View().Content, heldStyle.Render("300.0 GB of 4.0 TB")) {
		t.Error("under a tenth free is not amber")
	}

	// A test now is later than the run: it wins.
	press(t, m, "up", "t")
	if s := screen(m); !strings.Contains(s, "docs                  1.2 TB of 4.0 TB") ||
		!strings.Contains(s, "nas works: 1.2 TB free of 4.0 TB") {
		t.Errorf("after a test:\n%s", s)
	}
}

// Folders that are not there are left out, not reported as empty.
func TestMeasureDisks(t *testing.T) {
	m, _, _ := sharesTab(t, "")
	m.measuring = false
	gone := filepath.Join(t.TempDir(), "gone")
	m.o.Home = gone
	msg := m.measureDisks()()
	if got := msg.(disksMsg); len(got) != 0 {
		t.Errorf("a missing home measured: %v", got)
	}
	m.measuring = false
	m.o.Home = t.TempDir()
	if got := m.measureDisks()().(disksMsg); got[m.o.Home].Total <= 0 {
		t.Errorf("home not measured: %v", got)
	}
	if m.measureDisks() != nil {
		t.Error("a second asking went out while one was out")
	}
}

// In a narrow terminal, a long WHERE gives up its middle, and the columns
// after it still show.
func TestSharesNarrow(t *testing.T) {
	m, _, _ := sharesTab(t, sharesConfig+`
[shares.deep]
type = "path"
path = "/run/media/user/Backup Disk 2TB/shuttle/everything"
`)
	send(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	s := screen(m)
	if !strings.Contains(s, "FREE                LAST TEST") || !strings.Contains(s, "/run/me…rything") {
		t.Errorf("a narrow Shares tab:\n%s", s)
	}
	for _, l := range strings.Split(s, "\n") {
		if strings.HasSuffix(l, "…") {
			t.Errorf("a line is cut short: %q", l)
		}
	}
	if got := cutMiddle("abcdefghij", 7); got != "abc…hij" {
		t.Errorf("cutMiddle = %q", got)
	}
}
