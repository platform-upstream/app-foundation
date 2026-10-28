// Package appscaffold renders the canonical repository-local delivery starting point for a
// stateful Go application with an embedded React/Vite frontend.
package appscaffold

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/castillo-platform/app-foundation/internal/sourcetree"
)

//go:embed all:files
var files embed.FS

var applicationName = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

var templateFiles = []string{
	".ci/pipeline.yaml",
	"AGENTS.md",
	".dockerignore",
	"Dockerfile",
	"deploy/Chart.yaml",
	"deploy/values.yaml",
	"deploy/templates/_helpers.tpl",
	"deploy/templates/deployment.yaml",
	"deploy/templates/ingress.yaml",
	"deploy/templates/pvc.yaml",
	"deploy/templates/service.yaml",
}

// Options identifies the consumer repository without embedding a release tag.
type Options struct {
	Name            string
	ImageRepository string
}

// Files returns the stable consumer-relative file set.
func Files() []string { return append([]string(nil), templateFiles...) }

// Generate renders the scaffold into an empty or partially prepared consumer repository. It
// preflights every target and refuses to replace anything.
func Generate(target string, options Options) error {
	if target == "" {
		return errors.New("app scaffold target is required")
	}
	if !applicationName.MatchString(options.Name) {
		return fmt.Errorf("app scaffold name %q must be a DNS label", options.Name)
	}
	if options.ImageRepository == "" || strings.ContainsAny(options.ImageRepository, " \t\r\n") {
		return errors.New("app scaffold image repository is required and may not contain whitespace")
	}
	replacements := strings.NewReplacer(
		"__APP_NAME__", options.Name,
		"__IMAGE_REPOSITORY__", options.ImageRepository,
	)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create app scaffold target: %w", err)
	}
	tree, err := sourcetree.Open(target)
	if err != nil {
		return fmt.Errorf("open app scaffold target: %w", err)
	}
	defer tree.Close()
	type output struct {
		name string
		data []byte
	}
	outputs := make([]output, 0, len(templateFiles))
	for _, name := range templateFiles {
		if err := tree.CheckParents(name); err != nil {
			return fmt.Errorf("inspect app scaffold target %q: %w", name, err)
		}
		if _, err := tree.Lstat(name); err == nil {
			return fmt.Errorf("app scaffold target %q already exists", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect app scaffold target %q: %w", name, err)
		}
		raw, err := fs.ReadFile(files, "files/"+name)
		if err != nil {
			return fmt.Errorf("read app scaffold template %q: %w", name, err)
		}
		rendered := replacements.Replace(string(raw))
		if strings.Contains(rendered, "__APP_NAME__") || strings.Contains(rendered, "__IMAGE_REPOSITORY__") {
			return fmt.Errorf("unresolved app scaffold placeholder in %q", name)
		}
		outputs = append(outputs, output{name: name, data: []byte(rendered)})
	}
	for index, item := range outputs {
		if err := tree.AtomicWrite(item.name, item.data); err != nil {
			return fmt.Errorf("write app scaffold target %q: %w", templateFiles[index], err)
		}
	}
	return nil
}
