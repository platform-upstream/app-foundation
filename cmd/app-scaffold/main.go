// Command app-scaffold renders a complete first-party application into an empty repository.
//
//	go run github.com/platform-upstream/app-foundation/cmd/app-scaffold@vX.Y.Z \
//	  -platform ../platform-config/platform.json -name notes -title Notes
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/platform-upstream/app-foundation/appscaffold"
)

func main() {
	var options appscaffold.Options
	var platformFile string
	var platform appscaffold.Platform
	target := flag.String("target", ".", "empty repository root")
	flag.StringVar(&platformFile, "platform", "", `platform profile (JSON: {"org","domain","registry","accessTeam"})`)
	flag.StringVar(&platform.Org, "org", "", "GitHub organisation (overrides the profile)")
	flag.StringVar(&platform.Domain, "domain", "", "app domain (overrides the profile)")
	flag.StringVar(&platform.Registry, "registry", "", "image registry host (overrides the profile)")
	flag.StringVar(&platform.AccessTeam, "access-team", "", "<team>.cloudflareaccess.com (overrides the profile)")
	flag.StringVar(&options.Name, "name", "", "DNS-label application name (required)")
	flag.StringVar(&options.Title, "title", "", "human name; default: the name, capitalized")
	flag.StringVar(&options.Hostname, "host", "", "public hostname; default <name>.<domain>")
	flag.StringVar(&options.Module, "module", "", "Go module path; default github.com/<org>/<name>")
	flag.StringVar(&options.ImageRepository, "image", "", "image repository without tag; default <registry>/<org>/<name>")
	flag.StringVar(&options.Color, "color", "", "icon colour #rrggbb; default derived from the name")
	flag.StringVar(&options.Lang, "lang", "", "document language; default en")
	flag.IntVar(&options.Port, "port", 0, "local preview port (Vite: port-2900, phone audit: port-2800); default 8200-8299 from the name")
	flag.StringVar(&options.FoundationVersion, "foundation", ownVersion(), "app-foundation release to pin")
	flag.Parse()
	if flag.NArg() != 0 || options.Name == "" {
		_, _ = fmt.Fprintln(os.Stderr, "usage: app-scaffold -platform platform.json -name app [-title Title] [-target dir] [flags]")
		flag.PrintDefaults()
		os.Exit(2)
	}
	if platformFile != "" {
		raw, err := os.ReadFile(platformFile)
		if err == nil {
			err = json.Unmarshal(raw, &options.Platform)
		}
		if err != nil {
			fail(fmt.Errorf("platform profile %s: %w", platformFile, err))
		}
	}
	for field, value := range map[*string]string{
		&options.Platform.Org: platform.Org, &options.Platform.Domain: platform.Domain,
		&options.Platform.Registry: platform.Registry, &options.Platform.AccessTeam: platform.AccessTeam,
	} {
		if value != "" {
			*field = value
		}
	}
	if err := appscaffold.Generate(*target, options); err != nil {
		fail(err)
	}
	fmt.Printf("%s scaffolded (app-foundation %s). Next: go mod tidy && go mod vendor, then build web/.\n", options.Name, options.FoundationVersion)
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// ownVersion is the release this binary was built from when run as `go run ...@vX.Y.Z`.
func ownVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return ""
}
