package frontendregistry

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The logic of the modules push, palette, scroll-edges, api (online) and profile (pre-paint
// theme) runs in Node against the registry sources: testdata/<name>_test.mjs imports the
// TypeScript directly (testdata/ts.mjs). Needs Node 22.18 or later; skipped without Node, and a
// Node without type stripping prints SKIP.
func TestFrontendModuleLogic(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}
	for _, name := range []string{"push", "palette", "scroll_edges", "online", "profile_theme"} {
		t.Run(name, func(t *testing.T) {
			out, err := exec.Command(node, filepath.Join("testdata", name+"_test.mjs"), "sources").CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", name, err, out)
			}
			if strings.HasPrefix(string(out), "SKIP") {
				t.Skip(strings.TrimSpace(string(out)))
			}
		})
	}
}
