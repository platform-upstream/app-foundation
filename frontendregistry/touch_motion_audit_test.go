package frontendregistry

import (
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
