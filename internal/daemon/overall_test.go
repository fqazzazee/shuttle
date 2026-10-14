// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import "testing"

// The shuttles taken together are in the most urgent of their states;
// everything paused shows as paused unless something more urgent is going
// on; a broken config file is an error whatever the shuttles are doing.
func TestOverall(t *testing.T) {
	for _, tc := range []struct {
		name      string
		states    []State
		pausedAll bool
		configErr string
		want      State
	}{
		{"no shuttles", nil, false, "", StateIdle},
		{"all idle", []State{StateIdle, StateIdle}, false, "", StateIdle},
		{"disabled counts for nothing", []State{StateDisabled}, false, "", StateIdle},
		{"one running", []State{StateIdle, StateRunning}, false, "", StateRunning},
		{"offline over running", []State{StateRunning, StateOffline}, false, "", StateOffline},
		{"held over offline", []State{StateOffline, StateHeld}, false, "", StateHeld},
		{"error over all", []State{StateHeld, StateError, StateRunning}, false, "", StateError},
		{"one paused", []State{StateIdle, StatePaused}, false, "", StatePaused},
		{"all paused", []State{StateIdle}, true, "", StatePaused},
		{"all paused, one still running", []State{StateRunning}, true, "", StateRunning},
		{"all paused, one held", []State{StateHeld}, true, "", StateHeld},
		{"a broken config", []State{StateIdle}, false, "line 3: bad", StateError},
	} {
		all := make([]Status, len(tc.states))
		for i, s := range tc.states {
			all[i].State = s
		}
		if got := Overall(all, tc.pausedAll, tc.configErr); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}
