// SPDX-License-Identifier: GPL-2.0-or-later

package ignore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		isDir   bool
		want    bool
	}{
		// Unanchored: any depth.
		{"*.tmp", "a.tmp", false, true},
		{"*.tmp", "deep/dir/a.tmp", false, true},
		{"*.tmp", "a.tmpx", false, false},
		{"~$*", "docs/~$report.docx", false, true},
		{".DS_Store", "photos/.DS_Store", false, true},
		{"node_modules", "web/node_modules", true, true},
		// "*" and "?" stay inside one path element.
		{"a*b", "a/b", false, false},
		{"a?b", "a/b", false, false},
		{"a?b", "axb", false, true},
		// Anchored: a "/" anywhere but the end.
		{"/build", "build", true, true},
		{"/build", "src/build", true, false},
		{"docs/drafts", "docs/drafts", true, true},
		{"docs/drafts", "x/docs/drafts", true, false},
		// Trailing slash: folders only.
		{"bin/", "bin", true, true},
		{"bin/", "bin", false, false},
		{"bin/", "cmd/bin", true, true},
		// "**".
		{"**/cache", "cache", true, true},
		{"**/cache", "a/b/cache", true, true},
		{"logs/**", "logs/a/b.txt", false, true},
		{"logs/**", "logs", true, false},
		{"a/**/z", "a/z", false, true},
		{"a/**/z", "a/b/c/z", false, true},
		{"a/**/z", "xa/b/z", false, false},
		// Classes.
		{"file[0-9].txt", "file7.txt", false, true},
		{"file[!0-9].txt", "file7.txt", false, false},
		{"file[!0-9].txt", "fileX.txt", false, true},
		{"[]x]", "]", false, true},
		// Escapes and literal metacharacters.
		{`\#notes`, "#notes", false, true},
		{`\!important`, "!important", false, true},
		{"a+b.(1)", "a+b.(1)", false, true},
		{"a.b", "axb", false, false},
	}
	for _, tt := range tests {
		m, err := Compile([]string{tt.pattern})
		if err != nil {
			t.Fatalf("Compile(%q): %v", tt.pattern, err)
		}
		if got := m.Match(tt.path, tt.isDir); got != tt.want {
			t.Errorf("%q matching %q (dir=%v) = %v, want %v", tt.pattern, tt.path, tt.isDir, got, tt.want)
		}
	}
}

