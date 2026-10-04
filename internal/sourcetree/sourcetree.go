// Package sourcetree provides root-confined reads and atomic writes for source generators.
package sourcetree

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
)

// Tree anchors operations to one real directory. os.Root prevents concurrent symlink swaps from
// redirecting an operation outside that directory tree.
type Tree struct {
	root *os.Root
}

// Open rejects a symlink/non-directory target and anchors an existing Tree.
func Open(target string) (*Tree, error) {
	return open(target, os.OpenRoot)
}

func open(target string, openRoot func(string) (*os.Root, error)) (*Tree, error) {
	if target == "" {
		return nil, errors.New("source tree target is required")
	}
	observed, err := os.Lstat(target)
	if err != nil {
		return nil, err
	}
	if observed.Mode()&os.ModeSymlink != 0 || !observed.IsDir() {
		return nil, fmt.Errorf("source tree target must be a real directory")
	}
	root, err := openRoot(target)
	if err != nil {
		return nil, err
	}
	anchored, anchorErr := root.Stat(".")
	current, currentErr := os.Lstat(target)
	if anchorErr != nil || currentErr != nil || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() ||
		!os.SameFile(observed, anchored) || !os.SameFile(current, anchored) {
		_ = root.Close()
		return nil, errors.New("source tree target changed while it was opened")
	}
	return &Tree{root: root}, nil
}

// Close releases the anchored directory handle.
func (tree *Tree) Close() error { return tree.root.Close() }

// ReadFile reads a root-confined relative path.
func (tree *Tree) ReadFile(name string) ([]byte, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	return tree.root.ReadFile(name)
}

// Lstat returns root-confined metadata without following the final path component.
func (tree *Tree) Lstat(name string) (fs.FileInfo, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	return tree.root.Lstat(name)
}

// CheckParents rejects every existing symlink or non-directory parent component.
func (tree *Tree) CheckParents(name string) error {
	if err := validName(name); err != nil {
		return err
	}
	directory := path.Dir(name)
	if directory == "." {
		return nil
	}
	current := ""
	for _, component := range strings.Split(directory, "/") {
		if current == "" {
			current = component
		} else {
			current = path.Join(current, component)
		}
		info, err := tree.root.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("source tree parent %q is a symlink", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("source tree parent %q is not a directory", current)
		}
	}
	return nil
}

// AtomicWrite root-confines parent creation, temporary-file creation, and rename. It repeats the
// parent check after creation; os.Root remains the boundary if a parent is swapped concurrently.
func (tree *Tree) AtomicWrite(name string, data []byte) error {
	if err := tree.CheckParents(name); err != nil {
		return err
	}
	directory := path.Dir(name)
	if directory != "." {
		if err := tree.root.MkdirAll(directory, 0o755); err != nil {
			return err
		}
		if err := tree.CheckParents(name); err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 16; attempt++ {
		suffix := make([]byte, 8)
		if _, err := rand.Read(suffix); err != nil {
			return err
		}
		temporary := path.Join(directory, ".source-tree-"+hex.EncodeToString(suffix))
		file, err := tree.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		removeTemporary := true
		defer func() {
			_ = file.Close()
			if removeTemporary {
				_ = tree.root.Remove(temporary)
			}
		}()
		if _, err := file.Write(data); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := tree.root.Rename(temporary, name); err != nil {
			return err
		}
		removeTemporary = false
		if directoryFile, err := tree.root.Open(directory); err == nil {
			_ = directoryFile.Sync()
			_ = directoryFile.Close()
		}
		return nil
	}
	return errors.New("could not allocate a source tree temporary file")
}

func validName(name string) error {
	if !fs.ValidPath(name) || name == "." {
		return fmt.Errorf("invalid source tree path %q", name)
	}
	return nil
}
