// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package bus

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// ErrNameTaken means another daemon serves this user's pipe.
var ErrNameTaken = errors.New("another shuttle daemon is running for this user")

// watchBuffer is how many status changes a slow watcher may fall behind
// before it misses some: it is told each shuttle's latest, never stalls the
// daemon.
const watchBuffer = 64

// pipeServer serves one daemon to the clients on its pipe.
type pipeServer struct {
	d        *daemon.Daemon
	mu       sync.Mutex
	watchers map[chan []byte]bool // each gets every change, or every note (true), as its line
}

// Serve serves d on this user's pipe until ctx is done. Call it once d is
// Ready.
func Serve(ctx context.Context, d *daemon.Daemon, n *Notes) error {
	name, err := PipeName()
	if err != nil {
		return err
	}
	return serve(ctx, d, n, name)
}

func serve(ctx context.Context, d *daemon.Daemon, n *Notes, name string) error {
	sd, err := pipeSecurity()
	if err != nil {
		return err
	}
	l, err := winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: sd})
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) ||
			errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return ErrNameTaken
		}
		return fmt.Errorf("listening on %s: %w", name, err)
	}
	s := &pipeServer{d: d, watchers: map[chan []byte]bool{}}
	stop := d.Watch(s.changed)
	defer stop()
	if n != nil {
		n.attach(s)
		defer n.attach(nil)
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	var clients sync.WaitGroup
	defer clients.Wait()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accepting on %s: %w", name, err)
		}
		clients.Go(func() { s.handle(ctx, c) })
	}
}

// handle answers one client's requests until it hangs up or ctx is done.
func (s *pipeServer) handle(ctx context.Context, c net.Conn) {
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	dec, enc := json.NewDecoder(bufio.NewReader(c)), json.NewEncoder(c)
	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			return
		}
		if req.Method == methodWatch {
			s.watch(ctx, c, enc, req.ID, req.Notes)
			return
		}
		r := s.call(&req)
		r.ID = req.ID
		if enc.Encode(r) != nil {
			return
		}
	}
}

// call runs a request on the daemon.
func (s *pipeServer) call(req *request) *reply {
	r := &reply{}
	var err error
	switch req.Method {
	case methodShuttles:
		r.Shuttles = s.d.Statuses()
	case methodSyncNow:
		err = s.d.SyncNow(req.Name)
	case methodSetPaused:
		err = s.d.SetPaused(req.Name, req.On)
	case methodResolve:
		switch c := engine.Choice(req.Choice); c {
		case engine.KeepLocal, engine.KeepRemote, engine.KeepBoth:
			err = s.d.Resolve(context.Background(), req.Name, req.Path, c)
		default:
			err = fmt.Errorf("choice %q must be keep-local, keep-remote or keep-both", req.Choice)
		}
	case methodSetEnabled:
		err = s.d.SetEnabled(req.Name, req.On)
	case methodAllowDeletions:
		err = s.d.AllowDeletions(req.Name)
	case methodActivity:
		var es []activity.Entry
		es, err = s.d.Activity(req.Name, req.Limit)
		for _, e := range es {
			r.Activity = append(r.Activity, entry{Entry: e, Shuttle: e.Shuttle})
		}
	case methodReload:
		err = s.d.Reload()
	case methodConfigError:
		r.ConfigError = s.d.ConfigError()
	case methodState:
		r.State, r.Paused = s.d.State(), s.d.Paused()
	case methodRates:
		r.Up, r.Down = s.d.Rates()
	default:
		err = fmt.Errorf("unknown method %q", req.Method)
	}
	r.Error = toWire(err)
	return r
}

// watch sends c every status change from now on, or every note, until c
// hangs up or ctx is done. The reply to the watch request comes first: from
// then on, nothing is missed.
func (s *pipeServer) watch(ctx context.Context, c net.Conn, enc *json.Encoder, id uint64, notes bool) {
	ch := make(chan []byte, watchBuffer)
	s.mu.Lock()
	s.watchers[ch] = notes
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.watchers, ch)
		s.mu.Unlock()
	}()
	if enc.Encode(&reply{ID: id}) != nil {
		return
	}
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, c) //nolint:errcheck // only its end matters: the client hung up
		close(gone)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-gone:
			return
		case line := <-ch:
			if _, err := c.Write(line); err != nil {
				return
			}
		}
	}
}

// changed is the daemon's listener: it hands st to every watcher that has
// room, and never blocks. st is the daemon's, for the call alone, so it is
// encoded here, once.
func (s *pipeServer) changed(st *daemon.Status) {
	line, err := json.Marshal(&reply{Changed: st})
	if err != nil {
		return
	}
	s.send(append(line, '\n'), false)
}

// send hands line to every watcher of notes, or of changes, that has room.
func (s *pipeServer) send(line []byte, notes bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch, wants := range s.watchers {
		if wants != notes {
			continue
		}
		select {
		case ch <- line:
		default:
		}
	}
}
