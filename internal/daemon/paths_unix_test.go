// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package daemon

import "testing"

func TestDefaultPaths(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("XDG_STATE_HOME", "/st")
	p, err := DefaultPaths()
	if err != nil || p.Config != "/cfg/shuttle/config.toml" || p.StateDir != "/st/shuttle" {
		t.Errorf("DefaultPaths = %+v, %v", p, err)
	}
}
