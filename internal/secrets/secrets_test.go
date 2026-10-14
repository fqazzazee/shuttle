// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package secrets

import "testing"

var key = Key{Host: "nas.example", Share: "home", User: "user"}

func TestAccount(t *testing.T) {
	for k, want := range map[Key]string{
		key: "smb://user@nas.example/home",
		{Host: "nas.example", Port: 1445, Share: "home", User: "user", Domain: "WORKGROUP"}: "smb://WORKGROUP;user@nas.example:1445/home",
	} {
		if got := k.Account(); got != want {
			t.Errorf("Account() = %q, want %q", got, want)
		}
	}
}

func TestCheck(t *testing.T) {
	if err := key.Check(); err != nil {
		t.Errorf("Check(%+v) = %v", key, err)
	}
	for _, k := range []Key{{Share: "home", User: "u"}, {Host: "h", User: "u"}, {Host: "h", Share: "s"}} {
		if err := k.Check(); err == nil {
			t.Errorf("Check(%+v) accepted a key without a host, share or user", k)
		}
	}
}
