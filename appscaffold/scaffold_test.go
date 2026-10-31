package appscaffold

import (
	"bytes"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/platform-upstream/app-foundation/frontendregistry"
)

func generate(t *testing.T, options Options) string {
	t.Helper()
	target := t.TempDir()
	if err := Generate(target, options); err != nil {
		t.Fatal(err)
	}
	return target
}

func read(t *testing.T, target, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestGenerateRendersCompleteApp(t *testing.T) {
	target := generate(t, Options{Name: "field-notes", FoundationVersion: "v1.0.0", Platform: testPlatform, Title: "Field Notes: Familie", Lang: "de", Color: "#2F6FDE", Port: 8150})

	// No placeholder survives anywhere, in the scaffold's files or the registry's.
	err := filepath.WalkDir(target, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || strings.HasSuffix(name, ".png") {
			return err
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if found := placeholder.Find(raw); found != nil {
			t.Errorf("%s: unresolved %s", name, found)
		}
		if strings.HasSuffix(name, ".tmpl") {
			t.Errorf("%s: template suffix left", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string][]string{
		"go.mod":                              {"module github.com/acme-apps/field-notes", "app-foundation v1.0.0", "go 1.27.0"},
		"cmd/field-notes/main.go":             {`"github.com/acme-apps/field-notes/internal/api"`, `"127.0.0.1:8150"`},
		"deploy/values.yaml":                  {"repository: registry.example.org/acme-apps/field-notes", "hostname: field-notes.apps.example.org", "cfTeamDomain: acme.cloudflareaccess.com"},
		"deploy/Chart.yaml":                   {"name: field-notes"},
		"Dockerfile":                          {"./cmd/field-notes", "static-debian13:nonroot"},
		".ci/pipeline.yaml":                   {"name: field-notes"},
		"AGENTS.md":                           {"https://field-notes.apps.example.org", "Vite on 127.0.0.1:5250"},
		"CLAUDE.md":                           {"@AGENTS.md"},
		"web/index.html":                      {`<html lang="de">`, `<title>Field Notes: Familie</title>`, `content="Field Notes: Familie" />`, `href="/pwa/icon-180.png"`},
		"web/public/pwa/manifest.webmanifest": {`"name": "Field Notes: Familie"`, `"lang": "de"`},
		"web/package.json":                    {`"name": "field-notes-web"`},
		"web/public/sw.js":                    {`'field-notes-shell:'`},
		"web/vite.config.ts":                  {"port: 5250", "'http://127.0.0.1:8150'"},
		"web/src/App.tsx":                     {"NotesPage"},
		"web/public/pwa/icon.svg":             {`fill="#2f6fde"`},
	} {
		got := read(t, target, name)
		for _, fragment := range want {
			if !strings.Contains(got, fragment) {
				t.Errorf("%s does not contain %q", name, fragment)
			}
		}
	}

	for name, size := range map[string]int{"web/public/pwa/icon-180.png": 180, "web/public/pwa/icon-192.png": 192, "web/public/pwa/icon-512.png": 512} {
		img, err := png.Decode(bytes.NewReader([]byte(read(t, target, name))))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Fatalf("%s: %v", name, b)
		}
		// The letter is drawn: the centre column holds both colours.
		r0, _, _, _ := img.At(size/10, size/10).RGBA()
		seen := false
		for y := 0; y < size; y++ {
			if r, _, _, _ := img.At(size/2, y).RGBA(); r != r0 {
				seen = true
			}
		}
		if !seen {
			t.Fatalf("%s: no glyph", name)
		}
	}

	// The registry-owned files the Dockerfile checks match the pinned release.
	registry, err := frontendregistry.Load()
	if err != nil {
		t.Fatal(err)
	}
	differences, err := registry.Check(filepath.Join(target, "web"), "api", "pwa", "pwa-shell")
	if err != nil || len(differences) != 0 {
		t.Fatalf("registry check: %v %v", differences, err)
	}
}

func TestGenerateDefaults(t *testing.T) {
	target := generate(t, Options{Name: "zz", FoundationVersion: "v1.2.3", Platform: testPlatform})
	if got := read(t, target, "web/index.html"); !strings.Contains(got, "<title>Zz</title>") || !strings.Contains(got, `lang="en"`) {
		t.Fatalf("defaults not applied:\n%s", got)
	}
	options := Options{Name: "zz", FoundationVersion: "v1.2.3", Platform: testPlatform}
	if err := options.normalize(); err != nil {
		t.Fatal(err)
	}
	if options.Port < 8200 || options.Port > 8299 || options.Color == "" || options.Hostname != "zz.apps.example.org" {
		t.Fatalf("defaults: %+v", options)
	}
	for _, name := range Files() {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(strings.Replace(name, "cmd/app/", "cmd/zz/", 1)))); err != nil {
			t.Errorf("listed file %s missing: %v", name, err)
		}
	}
}

func TestEveryLetterHasAGlyph(t *testing.T) {
	for c := byte('a'); c <= 'z'; c++ {
		if _, err := newIconSpec(string(c)+"app", "#336699"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGeneratePreflightsAllTargets(t *testing.T) {
	for _, existing := range []string{"deploy/values.yaml", "web/src/foundation/api.ts", "web/package.json"} {
		target := t.TempDir()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(target, existing)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, existing), []byte("consumer\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Generate(target, Options{Name: "app", FoundationVersion: "v1.0.0", Platform: testPlatform}); err == nil {
			t.Fatalf("generate replaced existing %s", existing)
		}
		for _, untouched := range []string{"Dockerfile", "web/index.html"} {
			if _, err := os.Stat(filepath.Join(target, untouched)); !os.IsNotExist(err) {
				t.Fatalf("existing %s: generate wrote %s before completing preflight", existing, untouched)
			}
		}
	}
}

func TestGenerateRejectsInvalidOptions(t *testing.T) {
	for _, options := range []Options{
		{Name: "Bad_Name", FoundationVersion: "v1.0.0"},
		{Name: "app"},
		{Name: "app", FoundationVersion: "latest"},
		{Name: "app", FoundationVersion: "v1.0.0", ImageRepository: "example/app:latest"},
		{Name: "app", FoundationVersion: "v1.0.0", Color: "blue"},
		{Name: "app", FoundationVersion: "v1.0.0", Title: `A "quoted" title`},
		{Name: "app", FoundationVersion: "v1.0.0", Title: "Tab	here"},
		{Name: "app", FoundationVersion: "v1.0.0", Hostname: "not a host"},
		{Name: "app", FoundationVersion: "v1.0.0", Port: 80},
		{Name: "app", FoundationVersion: "v1.0.0", Lang: "deu"},
	} {
		if err := Generate(t.TempDir(), options); err == nil {
			t.Fatalf("accepted invalid options: %#v", options)
		}
	}
}

// The generated Go application compiles, vets and passes its own tests against this checkout of
// app-foundation (module cache only, no network). Skipped with -short.
func TestGeneratedAppBuildsAndPassesItsTests(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated application")
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	_, self, _, _ := runtime.Caller(0)
	foundation := filepath.Dir(filepath.Dir(self))
	target := generate(t, Options{Name: "probe", FoundationVersion: "v1.0.0", Platform: testPlatform})

	// Stand in for the Vite build and point the module at this checkout.
	if err := os.MkdirAll(filepath.Join(target, "web", "dist", "pwa"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"web/dist/index.html":               "<!doctype html><div id=\"root\"></div>",
		"web/dist/pwa/manifest.webmanifest": "{}",
		"web/dist/pwa/icon-180.png":         "png",
	} {
		if err := os.WriteFile(filepath.Join(target, filepath.FromSlash(name)), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(goBinary, args...)
		cmd.Dir = target
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("mod", "edit", "-replace", "github.com/platform-upstream/app-foundation="+foundation)
	run("mod", "tidy")
	run("vet", "./...")
	run("test", "./...")
	cmd := exec.Command(filepath.Join(filepath.Dir(goBinary), "gofmt"), "-l", "cmd", "internal", "migrations", "web")
	cmd.Dir = target
	if out, err := cmd.CombinedOutput(); err != nil || len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("gofmt: %v %s", err, out)
	}
}

var testPlatform = Platform{Org: "acme-apps", Domain: "apps.example.org", Registry: "registry.example.org", AccessTeam: "acme.cloudflareaccess.com"}

func TestGenerateRequiresAValidPlatform(t *testing.T) {
	for _, platform := range []Platform{
		{},
		{Org: "acme", Domain: "example.org", Registry: "registry.example.org"},
		{Org: "acme", Domain: "example.org", Registry: "registry.example.org", AccessTeam: "acme.example.org"},
		{Org: "acme/x", Domain: "example.org", Registry: "registry.example.org", AccessTeam: "acme.cloudflareaccess.com"},
		{Org: "acme", Domain: "Example Org", Registry: "registry.example.org", AccessTeam: "acme.cloudflareaccess.com"},
	} {
		if err := Generate(t.TempDir(), Options{Name: "app", FoundationVersion: "v1.0.0", Platform: platform}); err == nil {
			t.Fatalf("accepted platform %+v", platform)
		}
	}
}
