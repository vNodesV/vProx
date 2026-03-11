// Package configwizard provides a dual-mode configuration wizard for vProx TOML files.
// Terminal mode: vprox config [step]
// Web mode:      vprox config --web
package configwizard

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnknownStep is returned when an unrecognized step name is provided.
var ErrUnknownStep = errors.New("unknown step")

// Run executes the terminal wizard.
//
// home is $VPROX_HOME.
// step is an optional step name (ports|settings|chain|vlog|fleet|infra|backup|list|validate).
// If step is "", all steps are run in order.
// Extra args are passed to steps that accept them (chain <name>, infra <dc>).
func Run(home, step string, args []string) error {
	switch strings.ToLower(strings.TrimSpace(step)) {
	case "", "all":
		return runAll(home)
	case "ports":
		return runPorts(home)
	case "settings":
		return runSettings(home)
	case "chain":
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		return runChain(home, name)
	case "vlog":
		return runVLog(home)
	case "fleet":
		return runFleet(home)
	case "infra":
		dc := ""
		if len(args) > 0 {
			dc = args[0]
		}
		return runInfra(home, dc)
	case "backup":
		return runBackup(home)
	case "list":
		return runList(home)
	case "validate":
		return runValidate(home)
	default:
		return fmt.Errorf("%w: %q (valid: ports|settings|chain|vlog|fleet|infra|backup|list|validate)", ErrUnknownStep, step)
	}
}

// runAll executes all 7 steps in order. Errors are accumulated and reported at the end.
func runAll(home string) error {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          vProx Configuration Wizard — All Steps             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println("  Press Enter to accept defaults. Type 'skip' to skip any step.")
	fmt.Println()

	steps := []struct {
		name string
		fn   func() error
	}{
		{"ports", func() error { return runPorts(home) }},
		{"settings", func() error { return runSettings(home) }},
		{"vlog", func() error { return runVLog(home) }},
		{"fleet", func() error { return runFleet(home) }},
		{"backup", func() error { return runBackup(home) }},
	}

	var errs []string
	for _, s := range steps {
		fmt.Printf("\n▶ Running step: %s\n", s.name)
		if err := s.fn(); err != nil {
			fmt.Printf("  ✗ %s: %v\n", s.name, err)
			errs = append(errs, fmt.Sprintf("%s: %v", s.name, err))
		}
	}

	fmt.Println("\n  Chain and infra configs are per-instance — run individually:")
	fmt.Println("    vprox config chain <name>")
	fmt.Println("    vprox config infra <datacenter>")

	if len(errs) > 0 {
		return fmt.Errorf("wizard completed with %d error(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}

	fmt.Println("\n✓ All steps complete. Run 'vprox --validate' to verify the full configuration.")
	return nil
}

// runList prints all config files found under $VPROX_HOME/config/ and their validation status.
func runList(home string) error {
	return listConfigs(home)
}

// runValidate validates all present config files.
func runValidate(home string) error {
	return validateConfigs(home)
}
