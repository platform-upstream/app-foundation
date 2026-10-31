// Package appscaffold renders a complete, deployable first-party application: Go backend with
// Cloudflare Access, SQLite and migrations, the React/Vite frontend from the registry, a starting
// icon set, Dockerfile, CI pipeline, Helm chart and AGENTS.md. It renders once into an empty
// repository and refuses to replace anything; every generated file then belongs to the app.
package appscaffold

import (
	"embed"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/castillo-platform/app-foundation/frontendregistry"
	"github.com/castillo-platform/app-foundation/internal/sourcetree"
)

//go:embed all:files
var files embed.FS

var (
	applicationName = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	modulePath      = regexp.MustCompile(`^[a-z0-9.-]+(/[A-Za-z0-9._~-]+)+$`)
	hostname        = regexp.MustCompile(`^([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)
	version         = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	language        = regexp.MustCompile(`^[a-z]{2}$`)
	placeholder     = regexp.MustCompile(`__[A-Z][A-Z_]*__`)
)

// Platform defaults; every one can be overridden in Options.
const (
	DefaultOrg      = "castillo-platform"
	DefaultDomain   = "jprinzbach.de"
	DefaultRegistry = "zot.int.jprinzbach.de"
)

// frontendModules are installed into web/ from the frontend registry.
var frontendModules = []string{"template", "api"}

// Options describe the application. Only Name and FoundationVersion are required.
type Options struct {
	// Name is the DNS-label app name: repository, image, chart, command and hostname label.
	Name string
	// FoundationVersion is the app-foundation release the app pins (vX.Y.Z).
	FoundationVersion string
	// Title is the human name (manifest, document title, home screen). Default: Name, capitalized.
	Title string
	// Hostname defaults to <name>.jprinzbach.de.
	Hostname string
	// Module defaults to github.com/castillo-platform/<name>.
	Module string
	// ImageRepository (without tag) defaults to zot.int.jprinzbach.de/castillo-platform/<name>.
	ImageRepository string
	// Color (#rrggbb) of the starting icon. Default: a hue derived from the name.
	Color string
	// Lang is the document and manifest language (two letters). Default: en.
	Lang string
	// Port is the local preview port of the Go server; Vite runs on Port-2900. Default: a port in
	// 8200-8299 derived from the name.
	Port int
}

func (o *Options) normalize() error {
	if !applicationName.MatchString(o.Name) {
		return fmt.Errorf("app scaffold name %q must be a DNS label", o.Name)
	}
	if !version.MatchString(o.FoundationVersion) {
		return fmt.Errorf("app scaffold foundation version %q must be vX.Y.Z", o.FoundationVersion)
	}
	if o.Title == "" {
		o.Title = strings.ToUpper(o.Name[:1]) + strings.ReplaceAll(o.Name[1:], "-", " ")
	}
	// The title lands in HTML, JSON, a quoted YAML string and Go comments: plain text only.
	if strings.ContainsAny(o.Title, "<>\"&\\`") || strings.ContainsFunc(o.Title, unicode.IsControl) || len(o.Title) > 60 {
		return fmt.Errorf("app scaffold title %q must be plain text up to 60 bytes", o.Title)
	}
	if o.Hostname == "" {
		o.Hostname = o.Name + "." + DefaultDomain
	}
	if !hostname.MatchString(o.Hostname) {
		return fmt.Errorf("app scaffold hostname %q is invalid", o.Hostname)
	}
	if o.Module == "" {
		o.Module = "github.com/" + DefaultOrg + "/" + o.Name
	}
	if !modulePath.MatchString(o.Module) {
		return fmt.Errorf("app scaffold module %q is invalid", o.Module)
	}
	if o.ImageRepository == "" {
		o.ImageRepository = DefaultRegistry + "/" + DefaultOrg + "/" + o.Name
	}
	if strings.ContainsAny(o.ImageRepository, " \t\r\n:@") {
		return errors.New("app scaffold image repository may not contain whitespace, a tag or a digest")
	}
	if o.Color == "" {
		o.Color = nameColor(o.Name)
	}
	if _, err := parseHex(o.Color); err != nil {
		return err
	}
	o.Color = strings.ToLower(o.Color)
	if o.Lang == "" {
		o.Lang = "en"
	}
	if !language.MatchString(o.Lang) {
		return fmt.Errorf("app scaffold language %q must be two lowercase letters", o.Lang)
	}
	if o.Port == 0 {
		o.Port = 8200 + int(hash(o.Name)%100)
	}
	if o.Port < 3000 || o.Port > 65535 {
		return fmt.Errorf("app scaffold port %d must be 3000-65535 (Vite uses port-2900)", o.Port)
	}
	return nil
}

