// Command app-scaffold renders a complete first-party application into an empty repository.
//
//	go run github.com/castillo-platform/app-foundation/cmd/app-scaffold@vX.Y.Z -name notes -title Notes
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/castillo-platform/app-foundation/appscaffold"
)

func main() {
	var options appscaffold.Options
	target := flag.String("target", ".", "empty repository root")
	flag.StringVar(&options.Name, "name", "", "DNS-label application name (required)")
	flag.StringVar(&options.Title, "title", "", "human name; default: the name, capitalized")
	flag.StringVar(&options.Hostname, "host", "", "public hostname; default <name>."+appscaffold.DefaultDomain)
	flag.StringVar(&options.Module, "module", "", "Go module path; default github.com/"+appscaffold.DefaultOrg+"/<name>")
	flag.StringVar(&options.ImageRepository, "image", "", "image repository without tag; default "+appscaffold.DefaultRegistry+"/"+appscaffold.DefaultOrg+"/<name>")
	flag.StringVar(&options.Color, "color", "", "icon colour #rrggbb; default derived from the name")
	flag.StringVar(&options.Lang, "lang", "", "document language; default en")
	flag.IntVar(&options.Port, "port", 0, "local preview port (Vite: port-2900); default 8200-8299 from the name")
	flag.StringVar(&options.FoundationVersion, "foundation", ownVersion(), "app-foundation release to pin")
	flag.Parse()
	if flag.NArg() != 0 || options.Name == "" {
		_, _ = fmt.Fprintln(os.Stderr, "usage: app-scaffold -name app [-title Title] [-target dir] [flags]")
		flag.PrintDefaults()
		os.Exit(2)
	}
	if err := appscaffold.Generate(*target, options); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s scaffolded (app-foundation %s). Next: go mod tidy && go mod vendor, then build web/.\n", options.Name, options.FoundationVersion)
}

// ownVersion is the release this binary was built from when run as `go run ...@vX.Y.Z`.
func ownVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return ""
}
