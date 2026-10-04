// Package frontendregistry installs versioned frontend source into consumer repositories.
// Installed files are owned source, not a runtime dependency: consumers may inspect, test, and
// deliberately adapt them. Check reports drift; Sync is the explicit destructive update operation.
package frontendregistry

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed registry.json sources
var content embed.FS

type manifest struct {
	Version int               `json:"version"`
	Modules map[string]Module `json:"modules"`
	Files   []File            `json:"files"`
}

type Module struct {
	Description string   `json:"description"`
	Requires    []string `json:"requires,omitempty"`
}

// File maps one embedded registry source to its consumer-relative target.
type File struct {
	Module string `json:"module"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// Difference describes one missing or changed registry-owned target.
type Difference struct {
	Target string
	Reason string
}

// Registry is the validated embedded frontend registry.
type Registry struct {
	manifest manifest
}

// Load validates and returns the embedded registry.
func Load() (*Registry, error) {
	raw, err := fs.ReadFile(content, "registry.json")
	if err != nil {
		return nil, fmt.Errorf("read frontend registry: %w", err)
	}
	var value manifest
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode frontend registry: %w", err)
	}
	if err := validateManifest(value); err != nil {
		return nil, err
	}
	return &Registry{manifest: value}, nil
}

// Version returns the source-registry format version.
func (registry *Registry) Version() int { return registry.manifest.Version }

// Modules returns module descriptions keyed by stable module name.
func (registry *Registry) Modules() map[string]string {
	result := make(map[string]string, len(registry.manifest.Modules))
	for name, module := range registry.manifest.Modules {
		result[name] = module.Description
	}
	return result
}

// Files returns the files selected by module name. No names means every module.
func (registry *Registry) Files(modules ...string) ([]File, error) {
	selected, err := registry.selectedModules(modules)
	if err != nil {
		return nil, err
	}
	var result []File
	for _, file := range registry.manifest.Files {
		if selected[file.Module] {
			result = append(result, file)
		}
	}
	return result, nil
}

// Install writes absent selected files and refuses to replace drifted files.
func (registry *Registry) Install(target string, modules ...string) error {
	return registry.write(target, false, modules...)
}

// Sync writes every selected file, deliberately replacing consumer drift.
func (registry *Registry) Sync(target string, modules ...string) error {
	return registry.write(target, true, modules...)
}

// Check reports missing or byte-different selected files.
func (registry *Registry) Check(target string, modules ...string) ([]Difference, error) {
	files, err := registry.Files(modules...)
	if err != nil {
		return nil, err
	}
	var differences []Difference
	for _, file := range files {
		expected, err := fs.ReadFile(content, file.Source)
		if err != nil {
			return nil, fmt.Errorf("read registry source %q: %w", file.Source, err)
		}
		actual, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(file.Target)))
		switch {
		case errors.Is(err, os.ErrNotExist):
			differences = append(differences, Difference{Target: file.Target, Reason: "missing"})
		case err != nil:
			return nil, fmt.Errorf("read target %q: %w", file.Target, err)
		case string(actual) != string(expected):
			differences = append(differences, Difference{Target: file.Target, Reason: "changed"})
		}
	}
	return differences, nil
}

func (registry *Registry) write(target string, overwrite bool, modules ...string) error {
	files, err := registry.Files(modules...)
	if err != nil {
		return err
	}
	if target == "" {
		return errors.New("frontend registry target is required")
	}
	type pendingFile struct {
		file        File
		data        []byte
		destination string
	}
	pending := make([]pendingFile, 0, len(files))
	for _, file := range files {
		data, err := fs.ReadFile(content, file.Source)
		if err != nil {
			return fmt.Errorf("read registry source %q: %w", file.Source, err)
		}
		destination := filepath.Join(target, filepath.FromSlash(file.Target))
		if !overwrite {
			actual, readErr := os.ReadFile(destination)
			switch {
			case readErr == nil && string(actual) == string(data):
				continue
			case readErr == nil:
				return fmt.Errorf("target %q differs; use sync to replace registry-owned source", file.Target)
			case !errors.Is(readErr, os.ErrNotExist):
				return fmt.Errorf("read target %q: %w", file.Target, readErr)
			}
		}
		pending = append(pending, pendingFile{file: file, data: data, destination: destination})
	}
	for _, item := range pending {
		if err := atomicWrite(item.destination, item.data); err != nil {
			return fmt.Errorf("write target %q: %w", item.file.Target, err)
		}
	}
	return nil
}

func (registry *Registry) selectedModules(names []string) (map[string]bool, error) {
	result := make(map[string]bool, len(registry.manifest.Modules))
	if len(names) == 0 {
		for name := range registry.manifest.Modules {
			result[name] = true
		}
		return result, nil
	}
	for _, name := range names {
		if _, ok := registry.manifest.Modules[name]; !ok {
			return nil, fmt.Errorf("unknown frontend registry module %q", name)
		}
		result[name] = true
	}
	for changed := true; changed; {
		changed = false
		for name := range result {
			for _, dependency := range registry.manifest.Modules[name].Requires {
				if !result[dependency] {
					result[dependency] = true
					changed = true
				}
			}
		}
	}
	return result, nil
}

func validateManifest(value manifest) error {
	if value.Version != 1 {
		return fmt.Errorf("unsupported frontend registry version %d", value.Version)
	}
	if len(value.Modules) == 0 || len(value.Files) == 0 {
		return errors.New("frontend registry must contain modules and files")
	}
	for name, module := range value.Modules {
		if module.Description == "" {
			return fmt.Errorf("frontend registry module %q has no description", name)
		}
		for _, dependency := range module.Requires {
			if dependency == name {
				return fmt.Errorf("frontend registry module %q depends on itself", name)
			}
			if _, ok := value.Modules[dependency]; !ok {
				return fmt.Errorf("frontend registry module %q requires unknown module %q", name, dependency)
			}
		}
	}
	seen := make(map[string]bool, len(value.Files))
	for _, file := range value.Files {
		if _, ok := value.Modules[file.Module]; !ok {
			return fmt.Errorf("registry file %q has unknown module %q", file.Target, file.Module)
		}
		if !safeRelative(file.Source) || !strings.HasPrefix(file.Source, "sources/") {
			return fmt.Errorf("unsafe registry source %q", file.Source)
		}
		if !safeRelative(file.Target) {
			return fmt.Errorf("unsafe registry target %q", file.Target)
		}
		if seen[file.Target] {
			return fmt.Errorf("duplicate registry target %q", file.Target)
		}
		seen[file.Target] = true
		if _, err := fs.Stat(content, file.Source); err != nil {
			return fmt.Errorf("missing registry source %q: %w", file.Source, err)
		}
	}
	return nil
}

func safeRelative(name string) bool {
	return name != "" && name == path.Clean(name) && name != "." && !path.IsAbs(name) && !strings.HasPrefix(name, "../")
}

func atomicWrite(destination string, data []byte) (returnErr error) {
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".frontend-registry-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, destination)
}

// SortedModuleNames returns stable module names for command output.
func (registry *Registry) SortedModuleNames() []string {
	names := make([]string, 0, len(registry.manifest.Modules))
	for name := range registry.manifest.Modules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
