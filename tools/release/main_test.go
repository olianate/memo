package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetUpdatesEveryLocation(t *testing.T) {
	root := copyRepo(t)

	if err := set(root, "v9.9.9"); err != nil {
		t.Fatalf("set: %v", err)
	}

	for _, f := range fields {
		content, err := readFile(root, f.file)
		if err != nil {
			t.Fatalf("read %s: %v", f.file, err)
		}
		got, err := versionAt(content, f)
		if err != nil {
			t.Fatalf("%s: %v", f.file, err)
		}
		if got != "9.9.9" {
			t.Errorf("%s: %s = %q, want 9.9.9", f.file, f.name, got)
		}
	}

	before := snapshot(t, root)
	if err := set(root, "9.9.9"); err != nil {
		t.Fatalf("second set: %v", err)
	}
	if after := snapshot(t, root); after != before {
		t.Error("set is not idempotent")
	}
}

func TestSetOnlyTouchesRootVersionsInLockfile(t *testing.T) {
	root := copyRepo(t)
	before, err := readFile(root, "frontend/package-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := set(root, "9.9.9"); err != nil {
		t.Fatalf("set: %v", err)
	}
	after, err := readFile(root, "frontend/package-lock.json")
	if err != nil {
		t.Fatal(err)
	}

	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("line count changed: %d -> %d", len(beforeLines), len(afterLines))
	}
	changed := 0
	for i := range beforeLines {
		if beforeLines[i] != afterLines[i] {
			changed++
		}
	}
	if changed != 2 {
		t.Errorf("changed %d lines in package-lock.json, want 2 (root and packages[\"\"] version)", changed)
	}
	if got := strings.Count(after, `"version": "9.9.9"`); got != 2 {
		t.Errorf("found %d version entries, want 2", got)
	}
}

func TestCheckRejectsMismatch(t *testing.T) {
	root := copyRepo(t)
	err := check(root, "9.9.9", false)
	if err == nil {
		t.Fatal("check accepted a version that is not in the files")
	}
	if !strings.Contains(err.Error(), `want "9.9.9"`) {
		t.Errorf("error does not name the expected version: %v", err)
	}
}

func TestCheckUsesConfigVersion(t *testing.T) {
	root := copyRepo(t)
	if err := check(root, "", false); err != nil {
		t.Fatalf("check with config version: %v", err)
	}
}

func TestCheckRequiresReleaseNotesWhenRequested(t *testing.T) {
	root := copyRepo(t)
	version, err := configVersion(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := check(root, version, true); err == nil {
		t.Fatal("check accepted a release without notes")
	}

	notes := filepath.Join(root, "docs", "releases", "v"+version+".md")
	if err := os.WriteFile(notes, []byte("# notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := check(root, version, true); err != nil {
		t.Fatalf("check with notes: %v", err)
	}
}

func TestSetRejectsInvalidVersion(t *testing.T) {
	root := copyRepo(t)
	for _, version := range []string{"", "1.4", "1.4.0-", "release-1"} {
		if err := set(root, version); err == nil {
			t.Errorf("set accepted invalid version %q", version)
		}
	}
}

// copyRepo copies the version files from the real repository into a temp
// directory, so the tests exercise the exact formats the build uses.
func copyRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	seen := make(map[string]bool)
	for _, f := range fields {
		if seen[f.file] {
			continue
		}
		seen[f.file] = true
		src := filepath.Join("..", "..", filepath.FromSlash(f.file))
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		dst := filepath.Join(root, filepath.FromSlash(f.file))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "releases"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func snapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	seen := make(map[string]bool)
	for _, f := range fields {
		if seen[f.file] {
			continue
		}
		seen[f.file] = true
		data, err := readFile(root, f.file)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(f.file)
		b.WriteByte('\n')
		b.WriteString(data)
	}
	return b.String()
}

func configVersion(root string) (string, error) {
	content, err := readFile(root, fields[0].file)
	if err != nil {
		return "", err
	}
	return versionAt(content, fields[0])
}
