// SPDX-License-Identifier: GPL-2.0-or-later

package fsys

import (
	"context"
	"errors"
	"io/fs"
	"sync"
)

// ScanTree visits every entry under dir ("." for the whole FS), like Walk,
// but with up to workers folder listings in flight at once. On a network
// share a scan is dominated by round trips per folder, so this cuts its time
// roughly by the number of workers.
//
// fn is called for one entry at a time, never concurrently, so it needs no
// locking; only the order differs from Walk's: entries arrive as their
// folders are listed. Returning fs.SkipDir from a folder skips its contents,
// and from a file the rest of that file's folder. Any listing error, or any
// other error from fn, stops the scan, cancels the listings in flight and is
// returned: an incomplete scan is never passed off as a complete one.
func ScanTree(ctx context.Context, fsys FS, dir string, workers int, fn WalkFunc) error {
	workers = max(workers, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type listing struct {
		entries []FileInfo
		err     error
	}
	jobs := make(chan string)
	results := make(chan listing)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for d := range jobs {
				entries, err := fsys.ReadDir(ctx, d)
				select {
				case results <- listing{entries, err}:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	stop := func(err error) error {
		cancel()
		close(jobs)
		wg.Wait()
		return err
	}

	queue := []string{dir}
	inFlight := 0
	for len(queue) > 0 || inFlight > 0 {
		var send chan<- string
		var next string
		if len(queue) > 0 {
			send, next = jobs, queue[len(queue)-1]
		}
		select {
		case send <- next:
			queue = queue[:len(queue)-1]
			inFlight++
		case l := <-results:
			inFlight--
			if l.err != nil {
				return stop(l.err)
			}
			for _, e := range l.entries {
				err := fn(e)
				if errors.Is(err, fs.SkipDir) {
					if e.Kind == KindDir {
						continue
					}
					break // the rest of this folder
				}
				if err != nil {
					return stop(err)
				}
				if e.Kind == KindDir {
					queue = append(queue, e.Name)
				}
			}
		case <-ctx.Done():
			return stop(ctx.Err())
		}
	}
	return stop(nil)
}
