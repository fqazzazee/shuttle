// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"cmp"
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
)

// probeEvery is how often each share is checked for being reachable, and
// probeTimeout how long one check may take.
var (
	probeEvery   = 30 * time.Second
	probeTimeout = 3 * time.Second
)

// Share reachability, as Status.Share shows it.
const (
	ShareOnline  = "online"
	ShareOffline = "offline"
)

// reachable reports whether a share is there right now: its server accepts a
// connection, or its mounted folder exists. It logs in to nothing.
func reachable(ctx context.Context, s *config.Share, folder string) bool {
	switch s.Type {
	case config.SharePath:
		info, err := os.Stat(filepath.Join(s.Path, filepath.FromSlash(folder)))
		return err == nil && info.IsDir()
	case config.ShareSMB, config.ShareNFS3:
		port := cmp.Or(s.Port, 445)
		if s.Type == config.ShareNFS3 {
			port = cmp.Or(s.Port, 2049)
		}
		ctx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(s.Host, strconv.Itoa(port)))
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}
	return false
}

// watchShare probes one shuttle's share until quit, and tells the runner
// when it becomes reachable or unreachable.
func (r *runner) watchShare(quit <-chan struct{}) {
	share := r.cfg.Shares[r.shuttle.Share]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-quit
		cancel()
	}()
	for {
		r.shareChanged(reachable(ctx, &share, r.shuttle.Remote))
		select {
		case <-quit:
			return
		case <-time.After(probeEvery):
		}
	}
}

// shareChanged records a probe result. A shuttle with the share-available
// trigger runs when its share comes up, including on the first probe after
// the daemon starts.
func (r *runner) shareChanged(up bool) {
	now := ShareOffline
	if up {
		now = ShareOnline
	}
	// The shuttle's view and the share's, together: otherwise a change
	// could be seen before the one before it was counted, and go untold.
	r.d.shareMu.Lock()
	r.mu.Lock()
	was := r.st.Share
	r.st.Share = now
	r.mu.Unlock()
	if was != now {
		r.d.shareChanged(r.shuttle.Share)
	}
	r.d.shareMu.Unlock()
	if was == now {
		return
	}
	r.d.o.Log.Info("share "+now, "shuttle", r.shuttle.Name, "share", r.shuttle.Share)
	if up && r.shuttle.Settings.Has(config.TriggerShareAvailable) {
		r.trigger()
		return
	}
	r.changed()
}