func TestLastMatchWins(t *testing.T) {
	m, err := Compile([]string{
		"# build output",
		"",
		"*.log",
		"!keep.log",
		"secret/keep.log",
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"a.log":           true,
		"keep.log":        false, // re-included
		"x/keep.log":      false,
		"secret/keep.log": true, // ignored again by a later pattern
		"readme.md":       false,
	} {
		if got := m.Match(path, false); got != want {
			t.Errorf("Match(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestZeroValueIgnoresNothing(t *testing.T) {
	var m Matcher
	if m.Match("anything", false) {
		t.Error("the zero Matcher ignored a path")
	}
}

func TestSelect(t *testing.T) {
	tests := []struct {
		name             string
		include, exclude []string
		path             string
		isDir            bool
		want             bool
	}{
		{"everything", nil, nil, "a/b.txt", false, false},
		{"excluded folder", nil, []string{"raw"}, "raw", true, true},
		{"inside an excluded folder", nil, []string{"raw"}, "raw/a.jpg", false, true},
		{"excluded file", nil, []string{"docs/notes.txt"}, "docs/notes.txt", false, true},
		{"next to an excluded file", nil, []string{"docs/notes.txt"}, "docs/other.txt", false, false},
		{"same prefix, other name", nil, []string{"raw"}, "rawer", true, false},
		{"included folder", []string{"photos/2024"}, nil, "photos/2024", true, false},
		{"inside an included folder", []string{"photos/2024"}, nil, "photos/2024/a/b.jpg", false, false},
		{"folder on the way", []string{"photos/2024"}, nil, "photos", true, false},
		{"file named like the way", []string{"photos/2024"}, nil, "photos", false, true},
		{"next to an included folder", []string{"photos/2024"}, nil, "photos/2023", true, true},
		{"file next to the way", []string{"photos/2024"}, nil, "photos/a.jpg", false, true},
		{"top-level, not included", []string{"photos/2024"}, nil, "music", true, true},
		{"same prefix, not included", []string{"photos/2024"}, nil, "photos/20245", true, true},
		{"second include", []string{"photos/2024", "notes.txt"}, nil, "notes.txt", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Matcher
			m.Select(tt.include, tt.exclude)
			if got := m.Match(tt.path, tt.isDir); got != tt.want {
				t.Errorf("Match(%q, dir=%v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
			}
		})
	}
}

// TestSelectWithPatterns checks that the patterns still apply inside what
// is selected, and cannot re-include what is not.
func TestSelectWithPatterns(t *testing.T) {
	m, err := Compile([]string{"*.tmp", "!keep"})
	if err != nil {
		t.Fatal(err)
	}
	m.Select([]string{"docs"}, nil)
	for path, want := range map[string]bool{
		"docs/a.txt": false,
		"docs/a.tmp": true,
		"keep":       true,
	} {
		if got := m.Match(path, false); got != want {
			t.Errorf("Match(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	for _, p := range []string{"file[0-9", `trailing\`, "/", "!/"} {
		if _, err := Compile([]string{"ok", p}); err == nil {
			t.Errorf("Compile(%q) succeeded", p)
		}
	}
}

// TestAgreesWithGit checks the matcher against git itself: the same patterns
// in a .gitignore, the same paths through `git check-ignore`.
func TestAgreesWithGit(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	patterns := []string{
		"*.tmp", "~$*", "/build", "docs/drafts", "bin/", "**/cache", "logs/**",
		"a/**/z", "file[0-9].txt", "*.log", "!keep.log", "secret/keep.log", `\#notes`,
	}
	paths := []struct {
		p     string
		isDir bool
	}{
		{"a.tmp", false}, {"deep/dir/a.tmp", false}, {"a.tmpx", false}, {"docs/~$r.docx", false},
		{"build", true}, {"src/build", true}, {"docs/drafts", true}, {"x/docs/drafts", true},
		{"bin", true}, {"cmd/bin", true}, {"cache", true}, {"a/b/cache", true},
		{"logs/a/b.txt", false}, {"logs", true}, {"a/z", false}, {"a/b/c/z", false}, {"xa/b/z", false},
		{"file7.txt", false}, {"fileX.txt", false}, {"a.log", false}, {"keep.log", false},
		{"x/keep.log", false}, {"secret/keep.log", false}, {"#notes", false}, {"readme.md", false},
		{"src/main.go", false},
	}

	dir := t.TempDir()
	run := func(args ...string) (string, error) {
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("init", "-q"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(strings.Join(patterns, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Real files and folders, so git knows which is which. (A trailing "/"
	// hint is not the same: git then matches "logs/" against "logs/**".)
	for _, x := range paths {
		full := filepath.Join(dir, filepath.FromSlash(x.p))
		if x.isDir {
			err = os.MkdirAll(full, 0o755)
		} else if err = os.MkdirAll(filepath.Dir(full), 0o755); err == nil {
			err = os.WriteFile(full, nil, 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	m, err := Compile(patterns)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range paths {
		// Exit status 0: ignored, 1: not ignored.
		_, err := run("check-ignore", "-q", "--no-index", "--", x.p)
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Fatalf("git check-ignore: %v", err)
		}
		gitSays := err == nil
		if got := m.Match(x.p, x.isDir); got != gitSays {
			t.Errorf("%q (dir=%v): matcher says %v, git says %v", x.p, x.isDir, got, gitSays)
		}
	}
}
