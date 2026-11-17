package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
)

// Environment variables that carry the release into the process. A platform's chart sets them
// (platform-charts' app-chart: the chart version and appVersion its CI bakes in); an unset or
// blank one falls back to the binary's build information.
const (
	VersionEnv = "APP_VERSION"
	CommitEnv  = "APP_COMMIT"
)

// foundationModule is this module's path, looked up in the binary's dependencies.
const foundationModule = "github.com/platform-upstream/app-foundation"

// BuildInfo is what /version reports: the app's release, its source commit, the app-foundation
// version it was built with and the Go toolchain. An unknown field is empty.
type BuildInfo struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	Foundation string `json:"foundation"`
	Go         string `json:"go"`
}

// ReadBuildInfo returns the running binary's BuildInfo: version and commit from APP_VERSION and
// APP_COMMIT, else from the build information (the main module's version unless it is a
// development build, the vcs.revision setting), the app-foundation module version from the
// dependency list (a replaced module reports its replacement's version) and runtime.Version.
func ReadBuildInfo() BuildInfo {
	info, _ := debug.ReadBuildInfo()
	return buildInfo(os.Getenv, info)
}

func buildInfo(getenv func(string) string, info *debug.BuildInfo) BuildInfo {
	out := BuildInfo{
		Version: strings.TrimSpace(getenv(VersionEnv)),
		Commit:  strings.TrimSpace(getenv(CommitEnv)),
		Go:      runtime.Version(),
	}
	if info == nil {
		return out
	}
	if out.Version == "" && info.Main.Version != "(devel)" {
		out.Version = info.Main.Version
	}
	if out.Commit == "" {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				out.Commit = s.Value
			}
		}
	}
	if info.Main.Path == foundationModule {
		out.Foundation = moduleVersion(&info.Main)
	}
	for _, dep := range info.Deps {
		if dep.Path == foundationModule {
			out.Foundation = moduleVersion(dep)
		}
	}
	return out
}

func moduleVersion(m *debug.Module) string {
	if m.Replace != nil {
		m = m.Replace
	}
	if m.Version == "(devel)" {
		return ""
	}
	return m.Version
}

// Version returns the /version endpoint: 200 with the process's BuildInfo as JSON, read once
// when the handler is built. It names no host, path, person or setting, so it can sit with the
// probes in front of authentication; a platform's CI and anyone checking a release read it.
func Version() http.Handler {
	body, _ := json.Marshal(ReadBuildInfo())
	body = append(body, '\n')
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}

// Probes registers the three public probe routes on mux: GET /healthz (Health), GET /readyz
// (Ready with readyChecks) and GET /version (Version). Register them before the routes behind
// authentication (README, "Composition").
func Probes(mux *http.ServeMux, readyChecks ...func(context.Context) error) {
	mux.Handle("GET /healthz", Health())
	mux.Handle("GET /readyz", Ready(readyChecks...))
	mux.Handle("GET /version", Version())
}
