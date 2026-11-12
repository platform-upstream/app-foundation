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

// Mechanics modules carry no look: their CSS sits in the foundation layer (app rules win) and
// sets no colour, radius, shadow, font or backdrop.
func TestSheetToastPullCSSIsLookNeutral(t *testing.T) {
	look := regexp.MustCompile(`(?m)^\s*(color|background(-color|-image)?|border-radius|box-shadow|font(-family|-size|-weight)?|backdrop-filter|outline-color|fill|stroke)\s*:`)
	for _, name := range []string{"sources/sheet/sheet.css"} {
		raw, err := content.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		css := string(raw)
		if !strings.Contains(css, "@layer foundation {") {
			t.Errorf("%s must stay in @layer foundation", name)
		}
		if match := look.FindString(css); match != "" {
			t.Errorf("%s sets a look value: %q", name, strings.TrimSpace(match))
		}
	}
}

func TestSheetToastPullInstall(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := registry.Install(target, "sheet"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"src/foundation/sheet.ts", "src/foundation/useSheet.ts", "src/foundation/sheet.css"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(name))); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if differences, err := registry.Check(target, "sheet"); err != nil || len(differences) != 0 {
		t.Fatalf("check = %#v, %v", differences, err)
	}
	// The vanilla core must not need React.
	core, err := content.ReadFile("sources/sheet/sheet.ts")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(core), "from 'react'") {
		t.Fatal("sheet.ts imports React; keep it usable without")
	}
}