func (o Options) replacer() *strings.Replacer {
	return strings.NewReplacer(
		"__APP_NAME__", o.Name,
		"__APP_TITLE__", o.Title,
		"__APP_HOST__", o.Hostname,
		"__APP_PORT__", strconv.Itoa(o.Port),
		"__VITE_PORT__", strconv.Itoa(o.Port-2900),
		"__MODULE__", o.Module,
		"__IMAGE_REPOSITORY__", o.ImageRepository,
		"__FOUNDATION_VERSION__", o.FoundationVersion,
	)
}

type output struct {
	name string
	data []byte
}

// Files returns the consumer-relative paths Generate writes itself for a placeholder name
// "app" (the registry's web files come on top).
func Files() []string {
	var names []string
	_ = fs.WalkDir(files, "files", func(name string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			names = append(names, renderPath(strings.TrimPrefix(name, "files/"), strings.NewReplacer("__APP_NAME__", "app")))
		}
		return err
	})
	names = append(names, iconFiles...)
	sort.Strings(names)
	return names
}

var iconFiles = []string{"web/public/pwa/icon.svg", "web/public/pwa/icon-180.png", "web/public/pwa/icon-192.png", "web/public/pwa/icon-512.png"}

func renderPath(name string, replacements *strings.Replacer) string {
	return strings.TrimSuffix(replacements.Replace(name), ".tmpl")
}

// Generate renders the application into target, which must not contain any of its files yet. It
// preflights every target before writing.
func Generate(target string, options Options) error {
	if target == "" {
		return errors.New("app scaffold target is required")
	}
	if err := options.normalize(); err != nil {
		return err
	}
	replacements := options.replacer()

	var outputs []output
	err := fs.WalkDir(files, "files", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		rendered := replacements.Replace(string(raw))
		relative := renderPath(strings.TrimPrefix(name, "files/"), replacements)
		if found := placeholder.FindString(rendered); found != "" {
			return fmt.Errorf("unresolved app scaffold placeholder %s in %q", found, relative)
		}
		outputs = append(outputs, output{name: relative, data: []byte(rendered)})
		return nil
	})
	if err != nil {
		return fmt.Errorf("render app scaffold: %w", err)
	}
	icons, err := renderIcons(options)
	if err != nil {
		return err
	}
	outputs = append(outputs, icons...)

	registry, err := frontendregistry.Load()
	if err != nil {
		return err
	}
	registryFiles, err := registry.Files(frontendModules...)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create app scaffold target: %w", err)
	}
	tree, err := sourcetree.Open(target)
	if err != nil {
		return fmt.Errorf("open app scaffold target: %w", err)
	}
	defer tree.Close()
	check := func(name string) error {
		if err := tree.CheckParents(name); err != nil {
			return fmt.Errorf("inspect app scaffold target %q: %w", name, err)
		}
		if _, err := tree.Lstat(name); err == nil {
			return fmt.Errorf("app scaffold target %q already exists", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect app scaffold target %q: %w", name, err)
		}
		return nil
	}
	for _, item := range outputs {
		if err := check(item.name); err != nil {
			return err
		}
	}
	for _, file := range registryFiles {
		if err := check(path.Join("web", file.Target)); err != nil {
			return err
		}
	}

	if err := registry.Install(filepath.Join(target, "web"), frontendModules...); err != nil {
		return fmt.Errorf("install frontend registry: %w", err)
	}
	if err := personalize(tree, options); err != nil {
		return err
	}
	// Written after the registry, so the scaffold's own web/src/App.tsx replaces the template's.
	for _, item := range outputs {
		if err := tree.AtomicWrite(item.name, item.data); err != nil {
			return fmt.Errorf("write app scaffold target %q: %w", item.name, err)
		}
	}
	return nil
}

