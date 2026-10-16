// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package terminal

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// installed is a PATH with only these programs.
func installed(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, n := range names {
			if n == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

// The configured command is the config's: never written into, even with
// room to spare behind it.
func TestCommandKeepsConfigured(t *testing.T) {
	configured := make([]string, 1, 4)
	configured[0] = "term"
	Command(configured, installed(), "a")
	if spare := configured[:2]; spare[1] != "" {
		t.Errorf("the configured command was written into: %q", spare)
	}
}

func waitFor(t *testing.T, path string) string {
	t.Helper()
	for range 200 {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never written", path)
	return ""
}
