package frontendregistry

import (
	"os/exec"
	"path/filepath"
	"regexp"
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

// The registry shares mechanics, never look values (handbook decision 0005): the edge fade is a
// mask only, the nav badge geometry only, both in @layer foundation, and the badge stays out of
// flow so it never shifts the nav.
func TestLookNeutralEdgeFadeAndNavBadge(t *testing.T) {
	look := regexp.MustCompile(`(^|[;{\s])(color|background(-color|-image)?|border(-radius|-color)?|box-shadow|outline|font-family|backdrop-filter|filter)\s*:`)
	read := func(name string) string {
		raw, err := content.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	edges := read("sources/scroll-edges/scroll-edges.css")
	if !strings.Contains(edges, "@layer foundation {") {
		t.Fatal("scroll-edges.css must stay in @layer foundation")
	}
	if found := look.FindString(edges); found != "" {
		t.Fatalf("scroll-edges.css sets a look property: %q", found)
	}
	for _, selector := range []string{".edge-fade[data-more-end]", ".edge-fade[data-more-start]", ".edge-fade[data-more-start][data-more-end]"} {
		if !strings.Contains(edges, selector+" {") {
			t.Fatalf("scroll-edges.css lost %s", selector)
		}
	}

	shell := read("sources/pwa-shell/shell.css")
	start := strings.Index(shell, ".nav-badge-anchor {")
	if start < 0 {
		t.Fatal("shell.css lost the nav badge")
	}
	badge := shell[start:]
	if found := look.FindString(badge); found != "" {
		t.Fatalf("nav badge sets a look property: %q", found)
	}
	rule := func(selector string) string {
		match := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`).FindStringSubmatch(badge)
		if match == nil {
			t.Fatalf("rule %q missing", selector)
		}
		return match[1]
	}
	if !strings.Contains(rule(".nav-badge-anchor"), "position: relative") || !strings.Contains(rule(".nav-badge"), "position: absolute") {
		t.Fatal("the badge must sit out of flow over its anchor (no layout shift)")
	}
	if !strings.Contains(rule(".nav-badge"), "height: var(--nav-badge-size") || !strings.Contains(rule(".nav-badge"), "white-space: nowrap") {
		t.Fatal("the badge must keep a fixed height on one line")
	}

	component := read("sources/pwa-shell/NavBadge.tsx")
	for _, required := range []string{"count > 99 ? '99+'", "if (!(count > 0)) return null", `role="img"`, "aria-label={label}"} {
		if !strings.Contains(component, required) {
			t.Fatalf("NavBadge.tsx lost %q", required)
		}
	}
}

// The compact profile button is a 44px target (a 32px photo plus 6px padding) and its accessible
// name contains the initials it shows (axe label-content-name-mismatch); the full card names
// itself through its text. Rendering it (React, a bundler, axe in Chromium) is not part of go test.
func TestProfileButtonTargetAndName(t *testing.T) {
	raw, err := content.ReadFile("sources/profile/profile.tsx")
	if err != nil {
		t.Fatal(err)
	}
	tsx := string(raw)
	if got := strings.Count(tsx, "aria-label={profileButtonLabel(t('profile."); got != 2 {
		t.Fatalf("both profile buttons must take their label from profileButtonLabel, got %d", got)
	}
	if !strings.Contains(tsx, "if (!compact) return undefined;") || !strings.Contains(tsx, "person.photo ? '' : `, ${initials(person.name)}`") {
		t.Fatal("profileButtonLabel must leave the full card unlabelled and add the visible initials")
	}
	if got := strings.Count(tsx, `<span className="pf-sr">`); got != 2 {
		t.Fatalf("the full card needs its visually hidden purpose in both buttons, got %d", got)
	}
	if strings.Count(tsx, "size={compact ? 32 : 36}") != 2 {
		t.Fatal("the compact avatar size changed: recheck the 44px target")
	}
	css, err := content.ReadFile("sources/profile/profile.css")
	if err != nil {
		t.Fatal(err)
	}
	compact := regexp.MustCompile(`\.pf-card\.pf-compact \{[^}]*padding: (\d+)px`).FindStringSubmatch(string(css))
	if compact == nil {
		t.Fatal("no padding on .pf-card.pf-compact")
	}
	if padding := compact[1]; padding != "6" {
		t.Fatalf("compact padding %spx: 32px + 2 x padding must be at least 44px", padding)
	}
}
