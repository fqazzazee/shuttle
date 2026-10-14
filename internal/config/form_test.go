// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"strings"
	"testing"
)

// WriteRows writes each set row as its type has it, groups as one inline
// table, and removes rows not set or off.
func TestWriteRows(t *testing.T) {
	doc, err := ParseDoc([]byte(`[profiles.p]
mode = "push" # kept
interval = "6h"
`))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"mode": "sync", "triggers": "realtime, schedule", "deletion_guard": "20", "metered": "run",
		"interval":        "6h", // off below: not written
		"ignore":          "none",
		"versioning.kind": "simple", "versioning.count": "3",
		"notify.completed": "off",
	}
	off := func(key string) string {
		if key == "interval" {
			return "only with the schedule trigger, say"
		}
		return ""
	}
	if err := WriteRows(doc, []string{"profiles", "p"}, func(k string) string { return values[k] }, off); err != nil {
		t.Fatal(err)
	}
	got := string(doc.Bytes())
	for _, want := range []string{
		`mode = "sync" # kept`, `triggers = ["realtime", "schedule"]`, "deletion_guard = 20", `metered = "run"`,
		"ignore = []", `versioning = { kind = "simple", count = 3 }`, "notify = { completed = false }",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "interval") {
		t.Errorf("an off row was written:\n%s", got)
	}
	values["deletion_guard"] = "lots"
	if err := WriteRows(doc, []string{"profiles", "p"}, func(k string) string { return values[k] }, off); err == nil ||
		!strings.Contains(err.Error(), "guard must be a number") {
		t.Errorf("a number that is not: %v", err)
	}
}

func TestRawText(t *testing.T) {
	raw := map[string]any{
		"triggers": []any{"realtime", "schedule"}, "ignore": []any{}, "deletion_guard": int64(20),
		"notify": map[string]any{"completed": false},
	}
	for key, want := range map[string]string{
		"triggers": "realtime, schedule", "ignore": "none", "deletion_guard": "20", "notify.completed": "off", "mode": "",
	} {
		if got := RawText(Lookup(raw, key)); got != want {
			t.Errorf("RawText(%s) = %q, want %q", key, got, want)
		}
	}
	if got := ListItems(" realtime ,, schedule, none "); strings.Join(got, "|") != "realtime|schedule" {
		t.Errorf("ListItems = %q", got)
	}
	if RowOf("metered") == nil || RowOf("nothing") != nil {
		t.Error("RowOf")
	}
}
