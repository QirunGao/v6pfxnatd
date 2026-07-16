//go:build linux

package tests

import (
	"os"
	"path/filepath"
	"testing"

	. "v6pfxnatd/app"
)

func TestWriteOperationalStatusAtomicallyReplacesSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status")
	if err := WriteOperationalStatus(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := WriteOperationalStatus(path, []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "second\n"; got != want {
		t.Fatalf("contents = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0444); got != want {
		t.Fatalf("mode = %04o, want %04o", got, want)
	}
}
