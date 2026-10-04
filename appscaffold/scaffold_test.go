package appscaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	target := t.TempDir()
	options := Options{Name: "reference-app", ImageRepository: "zot.local:5000/niiku-platform/reference-app"}
	if err := Generate(target, options); err != nil {
		t.Fatal(err)
	}
	for _, name := range Files() {
		raw, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %q: %v", name, err)
		}
		value := string(raw)
		if strings.Contains(value, "__APP_NAME__") || strings.Contains(value, "__IMAGE_REPOSITORY__") {
			t.Fatalf("unresolved placeholder in %q", name)
		}
	}
	values, err := os.ReadFile(filepath.Join(target, "deploy", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(values), "repository: zot.local:5000/niiku-platform/reference-app") {
		t.Fatalf("image repository not rendered:\n%s", values)
	}
}

func TestGeneratePreflightsAllTargets(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "deploy", "values.yaml"), []byte("consumer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Generate(target, Options{Name: "reference-app", ImageRepository: "example/reference-app"})
	if err == nil {
		t.Fatal("generate replaced an existing target")
	}
	if _, err := os.Stat(filepath.Join(target, "Dockerfile")); !os.IsNotExist(err) {
		t.Fatalf("generate wrote before completing preflight: %v", err)
	}
}

func TestGenerateRejectsInvalidOptions(t *testing.T) {
	for _, options := range []Options{
		{Name: "Bad_Name", ImageRepository: "example/app"},
		{Name: "app", ImageRepository: ""},
		{Name: "app", ImageRepository: "example/app latest"},
	} {
		if err := Generate(t.TempDir(), options); err == nil {
			t.Fatalf("accepted invalid options: %#v", options)
		}
	}
}
