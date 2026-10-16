// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"strings"
	"testing"
)

func setShare(sh *Share) func(*Doc) error {
	return func(d *Doc) error { return d.SetShare(sh) }
}

func TestSetShare(t *testing.T) {
	unixPaths(t)
	// A new share goes after the other shares, type first.
	got := edit(t, commented, setShare(&Share{Name: "usb", Type: SharePath, Path: "/mnt/usb"}))
	want := strings.Replace(commented, "user = \"user\"\n", "user = \"user\"\n\n[shares.usb]\ntype = \"path\"\npath = \"/mnt/usb\"\n", 1)
	if got != want {
		t.Errorf("new share:\n%s\nwant:\n%s", got, want)
	}

	// An edit changes the share in place: comments and other keys stay.
	got = edit(t, commented, setShare(&Share{Name: "nas", Type: ShareSMB, Host: "nas.example", Share: "home",
		User: "other", Domain: "CORP", Port: 1445}))
	want = strings.Replace(commented, `user = "user"`, `user = "other"`+"\nport = 1445\ndomain = \"CORP\"", 1)
	if got != want {
		t.Errorf("edited share:\n%s\nwant:\n%s", got, want)
	}

	// A share that changes type keeps nothing of the old one; an emptied
	// optional key goes.
	got = edit(t, commented, setShare(&Share{Name: "nas", Type: ShareNFS3, Host: "nas.example", Export: "/export/home"}))
	if strings.Contains(got, `share = "home"`) || strings.Contains(got, `user = "user"`) ||
		!strings.Contains(got, "type = \"nfs3\"   # the Synology\nhost = \"nas.example\"\nexport = \"/export/home\"\n") {
		t.Errorf("share made NFS:\n%s", got)
	}
	if _, err := Parse([]byte(got), "/home/u"); err != nil {
		t.Errorf("the edited file does not load: %v", err)
	}
}

// uid = 0 is root, and is written; an unset uid is not.
func TestSetShareNFS(t *testing.T) {
	root, users := uint32(0), uint32(100)
	sh := &Share{Name: "nas", Type: ShareNFS3, Host: "nas.example", Export: "/e", MountPort: 892, UID: &root, GID: &users}
	got := edit(t, "", setShare(sh))
	want := "[shares.nas]\ntype = \"nfs3\"\nhost = \"nas.example\"\nmount_port = 892\nexport = \"/e\"\nuid = 0\ngid = 100\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	cfg, err := Parse([]byte(got), "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if back := cfg.Shares["nas"]; back.UID == nil || *back.UID != 0 || *back.GID != 100 || back.MountPort != 892 {
		t.Errorf("read back as %+v", back)
	}
	sh.UID, sh.GID, sh.MountPort = nil, nil, 0
	if got := edit(t, got, setShare(sh)); got != "[shares.nas]\ntype = \"nfs3\"\nhost = \"nas.example\"\nexport = \"/e\"\n" {
		t.Errorf("unset again:\n%s", got)
	}
}

func TestDeleteShare(t *testing.T) {
	doc := "[shares.a]\ntype = \"path\"\npath = \"/a\"\n\n# The USB disk.\n[shares.b]\ntype = \"path\"\npath = \"/b\"\n"
	if got := edit(t, doc, func(d *Doc) error { return d.DeleteShare("b") }); got != "[shares.a]\ntype = \"path\"\npath = \"/a\"\n" {
		t.Errorf("after deleting b:\n%q", got)
	}
}

func TestSetShareRefusesAnUnknownType(t *testing.T) {
	d, err := ParseDoc([]byte(commented))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetShare(&Share{Name: "x", Type: "ftp"}); err == nil {
		t.Error("an unknown type was written")
	}
}
