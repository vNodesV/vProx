package main

import (
	"fmt"
	"os"

	"github.com/vNodesV/vProx/internal/configwizard"
)

// runConfigCmd dispatches the `vprox config [step] [args...]` command.
//
// Usage:
//
//	vprox config               # run all steps in order (terminal)
//	vprox config --web         # launch browser wizard
//	vprox config ports         # step 1 only
//	vprox config settings      # step 2 only
//	vprox config chain [name]  # step 3 (optional: edit existing chain)
//	vprox config vlog          # step 4 only
//	vprox config fleet         # step 5 only
//	vprox config infra [dc]    # step 6 (optional: target datacenter)
//	vprox config backup        # step 7 only
//	vprox config list          # show all config files + sizes
//	vprox config validate      # validate all present configs
func runConfigCmd(home string, args []string) {
	// Check for --web flag anywhere in args.
	web := false
	filtered := args[:0]
	for _, a := range args {
		if a == "--web" {
			web = true
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered

	if web {
		w := configwizard.NewWeb(home)
		if err := w.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "config wizard: %v\n", err)
			os.Exit(1)
		}
		return
	}

	step := ""
	extra := []string{}
	if len(args) > 0 {
		step = args[0]
		extra = args[1:]
	}

	if err := configwizard.Run(home, step, extra); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
}
