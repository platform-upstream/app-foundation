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

func TestRejectsTargetReplacementDuringOpen(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	replacement := filepath.Join(parent, "replacement")
	moved := filepath.Join(parent, "observed")
	for _, directory := range []string{target, replacement} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, err := open(target, func(name string) (*os.Root, error) {
		if err := os.Rename(target, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, target); err != nil {
			t.Fatal(err)
		}
		return os.OpenRoot(name)
	})
	if err == nil {
		t.Fatal("replacement target was accepted")
	}
}

func TestRejectsSymlinkTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Symlink(t.TempDir(), target); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(target); err == nil {
		t.Fatal("symlink target was accepted")
	}
}
