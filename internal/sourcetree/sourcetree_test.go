package sourcetree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectsEscapingParentSymlink(t *testing.T) {
	target := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(target, "src")); err != nil {
		t.Fatal(err)
	}
	tree, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	if err := tree.AtomicWrite("src/escaped.txt", []byte("no\n")); err == nil {
		t.Fatal("write through parent symlink succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists: %v", err)
	}
}

func TestAtomicWrite(t *testing.T) {
	tree, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	if err := tree.AtomicWrite("nested/file.txt", []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := tree.AtomicWrite("nested/file.txt", []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	raw, err := tree.ReadFile("nested/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "second\n" {
		t.Fatalf("content = %q", raw)
	}
}
