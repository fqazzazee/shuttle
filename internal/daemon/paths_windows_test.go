// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import "testing"

func TestDefaultPaths(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\user\AppData\Roaming`)
	t.Setenv("LOCALAPPDATA", `C:\Users\user\AppData\Local`)
	t.Setenv("XDG_CONFIG_HOME", `C:\elsewhere`) // not Windows'
	p, err := DefaultPaths()
	if err != nil || p.Config != `C:\Users\user\AppData\Roaming\shuttle\config.toml` ||
		p.StateDir != `C:\Users\user\AppData\Local\shuttle\state` {
		t.Errorf("DefaultPaths = %+v, %v", p, err)
	}
	t.Setenv("LOCALAPPDATA", "")
	if _, err := DefaultPaths(); err == nil {
		t.Error("no LOCALAPPDATA: no error")
	}
}
