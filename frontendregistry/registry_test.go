package frontendregistry

import (
	"os"
	"path/filepath"
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
	if _, err := os.Stat(filepath.Join(target, "public", "sw.js")); err != nil {
		t.Fatal(err)
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
	for _, name := range []string{"package.json", "public/sw.js", "src/foundation/register-service-worker.ts"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(name))); err != nil {
			t.Fatalf("required template file %q: %v", name, err)
		}
	}
	if err := registry.Install(target, "api"); err != nil {
		t.Fatal(err)
	}
	for name, required := range map[string]string{
		"vite.config.ts":                            "base: '/'",
		"index.html":                                `href="/manifest.webmanifest"`,
		"public/manifest.webmanifest":               `"start_url": "/"`,
		"src/foundation/api.ts":                     "new URL('/api/', location.origin)",
		"src/foundation/register-service-worker.ts": "new URL('/sw.js', origin)",
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
