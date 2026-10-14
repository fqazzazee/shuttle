// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"fmt"
	"slices"
)

// shareKeys are the keys each type of share uses, besides type.
var shareKeys = map[ShareType][]string{
	ShareSMB:  {"host", "port", "share", "user", "domain"},
	ShareNFS3: {"host", "port", "mount_port", "export", "uid", "gid"},
	SharePath: {"path"},
}

// SetShare writes sh as [shares.<name>], adding the table if it is new and
// otherwise changing it in place: the keys sh's type uses are set (an empty
// one is removed), and keys of other types are removed, so a share that
// changed type keeps nothing of the old one. The rest of the file is left
// as it is.
func (d *Doc) SetShare(sh *Share) error {
	uses, ok := shareKeys[sh.Type]
	if !ok {
		return fmt.Errorf("share %s: unknown type %q", sh.Name, sh.Type)
	}
	// Only the keys that are set: uid = 0 is root, not "unset".
	values := map[string]any{}
	for key, v := range map[string]string{"host": sh.Host, "share": sh.Share, "export": sh.Export,
		"path": sh.Path, "user": sh.User, "domain": sh.Domain} {
		if v != "" {
			values[key] = v
		}
	}
	for key, v := range map[string]int{"port": sh.Port, "mount_port": sh.MountPort} {
		if v != 0 {
			values[key] = v
		}
	}
	for key, v := range map[string]*uint32{"uid": sh.UID, "gid": sh.GID} {
		if v != nil {
			values[key] = int64(*v)
		}
	}
	table := []string{"shares", sh.Name}
	if err := d.Set(table, "type", string(sh.Type)); err != nil {
		return err
	}
	for _, key := range []string{"host", "port", "mount_port", "share", "export", "uid", "gid", "path", "user", "domain"} {
		v, ok := values[key]
		if !ok || !slices.Contains(uses, key) {
			if err := d.Delete(table, key); err != nil {
				return err
			}
			continue
		}
		if err := d.Set(table, key, v); err != nil {
			return err
		}
	}
	return nil
}

// DeleteShare removes [shares.<name>], with the comment lines above it.
func (d *Doc) DeleteShare(name string) error {
	return d.DeleteTable([]string{"shares", name})
}
