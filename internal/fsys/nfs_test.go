// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/nfs3"
	"github.com/fqazzazee/shuttle/internal/nfs3/nfs3test"
)

// A folder's handle is used for dirTTL, then looked up again. (go-nfs's
// handles are paths, so a stale one cannot be seen through the server:
// this looks at the cache itself.)
func TestNFSFolderHandlesExpire(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(dir+"/d", 0o755); err != nil {
		t.Fatal(err)
	}
	host, port := nfs3test.Serve(t, dir)
	ctx := context.Background()
	n, err := DialNFS(ctx, &NFSOptions{Options: nfs3.Options{Host: host, Port: port, MountPort: port, Export: "/"}})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	real, err := n.dir(ctx, "test", "d")
	if err != nil {
		t.Fatal(err)
	}
	bogus := nfs3.FH("not a handle")
	n.dirs["d"] = dirHandle{fh: bogus, seen: time.Now()}
	if fh, _ := n.dir(ctx, "test", "d"); !bytes.Equal(fh, bogus) {
		t.Error("a fresh handle was not used")
	}
	n.dirs["d"] = dirHandle{fh: bogus, seen: time.Now().Add(-dirTTL)}
	if fh, _ := n.dir(ctx, "test", "d"); !bytes.Equal(fh, real) {
		t.Errorf("an expired handle was used: %q", fh)
	}
}