// personalize gives the registry's app-owned template files the app's identity. Each anchor must
// match exactly once, so a template change that drops one fails the scaffold test.
func personalize(tree *sourcetree.Tree, o Options) error {
	edits := map[string][][2]string{
		"web/index.html": {
			{`<html lang="en">`, `<html lang="` + o.Lang + `">`},
			{`content="Foundation" />`, `content="` + o.Title + `" />`},
			{`<title>Foundation app</title>`, `<title>` + o.Title + `</title>`},
		},
		"web/public/pwa/manifest.webmanifest": {
			{`"name": "Foundation app"`, `"name": "` + o.Title + `"`},
			{`"short_name": "Foundation"`, `"short_name": "` + o.Title + `"`},
			{`"lang": "en"`, `"lang": "` + o.Lang + `"`},
		},
		"web/package.json": {
			{`"name": "foundation-react-app"`, `"name": "` + o.Name + `-web"`},
		},
		"web/public/sw.js": {
			{`'foundation-shell:'`, `'` + o.Name + `-shell:'`},
		},
		"web/vite.config.ts": {
			{"  build: { outDir: 'dist', emptyOutDir: true },\n", "  build: { outDir: 'dist', emptyOutDir: true },\n" +
				fmt.Sprintf("  // Local preview: Go on :%d (AUTH_MODE=local), Vite on :%d with /api proxied.\n", o.Port, o.Port-2900) +
				fmt.Sprintf("  server: { port: %d, strictPort: true, proxy: { '/api': 'http://127.0.0.1:%d' } },\n", o.Port-2900, o.Port)},
		},
	}
	names := make([]string, 0, len(edits))
	for name := range edits {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw, err := tree.ReadFile(name)
		if err != nil {
			return fmt.Errorf("personalize %q: %w", name, err)
		}
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		for _, edit := range edits[name] {
			if strings.Count(text, edit[0]) != 1 {
				return fmt.Errorf("personalize %q: anchor %q not found exactly once", name, edit[0])
			}
			text = strings.Replace(text, edit[0], edit[1], 1)
		}
		if err := tree.AtomicWrite(name, []byte(text)); err != nil {
			return fmt.Errorf("personalize %q: %w", name, err)
		}
	}
	return nil
}

func renderIcons(o Options) ([]output, error) {
	spec, err := newIconSpec(o.Name, o.Color)
	if err != nil {
		return nil, err
	}
	icons := []output{{name: iconFiles[0], data: spec.svg()}}
	for i, size := range []int{180, 192, 512} {
		data, err := spec.png(size)
		if err != nil {
			return nil, err
		}
		icons = append(icons, output{name: iconFiles[i+1], data: data})
	}
	return icons, nil
}

func hash(value string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return h.Sum32()
}

// nameColor picks a saturated mid-tone hue from the name, so neighbouring apps differ.
func nameColor(name string) string {
	hue := float64(hash(name)%360) / 360
	r, g, b := hslToRGB(hue, 0.62, 0.46)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func hslToRGB(h, s, l float64) (uint8, uint8, uint8) {
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	channel := func(t float64) uint8 {
		t = t - math.Floor(t)
		var v float64
		switch {
		case t < 1.0/6:
			v = p + (q-p)*6*t
		case t < 0.5:
			v = q
		case t < 2.0/3:
			v = p + (q-p)*(2.0/3-t)*6
		default:
			v = p
		}
		return uint8(math.Round(v * 255))
	}
	return channel(h + 1.0/3), channel(h), channel(h - 1.0/3)
}
