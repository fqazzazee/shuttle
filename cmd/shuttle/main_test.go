// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"version", []string{"version"}, 0, "shuttle dev\n", ""},
		{"version flag", []string{"--version"}, 0, "shuttle dev\n", ""},
		{"help", []string{"help"}, 0, "Usage: shuttle", ""},
		{"no args", nil, 2, "", "Usage: shuttle"},
		{"unknown", []string{"bogus"}, 2, "", `unknown command "bogus"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(context.Background(), tt.args, &stdout, &stderr); got != tt.wantCode {
				t.Errorf("exit code = %d, want %d", got, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestDaemonWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"daemon"}, &stdout, &stderr); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), dir+"/shuttle/config.toml") {
		t.Errorf("stderr = %q, want it to name the missing config file", stderr.String())
	}
}

// The daemon starts, runs until its context ends, and exits cleanly.
func TestDaemonRunsUntilStopped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	local := t.TempDir()
	cfg := "[shares.m]\ntype = \"path\"\npath = \"" + t.TempDir() + "\"\n[shuttles.s]\nmode = \"push\"\nlocal = \"" + local + "\"\nremote = \"m:\"\n"
	if err := os.MkdirAll(dir+"/shuttle", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/shuttle/config.toml", []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var stdout, stderr bytes.Buffer
	if code := run(ctx, []string{"daemon"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
	}
	for _, want := range []string{"daemon started", "daemon stopped"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, stderr.String())
		}
	}
}
