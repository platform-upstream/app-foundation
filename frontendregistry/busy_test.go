package frontendregistry

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// busy.css carries mechanics only (handbook ADR 0005): wholly in @layer foundation, no !important,
// no literal durations, and its look properties only take the app's custom properties or neutral
// values; every animation and transition sits under prefers-reduced-motion: no-preference, and a
// reduce block keeps a still bar.
func TestBusyCSSIsLookNeutral(t *testing.T) {
	css := stripComments(readSource(t, "sources/busy/busy.css"))
	if !strings.HasPrefix(strings.TrimSpace(css), "@layer foundation {") {
		t.Fatal("busy.css must be wholly inside @layer foundation so application rules win")
	}
	if strings.Contains(css, "!important") {
		t.Fatal("busy.css must not use !important: in a layer it beats every application rule")
	}
	if regexp.MustCompile(`\d+(\.\d+)?m?s\b`).MatchString(css) {
		t.Fatal("busy.css must not carry durations: they are the application's")
	}
	if regexp.MustCompile(`--[a-z-]+\s*:`).MatchString(css) {
		t.Fatal("busy.css must not define custom properties: the application sets them")
	}
	neutral := regexp.MustCompile(`^(0|1|none|transparent|inherit|currentColor|var\(--busy-[a-z-]+(, (1|none))?\))$`)
	for _, match := range regexp.MustCompile(`(?m)(?:^|[;{\s])(color|background(?:-color|-image)?|border-radius|border-color|box-shadow|font-family|font-size|font-weight|outline(?:-color)?|opacity|filter|backdrop-filter|fill|stroke)\s*:\s*([^;]+);`).FindAllStringSubmatch(css, -1) {
		if value := strings.TrimSpace(match[2]); !neutral.MatchString(value) {
			t.Errorf("busy.css sets a look value: %s: %s", match[1], value)
		}
	}

	motion := regexp.MustCompile(`(?s)@media \(prefers-reduced-motion: no-preference\) \{(.*?)\n  \}`).FindStringSubmatch(css)
	if motion == nil {
		t.Fatal("busy.css needs a prefers-reduced-motion: no-preference block")
	}
	outside := strings.Replace(css, motion[0], "", 1)
	if regexp.MustCompile(`(?m)^\s*(animation|transition)[a-z-]*\s*:`).MatchString(outside) {
		t.Fatal("animations and transitions belong under prefers-reduced-motion: no-preference")
	}
	if !strings.Contains(motion[1], "animation: foundation-busy-bar var(--busy-bar-duration)") {
		t.Fatal("the bar's sweep must take the app's --busy-bar-duration")
	}
	reduce := regexp.MustCompile(`(?s)@media \(prefers-reduced-motion: reduce\) \{(.*?)\n  \}`).FindStringSubmatch(css)
	if reduce == nil || !strings.Contains(reduce[1], "width: 100%") {
		t.Fatal("under reduced motion the bar must stand still at full width")
	}
	keyframes := regexp.MustCompile(`(?s)@keyframes foundation-busy-bar \{(.*)\}`).FindStringSubmatch(css)
	if keyframes == nil {
		t.Fatal("busy.css lost its keyframes")
	}
	for _, property := range regexp.MustCompile(`(?m)^\s*([a-z-]+)\s*:`).FindAllStringSubmatch(keyframes[1], -1) {
		if property[1] != "transform" {
			t.Fatalf("the sweep animates %s: transform only, so it never lays out or repaints", property[1])
		}
	}
	for _, selector := range []string{".busy-bar[data-busy] {", ".busy-scope[data-pending] [data-result] {", ".busy-status {"} {
		if !strings.Contains(css, selector) {
			t.Fatalf("busy.css lost %s", selector)
		}
	}
}

// The live region and the bar are always mounted (no layout shift, the region is announced when
// its text changes); the bar is hidden from assistive technology; the scope sets aria-busy.
func TestBusyComponents(t *testing.T) {
	tsx := readSource(t, "sources/busy/Busy.tsx")
	for _, want := range []string{
		`data-busy={on ? '' : undefined}`,
		`<span className="busy-bar-track" aria-hidden="true">`,
		`<span className="busy-status" role="status">{on && status ? status : ''}</span>`,
		`data-pending={on ? '' : undefined} aria-busy={on || undefined}`,
		`typeof children === 'function' ? children(on) : children`,
	} {
		if !strings.Contains(tsx, want) {
			t.Fatalf("Busy.tsx lost %q", want)
		}
	}
	if regexp.MustCompile(`(?m)^\s*(if|return).*\bon\b.*null`).MatchString(tsx) {
		t.Fatal("the bar and the live region must stay mounted")
	}
	if strings.Contains(readSource(t, "sources/busy/busy.ts"), "from 'react") {
		t.Fatal("busy.ts imports React; keep the gate usable without")
	}
}

func TestBusyInstall(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := registry.Install(target, "busy"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"src/foundation/busy.ts", "src/foundation/Busy.tsx", "src/foundation/busy.css"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(name))); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if differences, err := registry.Check(target, "busy"); err != nil || len(differences) != 0 {
		t.Fatalf("check = %#v, %v", differences, err)
	}
}
