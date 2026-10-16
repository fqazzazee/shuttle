// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/notify/notifytest"
)

// The daemon starts, serves D-Bus, shows notifications, runs until its
// context ends, and exits cleanly.
func TestDaemonRunsUntilStopped(t *testing.T) {
	privateSessionBus(t)
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	notes, err := notifytest.Serve(conn, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	isolate(t, dir)
	local := t.TempDir()
	cfg := fmt.Sprintf("[shares.m]\ntype = \"path\"\npath = %q\n[shuttles.s]\nmode = \"push\"\nlocal = %q\nremote = \"m:\"\n", t.TempDir(), local)
	if err := os.MkdirAll(dir+"/shuttle", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/shuttle/config.toml", []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"daemon"}, &stdout, &stderr) }()

	// While it runs, it answers on the bus.
	c, err := bus.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var shuttles []daemon.Status
	for range 200 {
		if shuttles, err = c.Shuttles(context.Background()); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || len(shuttles) != 1 || shuttles[0].Name != "s" {
		t.Fatalf("over the bus: %+v, %v", shuttles, err)
	}
	if err := c.SyncNow(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	for i := 0; len(notes.Notes()) == 0; i++ {
		if i == 500 {
			t.Fatal("no notification within 5s of the run asked for")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := notes.Notes()[0]; n.Summary != "s: synced" {
		t.Errorf("notification = %+v, want the run's completion", n)
	}

	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
	}
	for _, want := range []string{"daemon started", "serving D-Bus", "daemon stopped"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, stderr.String())
		}
	}
}
