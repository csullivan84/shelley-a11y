package platformpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistingResolvesDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(realDirectory, link); err != nil {
		t.Fatal(err)
	}

	got, err := Existing(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(realDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Existing(%q) = %q, want %q", link, got, want)
	}
}

func TestExistingRejectsMissingPath(t *testing.T) {
	if _, err := Existing(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected an error for a missing path")
	}
}
