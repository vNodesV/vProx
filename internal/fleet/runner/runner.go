// Package runner executes chain bash scripts on remote VMs via SSH.
// Scripts live at ~/vProx/scripts/chains/{chain}/{component}/{script}.sh
// on every VM that has cloned the vProx repo.
package runner

import (
	"fmt"

	"github.com/vNodesV/vProx/internal/fleet/config"
	fleetssh "github.com/vNodesV/vProx/internal/fleet/ssh"
)

// scriptBase is the path on remote VMs where vProx scripts are cloned.
const scriptBase = "~/vProx/scripts"

// Result holds the output of a remote script execution.
type Result struct {
	Output string
	Err    error
}

// Runner executes deployment scripts on remote VMs.
type Runner struct{}

// New returns a new Runner.
func New() *Runner { return &Runner{} }

// Deploy runs chain/component/script.sh on vm.
//
//   - chain:     e.g. "akash"
//   - component: e.g. "node" | "validator" | "provider" | "relayer"
//   - script:    e.g. "install" | "configure" | "service"
//   - dryRun:    passes --dry-run flag to the script
//   - env:       additional KEY=VALUE pairs prepended to the command
func (r *Runner) Deploy(vm config.VM, chain, component, script string, dryRun bool, env map[string]string) Result {
	c, err := fleetssh.Dial(vm.Host, vm.Port, vm.User, vm.KeyPath)
	if err != nil {
		return Result{Err: fmt.Errorf("runner: ssh dial: %w", err)}
	}
	defer c.Close()

	scriptPath := fmt.Sprintf("%s/chains/%s/%s/%s.sh", scriptBase, chain, component, script)

	var envStr string
	for k, v := range env {
		envStr += fmt.Sprintf("%s=%q ", k, v)
	}

	dryRunFlag := ""
	if dryRun {
		dryRunFlag = " --dry-run"
	}

	cmd := fmt.Sprintf("bash %s%s", scriptPath, dryRunFlag)
	if envStr != "" {
		cmd = envStr + cmd
	}

	out, err := c.Run(cmd)
	return Result{Output: out, Err: err}
}

// RunCmd executes an arbitrary command on vm (for diagnostics / one-offs).
func (r *Runner) RunCmd(vm config.VM, cmd string) Result {
	c, err := fleetssh.Dial(vm.Host, vm.Port, vm.User, vm.KeyPath)
	if err != nil {
		return Result{Err: fmt.Errorf("runner: ssh dial: %w", err)}
	}
	defer c.Close()

	out, err := c.Run(cmd)
	return Result{Output: out, Err: err}
}
