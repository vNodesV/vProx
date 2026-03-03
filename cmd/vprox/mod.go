package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/vNodesV/vProx/internal/modules"
)

// runModCmd handles: vprox mod <sub> [flags]
//
//	mod list                    — list installed modules
//	mod add    <name@ver> <bin> — register a module binary
//	mod update <name> [--version v] [--bin /path] — update a registered module
//	mod remove <name>           — remove a module from the registry
//	mod restart <name>          — restart the module's systemd service
func runModCmd(home string, args []string) {
	usage := func() {
		fmt.Fprintln(os.Stderr, "Usage: vprox mod <sub> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  list                      list installed vProx ecosystem modules")
		fmt.Fprintln(os.Stderr, "  add    <name@ver> <bin>   register a module binary")
		fmt.Fprintln(os.Stderr, "  update <name> [flags]     update version/path of a registered module")
		fmt.Fprintln(os.Stderr, "  remove <name>             remove a module from the registry")
		fmt.Fprintln(os.Stderr, "  restart <name>            restart the module's systemd service")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags for update:")
		fmt.Fprintln(os.Stderr, "  --version string   new version tag (e.g. v1.3.0)")
		fmt.Fprintln(os.Stderr, "  --bin string       new binary path")
	}

	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	cfgPath := filepath.Join(home, "config", "modules.toml")
	mgr := modules.New(cfgPath)

	switch args[0] {
	case "list":
		mods, err := mgr.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mod list: %v\n", err)
			os.Exit(1)
		}
		if len(mods) == 0 {
			fmt.Println("No modules registered.")
			return
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tVERSION\tBINARY\tSERVICE\tINSTALLED")
		for _, m := range mods {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				m.Name, m.Version, m.BinaryPath, m.ServiceName,
				m.InstalledAt.Format("2006-01-02"))
		}
		tw.Flush()

	case "add":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "mod add: requires <name@version> <binary-path>")
			os.Exit(1)
		}
		nameVer, binPath := args[1], args[2]
		svc := ""
		if len(args) >= 4 {
			svc = args[3]
		}
		if err := mgr.Add(nameVer, binPath, svc); err != nil {
			fmt.Fprintf(os.Stderr, "mod add: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Module %q registered.\n", nameVer)

	case "update":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "mod update: requires <name>")
			os.Exit(1)
		}
		fs := flag.NewFlagSet("mod update", flag.ExitOnError)
		ver := fs.String("version", "", "new version")
		bin := fs.String("bin", "", "new binary path")
		if err := fs.Parse(args[2:]); err != nil {
			os.Exit(1)
		}
		if err := mgr.Update(args[1], *ver, *bin); err != nil {
			fmt.Fprintf(os.Stderr, "mod update: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Module %q updated.\n", args[1])

	case "remove":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "mod remove: requires <name>")
			os.Exit(1)
		}
		if err := mgr.Remove(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "mod remove: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Module %q removed.\n", args[1])

	case "restart":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "mod restart: requires <name>")
			os.Exit(1)
		}
		mods, err := mgr.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mod restart: %v\n", err)
			os.Exit(1)
		}
		var svcName string
		for _, m := range mods {
			if m.Name == args[1] {
				svcName = m.ServiceName
				break
			}
		}
		if svcName == "" {
			fmt.Fprintf(os.Stderr, "mod restart: module %q not found or has no service name\n", args[1])
			os.Exit(1)
		}
		out, err := modules.RestartService(svcName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mod restart: %v\n%s\n", err, out)
			os.Exit(1)
		}
		fmt.Printf("Service %q restarted.\n", svcName)

	default:
		fmt.Fprintf(os.Stderr, "vprox mod: unknown subcommand %q\n\n", args[0])
		usage()
		os.Exit(1)
	}
}
