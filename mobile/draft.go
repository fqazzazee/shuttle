// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package mobile

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fqazzazee/shuttle/internal/config"
)

// Draft is a shuttle the wizard makes until it is saved (config.Draft). The
// app fills it in a step at a time; each step's check reads the config file
// as it is then, with the draft in it.
type Draft struct {
	mu   sync.Mutex
	path string
	d    config.Draft
}

// NewDraft is a new shuttle, for the wizard.
func (m *Daemon) NewDraft() *Draft {
	return &Draft{path: m.configPath, d: config.Draft{New: true, Settings: map[string]string{}}}
}

// EditDraft is the named shuttle as the config file has it, for the wizard
// to edit. It keeps its name.
func (m *Daemon) EditDraft(name string) (*Draft, error) {
	d := &Draft{path: m.configPath}
	data, _, err := d.read()
	if err != nil {
		return nil, err
	}
	dd, err := config.DraftOf(data, name)
	if err != nil {
		return nil, err
	}
	d.d = *dd
	return d, nil
}

// draftJSON is where a shuttle syncs, for the wizard to start from.
type draftJSON struct {
	Name, Share string
	Folder      string // a slash path from the share's top
	Local       string // a full path
	Profile     string
	Selection   struct{ Include, Exclude []string }
}

// JSON is where the shuttle syncs and its profile, for the wizard to start
// its steps from: {"Name": "photos", "Share": "nas", "Folder": "/Photos",
// "Local": "/storage/emulated/0/DCIM", "Profile": "", "Selection":
// {"Include": ["2024"], "Exclude": null}}, the selection as a Picker's
// SetSelection takes it.
func (d *Draft) JSON() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := draftJSON{Name: d.d.Name, Profile: d.d.Profile, Local: d.d.Local}
	share, folder, _ := strings.Cut(d.d.Remote, ":")
	out.Share, out.Folder = share, path.Clean("/"+folder)
	if rest, ok := strings.CutPrefix(out.Local, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			out.Local = filepath.Join(home, rest)
		}
	}
	out.Selection.Include, out.Selection.Exclude = d.d.Include, d.d.Exclude
	return toJSON(out)
}

// Moved reports whether the edit syncs other folders than the shuttle
// does: it then starts afresh, as on its first run, which the wizard asks
// about before it saves.
func (d *Draft) Moved() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, home, err := d.read()
	if err != nil {
		return false, err
	}
	return d.d.Moved(data, home)
}

// SetName names the new shuttle, and says why the name will not do: empty,
// not a name the config takes, or another shuttle's.
func (d *Draft) SetName(name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.d.Name = name
	data, _, err := d.read()
	if err != nil {
		return err
	}
	return d.d.CheckName(data)
}

// SetFolders sets where the shuttle syncs: folder, a slash path from the
// top of share, and local, a full path on the phone, with what it selects
// of folder as the wizard's Picker has it (its SelectionJSON). It says why
// they will not do, such as a folder inside another shuttle's; what is
// still to set, such as the mode, is not held against them.
func (d *Draft) SetFolders(share, folder, local, selectionJSON string) error {
	var sel struct{ Include, Exclude []string }
	if err := json.Unmarshal([]byte(selectionJSON), &sel); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.d.Remote, d.d.Local, d.d.Include, d.d.Exclude = share+":"+folder, local, sel.Include, sel.Exclude
	data, home, err := d.read()
	if err != nil {
		return err
	}
	return d.d.CheckFolders(data, home)
}

// SetProfile sets the shuttle's profile; "" for none.
func (d *Draft) SetProfile(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.d.Profile = name
}

// Profile is the shuttle's profile; "" for none.
func (d *Draft) Profile() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.d.Profile
}

// SettingsJSON is the shuttle's settings as rows, as Daemon.SettingsJSON
// gives a saved shuttle's.
func (d *Draft) SettingsJSON() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, _, err := d.read()
	if err != nil {
		return "", err
	}
	rows, err := d.d.Rows(data)
	if err != nil {
		return "", err
	}
	return settingsJSON(rows), nil
}

// Set sets one of the shuttle's settings, as Daemon.SetSetting does a
// saved shuttle's: "" resets it to its profile's or the default. A value
// its row refuses is an error saying why.
func (d *Draft) Set(key, text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, home, err := d.read()
	if err != nil {
		return err
	}
	return d.d.Set(data, home, key, text)
}

// Save writes the shuttle into the config file, or says why it will not
// load as it is, writing nothing. The daemon follows the file.
func (d *Draft) Save() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return d.d.Save(d.path, home)
}

// read is the config file as it is now, nil if there is none yet, and the
// home Parse takes.
func (d *Draft) read() (data []byte, home string, err error) {
	if home, err = os.UserHomeDir(); err != nil {
		return nil, "", err
	}
	data, err = os.ReadFile(d.path)
	if os.IsNotExist(err) {
		return nil, home, nil
	}
	return data, home, err
}
