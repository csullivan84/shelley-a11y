package unixsocket

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathPreservesPortablePath(t *testing.T) {
	want := "/tmp/shelley.sock"
	got, err := Path(want)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Path(%q) = %q", want, got)
	}
}

func TestPathShortensLongPathDeterministically(t *testing.T) {
	long := filepath.Join("/tmp", strings.Repeat("nested-directory/", 12), "session.sock")
	first, err := Path(long)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Path(long)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("Path was not deterministic: %q != %q", first, second)
	}
	if len([]byte(first)) > maxPortablePathBytes {
		t.Fatalf("short path is %d bytes: %q", len([]byte(first)), first)
	}
	other, err := Path(long + "-other")
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("distinct logical paths collided")
	}
}

func TestPathRejectsEmptyPath(t *testing.T) {
	if _, err := Path(""); err == nil {
		t.Fatal("expected empty path error")
	}
}
