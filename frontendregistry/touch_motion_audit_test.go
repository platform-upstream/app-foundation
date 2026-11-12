package frontendregistry

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// lookProperties are look values (ADR 0005): a registry module may set behaviour, never these.
var lookProperties = regexp.MustCompile(`(?m)(^|[;{\s])(color|background(-color|-image)?|border(-radius|-color)?|box-shadow|font-family|font-weight|outline(-color)?|opacity|filter|backdrop-filter)\s*:`)

func readSource(t *testing.T, name string) string {
	t.Helper()
	raw, err := content.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// stripComments drops /* */ comments so prose cannot satisfy or fail a rule check.
func stripComments(css string) string {
	return regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
}

func TestTouchIsLookNeutralAndLayered(t *testing.T) {
	css := stripComments(readSource(t, "sources/touch/touch.css"))
	if !strings.HasPrefix(strings.TrimSpace(css), "@layer foundation {") {
		t.Fatal("touch.css must be wholly inside @layer foundation so application rules win")
	}
	if match := lookProperties.FindString(css); match != "" {
		t.Fatalf("touch.css sets a look value: %q", match)
	}
	if strings.Contains(css, "!important") {
		t.Fatal("touch.css must not use !important: in a layer it beats every application rule")
	}
	coarse := regexp.MustCompile(`(?s)@media \(pointer: coarse\) \{(.*?)\n  \}`).FindStringSubmatch(css)
	if coarse == nil {
		t.Fatal("touch floors must sit under @media (pointer: coarse)")
	}
	for _, want := range []string{"font-size: max(16px, 1em)", "min-height: 44px"} {
		if !strings.Contains(coarse[1], want) {
			t.Fatalf("coarse-pointer block lost %q", want)
		}
	}
	// Checkboxes and radios are excluded from the 44px floor (the label takes the finger).
	if !strings.Contains(coarse[1], "input:not([type='checkbox'], [type='radio']") {
		t.Fatal("44px floor must exclude checkboxes and radios")
	}
	for _, want := range []string{"touch-action: manipulation", "-webkit-tap-highlight-color: transparent", "text-size-adjust: 100%",
		"inset: min(0px, calc((100% - 44px) / 2))", "overflow-x: clip", "overscroll-behavior-x: none"} {
		if !strings.Contains(css, want) {
			t.Fatalf("touch.css lost %q", want)
		}
	}
	// touch-action belongs on controls only: on html it would also reach the page's own scrollers.
	if regexp.MustCompile(`(?m)^\s*html\s*\{[^}]*touch-action`).MatchString(css) {
		t.Fatal("touch-action must stay on controls, not html")
	}
}

// nodeTS returns a Node that runs TypeScript by type stripping and has module.registerHooks, or
// skips the test (the CI image's Node may be older).
func nodeTS(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}
	probe := "process.exit(process.features.typescript && typeof require('node:module').registerHooks === 'function' ? 0 : 1)"
	if err := exec.Command(node, "-e", probe).Run(); err != nil {
		t.Skip("node without type stripping or module.registerHooks")
	}
	return node
}

// The motion policy: no tokens, nothing outside the reduced-motion media query, no !important.
func TestMotionPolicyCSS(t *testing.T) {
	css := stripComments(readSource(t, "sources/motion/motion.css"))
	if !strings.HasPrefix(strings.TrimSpace(css), "@layer foundation {") {
		t.Fatal("motion.css must be wholly inside @layer foundation")
	}
	if strings.Contains(css, "!important") {
		t.Fatal("motion.css must not use !important (the @layer + !important trap)")
	}
	if regexp.MustCompile(`--[a-z-]+\s*:`).MatchString(css) {
		t.Fatal("motion.css must not define tokens: durations and easings are the application's")
	}
	if regexp.MustCompile(`\d+(\.\d+)?m?s\b`).MatchString(css) {
		t.Fatal("motion.css must not carry durations")
	}
	if !regexp.MustCompile(`@media \(prefers-reduced-motion: reduce\) \{\s*::view-transition-group\(\*\)`).MatchString(css) {
		t.Fatal("view transitions must stop under reduced motion")
	}
	ts := readSource(t, "sources/motion/motion.ts")
	if regexp.MustCompile(`(?m)^[^*/]*\.animate\(`).MatchString(ts) {
		t.Fatal("motion.ts must not animate by itself: keyframes are the application's look")
	}
}

func TestMotion(t *testing.T) {
	node := nodeTS(t)
	out, err := exec.Command(node, filepath.Join("testdata", "motion_test.mjs"), filepath.Join("sources", "motion", "motion.ts")).CombinedOutput()
	if err != nil {
		t.Fatalf("motion.ts: %v\n%s", err, out)
	}
}
