package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/castillo-platform/app-foundation/frontendregistry"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	registry, err := frontendregistry.Load()
	if err != nil {
		return err
	}
	if len(arguments) == 0 {
		return usageError()
	}
	if arguments[0] == "list" {
		if len(arguments) != 1 {
			return usageError()
		}
		for _, name := range registry.SortedModuleNames() {
			fmt.Printf("%s\t%s\n", name, registry.Modules()[name])
		}
		return nil
	}
	if arguments[0] != "install" && arguments[0] != "sync" && arguments[0] != "check" {
		return usageError()
	}
	flags := flag.NewFlagSet(arguments[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	target := flags.String("target", ".", "consumer repository root")
	moduleList := flags.String("modules", "", "comma-separated modules; empty selects all")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return usageError()
	}
	modules := splitModules(*moduleList)
	switch arguments[0] {
	case "install":
		return registry.Install(*target, modules...)
	case "sync":
		return registry.Sync(*target, modules...)
	default:
		differences, err := registry.Check(*target, modules...)
		if err != nil {
			return err
		}
		if len(differences) == 0 {
			return nil
		}
		for _, difference := range differences {
			_, _ = fmt.Fprintf(os.Stderr, "%s: %s\n", difference.Target, difference.Reason)
		}
		return fmt.Errorf("frontend registry check found %d difference(s); registry-owned files are not edited in the app: run `frontend-registry sync -target %s -modules %s` and review the diff", len(differences), *target, *moduleList)
	}
}

func splitModules(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func usageError() error {
	return fmt.Errorf("usage: frontend-registry list | <install|sync|check> [-target directory] [-modules api,pwa,template]")
}
