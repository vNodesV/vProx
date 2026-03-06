package status

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vNodesV/vProx/internal/push/config"
	pushssh "github.com/vNodesV/vProx/internal/push/ssh"
)

// VMStatus holds live system metrics for one VM, collected via SSH.
type VMStatus struct {
	// Identity (from vms.toml)
	Name       string `json:"name"`
	Datacenter string `json:"datacenter"`
	LanIP      string `json:"lan_ip"`
	PublicIP   string `json:"public_ip"`
	HostRef    string `json:"host_ref,omitempty"`
	Type       string `json:"type"`

	// RPC/REST endpoints (for dashboard probing)
	RPCURL  string `json:"rpc_url,omitempty"`
	RESTURL string `json:"rest_url,omitempty"`

	// Probe config (forwarded for dashboard ping)
	PingCountry  string `json:"ping_country,omitempty"`
	PingProvider string `json:"ping_provider,omitempty"`

	// Live metrics (empty/zero when SSH fails)
	Online     bool    `json:"online"`
	CPUPct     float64 `json:"cpu_pct"`      // 1-minute CPU usage %
	MemPct     float64 `json:"mem_pct"`      // RAM used %
	StoragePct float64 `json:"storage_pct"`  // root partition used %
	LoadAvg    string  `json:"load_avg"`     // "1.23 0.98 0.75" (1/5/15 min)
	AptCount   int     `json:"apt_count"`    // pending apt upgrades

	// Error from last SSH poll (empty on success)
	Error     string    `json:"error,omitempty"`
	PolledAt  time.Time `json:"polled_at"`
}

const vmSSHTimeout = 10 * time.Second

// PollAllVMs polls every VM in cfg concurrently and returns one VMStatus per VM.
// Hosts without [[vm]] entries contribute nothing; [[host]] entries are resolved
// so their public_ip is inherited by VMs with blank public_ip.
func PollAllVMs(cfg *config.Config) []VMStatus {
	results := make([]VMStatus, len(cfg.VMs))

	var wg sync.WaitGroup
	for i, vm := range cfg.VMs {
		wg.Add(1)
		go func(idx int, v config.VM) {
			defer wg.Done()
			results[idx] = pollVM(v, cfg)
		}(i, vm)
	}
	wg.Wait()
	return results
}

func pollVM(vm config.VM, cfg *config.Config) VMStatus {
	st := VMStatus{
		Name:         vm.Name,
		Datacenter:   vm.Datacenter,
		LanIP:        vm.DisplayLanIP(),
		PublicIP:     resolvePublicIP(vm, cfg),
		HostRef:      vm.HostRef,
		Type:         vm.Type,
		RPCURL:       vm.RPC(),
		RESTURL:      vm.REST(),
		PingCountry:  vm.Ping.Country,
		PingProvider: vm.Ping.Provider,
		PolledAt:     time.Now(),
	}

	client, err := pushssh.Dial(vm.Host, vm.Port, vm.User, vm.KeyPath)
	if err != nil {
		st.Online = false
		st.Error = fmt.Sprintf("ssh: %v", err)
		return st
	}
	defer client.Close()

	st.Online = true

	// All metrics in one compound command to minimise round trips.
	// Fields are newline-delimited: cpu | mem | storage | load | apt
	const cmd = `
set -o pipefail
printf '%s\n' \
  "$(top -bn1 2>/dev/null | awk '/^%Cpu/{gsub(/[^0-9.]/,"",$2); print $2+0}' || echo 0)" \
  "$(free 2>/dev/null | awk '/^Mem/{printf "%.1f", $3/$2*100}' || echo 0)" \
  "$(df / 2>/dev/null | awk 'NR==2{gsub(/%/,"",$5); print $5+0}' || echo 0)" \
  "$(uptime 2>/dev/null | awk -F'load average:' '{gsub(/ /,"",$2); print $2}' || echo '0,0,0')" \
  "$(apt list --upgradable 2>/dev/null | grep -c '/' || echo 0)"
`
	out, err := client.Run(strings.TrimSpace(cmd))
	if err != nil {
		st.Error = fmt.Sprintf("metrics: %v", err)
		return st
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) >= 5 {
		st.CPUPct, _ = strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
		st.MemPct, _ = strconv.ParseFloat(strings.TrimSpace(lines[1]), 64)
		st.StoragePct, _ = strconv.ParseFloat(strings.TrimSpace(lines[2]), 64)
		// load avg: "1.23,0.98,0.75" → "1.23 0.98 0.75"
		st.LoadAvg = strings.ReplaceAll(strings.TrimSpace(lines[3]), ",", " ")
		st.AptCount, _ = strconv.Atoi(strings.TrimSpace(lines[4]))
	}

	return st
}

// resolvePublicIP returns the VM's public IP, falling back to the parent host
// if vm.PublicIP is blank and a host_ref is set.
func resolvePublicIP(vm config.VM, cfg *config.Config) string {
	if vm.PublicIP != "" {
		return vm.PublicIP
	}
	if vm.HostRef != "" && cfg != nil {
		if h := cfg.FindHost(vm.HostRef); h != nil {
			return h.PublicIP
		}
	}
	return ""
}
