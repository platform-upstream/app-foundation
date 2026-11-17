package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestBuildInfo(t *testing.T) {
	app := &debug.BuildInfo{
		Main:     debug.Module{Path: "example.org/acme/app", Version: "v1.2.3"},
		Deps:     []*debug.Module{{Path: foundationModule, Version: "v1.17.0"}, {Path: "example.org/other", Version: "v0.1.0"}},
		Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: "abc123"}},
	}
	devel := &debug.BuildInfo{
		Main: debug.Module{Path: "example.org/acme/app", Version: "(devel)"},
		Deps: []*debug.Module{{Path: foundationModule, Version: "v1.17.0", Replace: &debug.Module{Path: "../app-foundation", Version: "(devel)"}}},
	}
	self := &debug.BuildInfo{Main: debug.Module{Path: foundationModule, Version: "v1.17.0"}}
	for name, test := range map[string]struct {
		env  map[string]string
		info *debug.BuildInfo
		want BuildInfo
	}{
		"environment wins": {
			map[string]string{VersionEnv: " 1.15.10 ", CommitEnv: "0123abcd"}, app,
			BuildInfo{Version: "1.15.10", Commit: "0123abcd", Foundation: "v1.17.0", Go: runtime.Version()},
		},
		"build information": {
			map[string]string{VersionEnv: "  "}, app,
			BuildInfo{Version: "v1.2.3", Commit: "abc123", Foundation: "v1.17.0", Go: runtime.Version()},
		},
		"development build with a local replacement": {
			nil, devel, BuildInfo{Go: runtime.Version()},
		},
		"the module itself": {
			nil, self, BuildInfo{Version: "v1.17.0", Foundation: "v1.17.0", Go: runtime.Version()},
		},
		"no build information": {
			map[string]string{CommitEnv: "0123abcd"}, nil, BuildInfo{Commit: "0123abcd", Go: runtime.Version()},
		},
	} {
		if got := buildInfo(env(test.env), test.info); got != test.want {
			t.Errorf("%s: %+v, want %+v", name, got, test.want)
		}
	}
}

func TestVersion(t *testing.T) {
	t.Setenv(VersionEnv, "1.15.10")
	t.Setenv(CommitEnv, "0123abcd")
	w := httptest.NewRecorder()
	Version().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/version", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("version = %d %#v", w.Code, w.Header())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got["version"] != "1.15.10" || got["commit"] != "0123abcd" || got["go"] != runtime.Version() {
		t.Fatalf("version body = %s", w.Body.String())
	}
	if _, ok := got["foundation"]; !ok {
		t.Fatalf("version body without foundation: %s", w.Body.String())
	}
}

func TestProbes(t *testing.T) {
	t.Setenv(VersionEnv, "2.0.0")
	mux := http.NewServeMux()
	Probes(mux, func(context.Context) error { return errors.New("down") })
	for path, want := range map[string]int{
		"/healthz": http.StatusOK,
		"/readyz":  http.StatusServiceUnavailable,
		"/version": http.StatusOK,
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Errorf("GET %s = %d, want %d", path, w.Code, want)
		}
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want 405", path, w.Code)
		}
	}
}
