// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package bus

import (
	"encoding/json"
	"sync"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/notice"
)

// Notes hands what is worth a notification to the pipe's watchers of notes:
// on Windows, the tray, which shows them. Its Send is the daemon's OnEvent;
// Serve attaches it to the pipe. Until then, and with no one watching, notes
// are dropped: the daemon never waits on them.
type Notes struct {
	mu sync.Mutex
	s  *pipeServer
}

// NewNotes is Notes not yet attached to a pipe.
func NewNotes() *Notes { return &Notes{} }

// Send words e as the desktop and the Android app do (notice.Of), for every
// watcher of notes. It never blocks.
func (n *Notes) Send(e *daemon.Event) {
	note := notice.Of(e)
	line, err := json.Marshal(&reply{Note: &note})
	if err != nil {
		return
	}
	n.mu.Lock()
	s := n.s
	n.mu.Unlock()
	if s != nil {
		s.send(append(line, '\n'), true)
	}
}

func (n *Notes) attach(s *pipeServer) {
	n.mu.Lock()
	n.s = s
	n.mu.Unlock()
}
