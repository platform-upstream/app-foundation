package frontendregistry

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runNodeTSTest runs a testdata script against a TypeScript registry source. It needs a Node
// that strips types (22.18+, 23.6+); an older Node skips, as does a machine without Node.
func runNodeTSTest(t *testing.T, script, source string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}
	if exec.Command(node, "-e", "process.exit(process.features.typescript ? 0 : 3)").Run() != nil {
		t.Skip("node cannot strip TypeScript types")
	}
	out, err := exec.Command(node, filepath.Join("testdata", script), filepath.Join("sources", filepath.FromSlash(source))).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}
}

func TestSheetLogic(t *testing.T) { runNodeTSTest(t, "sheet_test.mjs", "sheet/sheet.ts") }

func TestToastStore(t *testing.T) { runNodeTSTest(t, "toast_test.mjs", "toast/toast.ts") }

// Mechanics modules carry no look: their CSS sits in the foundation layer (app rules win) and
// sets no colour, radius, shadow, font or backdrop; only resets of the UA popover/dialog box.
func TestSheetToastPullCSSIsLookNeutral(t *testing.T) {
	look := regexp.MustCompile(`(?m)^\s*(color|background(?:-color|-image)?|border-radius|box-shadow|font(?:-family|-size|-weight)?|backdrop-filter|outline-color|fill|stroke)\s*:\s*([^;]+);`)
	for _, name := range []string{"sources/sheet/sheet.css", "sources/toast/toast.css"} {
		raw, err := content.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		css := string(raw)
		if !strings.Contains(css, "@layer foundation {") {
			t.Errorf("%s must stay in @layer foundation", name)
		}
		for _, match := range look.FindAllStringSubmatch(css, -1) {
			switch strings.TrimSpace(match[2]) {
			case "transparent", "inherit", "none":
			default:
				t.Errorf("%s sets a look value: %q", name, strings.TrimSpace(match[0]))
			}
		}
	}
}

func TestSheetToastPullInstall(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	modules := map[string][]string{
		"sheet": {"src/foundation/sheet.ts", "src/foundation/useSheet.ts", "src/foundation/sheet.css"},
		"toast": {"src/foundation/toast.ts", "src/foundation/ToastRegion.tsx", "src/foundation/toast.css"},
	}
	for module, files := range modules {
		target := t.TempDir()
		if err := registry.Install(target, module); err != nil {
			t.Fatal(err)
		}
		for _, name := range files {
			if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(name))); err != nil {
				t.Fatalf("%s: %s: %v", module, name, err)
			}
		}
		if differences, err := registry.Check(target, module); err != nil || len(differences) != 0 {
			t.Fatalf("%s: check = %#v, %v", module, differences, err)
		}
	}
	// The cores must not need React, so apps without it can use them.
	for _, core := range []string{"sources/sheet/sheet.ts", "sources/toast/toast.ts"} {
		raw, err := content.ReadFile(core)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "from 'react") {
			t.Fatalf("%s imports React; keep it usable without", core)
		}
	}
}
