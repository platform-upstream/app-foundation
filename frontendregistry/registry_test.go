package frontendregistry

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInstallCheckAndExplicitSync(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := registry.Install(target); err != nil {
		t.Fatal(err)
	}
	if differences, err := registry.Check(target); err != nil || len(differences) != 0 {
		t.Fatalf("check after install = %#v, %v", differences, err)
	}

	apiPath := filepath.Join(target, "src", "foundation", "api.ts")
	if err := os.WriteFile(apiPath, []byte("consumer edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(target, "api"); err == nil {
		t.Fatal("install replaced a changed file")
	}
	differences, err := registry.Check(target, "api")
	if err != nil {
		t.Fatal(err)
	}
	if len(differences) != 1 || differences[0] != (Difference{Target: "src/foundation/api.ts", Reason: "changed"}) {
		t.Fatalf("differences = %#v", differences)
	}
	if err := registry.Sync(target, "api"); err != nil {
		t.Fatal(err)
	}
	if differences, err := registry.Check(target, "api"); err != nil || len(differences) != 0 {
		t.Fatalf("check after sync = %#v, %v", differences, err)
	}
}

func TestSelectedModuleAndUnknownModule(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := registry.Install(target, "pwa"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "src", "foundation", "register-service-worker.ts")); err != nil {
		t.Fatal(err)
	}
	// The service worker is a template starting point the application owns, not part of pwa.
	if _, err := os.Stat(filepath.Join(target, "public", "sw.js")); !os.IsNotExist(err) {
		t.Fatalf("sw.js unexpectedly installed by pwa: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "package.json")); !os.IsNotExist(err) {
		t.Fatalf("template unexpectedly installed: %v", err)
	}
	if err := registry.Install(target, "missing"); err == nil {
		t.Fatal("unknown module accepted")
	}
}

func TestTemplateIncludesRequiredPWAModule(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := registry.Install(target, "template"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.json", "public/sw.js", "src/foundation/register-service-worker.ts", "src/foundation/shell.css", "src/foundation/AppShell.tsx", "src/foundation/viewport.ts", "src/foundation/touch.css", "src/foundation/motion.css", "src/foundation/motion.ts"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(name))); err != nil {
			t.Fatalf("required template file %q: %v", name, err)
		}
	}
	if err := registry.Install(target, "api"); err != nil {
		t.Fatal(err)
	}
	for name, required := range map[string]string{
		"vite.config.ts":                            "base: '/'",
		"index.html":                                `href="/pwa/manifest.webmanifest"`,
		"public/pwa/manifest.webmanifest":           `"start_url": "/"`,
		"src/foundation/api.ts":                     "new URL('/api/', location.origin)",
		"src/foundation/register-service-worker.ts": "new URL(options.base ?? '/', origin)",
	} {
		contents, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(contents), required) {
			t.Fatalf("%s does not contain root-origin contract %q", name, required)
		}
	}
}

// The template passes the phone audit as installed: no sideways pan on html, body and the scroller,
// foundation CSS before the app's, 44px nav items.
func TestTemplateIsPhoneReady(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := registry.Install(target, "template"); err != nil {
		t.Fatal(err)
	}
	for _, want := range [][2]string{
		{"index.html", `<html lang="en" class="no-pan-x">`},
		{"index.html", `<body class="no-pan-x">`},
		{"src/App.tsx", "className: 'no-pan-x'"},
		{"src/main.tsx", "import './foundation/shell.css';\nimport './foundation/touch.css';\nimport './foundation/motion.css';\nimport './app.css';"},
		{"src/app.css", "min-height: 44px;"},
	} {
		contents, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(want[0])))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(contents), want[1]) {
			t.Errorf("%s does not contain %q", want[0], want[1])
		}
	}
}

func TestInstallPreflightsBeforeWriting(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte("consumer package\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(target); err == nil {
		t.Fatal("install accepted a changed target")
	}
	if _, err := os.Stat(filepath.Join(target, "src", "foundation", "api.ts")); !os.IsNotExist(err) {
		t.Fatalf("install wrote before completing preflight: %v", err)
	}
}

func TestInstallAndSyncRejectEscapingParentSymlink(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []struct {
		name string
		run  func(string) error
	}{
		{name: "install", run: func(target string) error { return registry.Install(target, "api") }},
		{name: "sync", run: func(target string) error { return registry.Sync(target, "api") }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			target := t.TempDir()
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(target, "src")); err != nil {
				t.Fatal(err)
			}
			if err := operation.run(target); err == nil {
				t.Fatal("write through parent symlink succeeded")
			}
			if _, err := os.Stat(filepath.Join(outside, "foundation", "api.ts")); !os.IsNotExist(err) {
				t.Fatalf("outside file exists: %v", err)
			}
		})
	}
}

func TestManifestPathValidation(t *testing.T) {
	value := manifest{
		Version: 1,
		Modules: map[string]Module{"api": {Description: "test"}},
		Files:   []File{{Module: "api", Source: "sources/api/api.ts", Target: "../outside"}},
	}
	if err := validateManifest(value); err == nil {
		t.Fatal("unsafe target accepted")
	}
}

// Port of Omni's guard: the shell stays in normal flow, on the large viewport under a translucent
// status bar in a standalone app (dvh otherwise), and the keyboard rule
// never sets a property that turns the shell into a containing block for fixed overlays.
func TestShellViewportByStatusBarAndNoContainingBlock(t *testing.T) {
	raw, err := content.ReadFile("sources/pwa-shell/shell.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	if !strings.Contains(css, "@layer foundation {") {
		t.Fatal("shell.css must stay in @layer foundation so app utilities win")
	}
	rule := func(selector string) string {
		match := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
		if match == nil {
			t.Fatalf("rule %q missing", selector)
		}
		return match[1]
	}
	shell := rule(".app-shell")
	if !strings.Contains(shell, "height: 100dvh") {
		t.Fatalf(".app-shell lost 100dvh (exact with the default status bar):%s", shell)
	}
	translucent := rule(`:where(html:has(meta[name='apple-mobile-web-app-status-bar-style'][content='black-translucent'])) .app-shell`)
	if !strings.Contains(translucent, "height: 100lvh") {
		t.Fatalf("black-translucent shell lost 100lvh:%s", translucent)
	}
	if !regexp.MustCompile(`@media \(display-mode: standalone\) \{\s*:where\(html:has\(meta\[name='apple-mobile-web-app-status-bar-style'\]`).MatchString(css) {
		t.Fatal("the 100lvh rule must stay inside @media (display-mode: standalone): in a browser tab lvh pushes the nav under the URL bar's height")
	}
	if regexp.MustCompile(`(^|[;{\s])position\s*:`).MatchString(shell) {
		t.Fatalf(".app-shell must stay in normal flow at rest:%s", shell)
	}
	keyboard := rule(".keyboard-open .app-shell")
	for _, property := range []string{"transform", "filter", "perspective", "will-change", "contain"} {
		if regexp.MustCompile(`(^|[;{\s])` + property + `\s*:`).MatchString(keyboard) {
			t.Fatalf("keyboard rule sets %s (containing-block trap)", property)
		}
	}
}

// The template service worker caches only the shell and /assets/, honours no-store/private, names
// its cache from ?name= and loads sw-app.js (testdata/sw_test.mjs). Needs Node; skipped without.
func TestServiceWorker(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}
	out, err := exec.Command(node, filepath.Join("testdata", "sw_test.mjs"), filepath.Join("sources", "pwa", "sw.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("sw.js: %v\n%s", err, out)
	}
}
