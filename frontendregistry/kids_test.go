package frontendregistry

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Module kids: installs and checks its three files, keeps its CSS look-neutral (layout only, in
// @layer foundation, no !important), and its copy and suggestion logic pass in Node.
func TestKidsModule(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := registry.Install(target, "kids"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"src/foundation/kids.ts", "src/foundation/Kids.tsx", "src/foundation/kids.css"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(name))); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if differences, err := registry.Check(target, "kids"); err != nil || len(differences) != 0 {
		t.Fatalf("check after install: %v %v", differences, err)
	}

	raw, err := content.ReadFile("sources/kids/kids.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	if !strings.Contains(css, "@layer foundation {") || strings.Contains(css, "!important") {
		t.Fatal("kids.css must sit in @layer foundation without !important")
	}
	look := regexp.MustCompile(`(?m)(?:^|[;{]\s*)(color|background(?:-color|-image)?|border(?:-radius|-color)?|box-shadow|font(?:-family|-size|-weight)?|backdrop-filter|outline(?:-color)?|fill|stroke)\s*:\s*([^;}]+)`)
	for _, match := range look.FindAllStringSubmatch(css, -1) {
		switch strings.TrimSpace(match[2]) {
		case "transparent", "inherit", "none", "0":
		default:
			t.Errorf("kids.css sets a look value: %s: %s", match[1], strings.TrimSpace(match[2]))
		}
	}
}

func TestKidsLogic(t *testing.T) { runNodeTSTest(t, "kids_test.mjs", "kids/kids.ts") }
