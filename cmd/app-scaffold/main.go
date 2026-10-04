package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/niiku-platform/app-foundation/appscaffold"
)

func main() {
	target := flag.String("target", ".", "consumer repository root")
	name := flag.String("name", "", "DNS-label application name")
	image := flag.String("image", "", "chart-owned image repository without a tag")
	flag.Parse()
	if flag.NArg() != 0 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: app-scaffold -name app -image registry/owner/app [-target directory]")
		os.Exit(2)
	}
	if err := appscaffold.Generate(*target, appscaffold.Options{Name: *name, ImageRepository: *image}); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
