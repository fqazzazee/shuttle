// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/bus"
)

// The daemon starts, serves the test's pipe, runs a shuttle when asked,
// runs until its context ends, and exits cleanly.
func TestDaemonRunsUntilStopped(t *testing.T) {
	dir := t.TempDir()
	isolate(t, dir)
	local, mount := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("[shares.m]\ntype = \"path\"\npath = %q\n[shuttles.s]\nmode = \"push\"\nlocal = %q\nremote = \"m:\"\n", mount, local)
	if err := os.MkdirAll(filepath.Join(dir, "shuttle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shuttle", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"daemon"}, &stdout, &stderr) }()

	// While it runs, it answers on the pipe.
	var c *bus.Client
	var err error
	for range 300 {
		if c, err = bus.Connect(); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("the daemon never served its pipe: %v\n%s", err, stderr.String())
	}
	defer c.Close()
	shuttles, err := c.Shuttles(context.Background())
	if err != nil || len(shuttles) != 1 || shuttles[0].Name != "s" {
		t.Fatalf("over the pipe: %+v, %v", shuttles, err)
	}
	if err := c.SyncNow(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		if _, err := os.Stat(filepath.Join(mount, "a.txt")); err == nil {
			break
		}
		if i == 500 {
			t.Fatal("the run asked for did not push within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
	}
	for _, want := range []string{"daemon started", "serving the pipe", "daemon stopped"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, stderr.String())
		}
	}
	if _, err := bus.Connect(); err == nil {
		t.Error("the pipe is still served after the daemon stopped")
	}
}
