// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"strings"
	"testing"
)

func setShare(sh *Share) func(*Doc) error {
	return func(d *Doc) error { return d.SetShare(sh) }
}

func TestSetShare(t *testing.T) {
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
