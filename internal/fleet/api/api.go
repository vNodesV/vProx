// Package api exposes fleet Service functionality as HTTP JSON handlers.
// Handlers are methods on Handlers and are wired into the vOps mux by server.go.
package api

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vNodesV/vProx/internal/fleet"
	"github.com/vNodesV/vProx/internal/fleet/config"
	"github.com/vNodesV/vProx/internal/fleet/status"
)

// Handlers holds a reference to the fleet Service.
type Handlers struct {
	svc *fleet.Service
}

// New returns an Handlers backed by svc.
func New(svc *fleet.Service) *Handlers { return &Handlers{svc: svc} }

// ── GET /api/v1/fleet/vms/status ──────────────────────────────────────────────

// HandleVMStatus polls all VMs concurrently via SSH and returns live metrics.
// The response includes a "hosts" array for tree grouping in the dashboard.
func (h *Handlers) HandleVMStatus(w http.ResponseWriter, r *http.Request) {
	results := status.PollAllVMs(h.svc.Config())
	writeJSON(w, http.StatusOK, map[string]any{
		"vms":   results,
		"hosts": h.svc.Hosts(),
	})
}

// ── GET /api/v1/fleet/vms ─────────────────────────────────────────────────────

type vmView struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Datacenter string `json:"datacenter"`
	Type       string `json:"type"`
}

// HandleVMs returns the list of registered VMs.
func (h *Handlers) HandleVMs(w http.ResponseWriter, r *http.Request) {
	vms := h.svc.VMs()
	out := make([]vmView, 0, len(vms))
	for _, vm := range vms {
		out = append(out, vmView{Name: vm.Name, Host: vm.Host, Datacenter: vm.Datacenter, Type: vm.Type})
	}
	writeJSON(w, http.StatusOK, map[string]any{"vms": out})
}

// ── GET /api/v1/fleet/chains ──────────────────────────────────────────────────

// HandleChains returns all chain statuses (VM-managed + registered).
func (h *Handlers) HandleChains(w http.ResponseWriter, r *http.Request) {
	statuses := h.svc.AllStatuses()
	writeJSON(w, http.StatusOK, map[string]any{"chains": statuses})
}

// ── GET /api/v1/fleet/chains/{chain} ──────────────────────────────────────────

// HandleChainStatus returns the polled status for a single chain.
func (h *Handlers) HandleChainStatus(w http.ResponseWriter, r *http.Request) {
	chain := r.PathValue("chain")
	if chain == "" {
		http.Error(w, "chain required", http.StatusBadRequest)
		return
	}
	st := h.svc.Status(chain)
	if st == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "chain not found or not yet polled"})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ── GET /api/v1/fleet/deployments ─────────────────────────────────────────────

// HandleDeployments returns recent deployment history.
func (h *Handlers) HandleDeployments(w http.ResponseWriter, r *http.Request) {
	chain := r.URL.Query().Get("chain")
	deps, err := h.svc.DB().ListDeployments(chain)
	if err != nil {
		log.Printf("[fleet/api] list deployments: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": deps})
}

// ── POST /api/v1/fleet/deploy ─────────────────────────────────────────────────

type deployRequest struct {
	VM        string            `json:"vm"`
	Chain     string            `json:"chain"`
	Component string            `json:"component"`
	Script    string            `json:"script"`
	DryRun    bool              `json:"dry_run"`
	Env       map[string]string `json:"env"`
}

// HandleDeploy runs a chain script on a VM and records the result.
func (h *Handlers) HandleDeploy(w http.ResponseWriter, r *http.Request) {
	var req deployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.VM == "" || req.Chain == "" || req.Component == "" || req.Script == "" {
		http.Error(w, "vm, chain, component, script required", http.StatusBadRequest)
		return
	}

	vm := h.svc.FindVM(req.VM)
	if vm == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "vm not found: " + req.VM})
		return
	}

	id, err := h.svc.DB().InsertDeployment(req.Chain, req.Component, req.VM)
	if err != nil {
		log.Printf("[fleet/api] insert deployment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Run asynchronously so the HTTP response returns immediately.
	go func(vm config.VM, id int64) {
		_ = h.svc.DB().UpdateDeployment(id, "running", "")
		result := h.svc.Runner().Deploy(vm, req.Chain, req.Component, req.Script, req.DryRun, req.Env)
		status := "done"
		if result.Err != nil {
			status = "failed"
		}
		if err := h.svc.DB().UpdateDeployment(id, status, result.Output); err != nil {
			log.Printf("[fleet/api] update deployment %d: %v", id, err)
		}
	}(*vm, id)

	writeJSON(w, http.StatusAccepted, map[string]any{"deployment_id": id, "status": "running"})
}

// ── GET+POST+DELETE /api/v1/fleet/chains/registered ───────────────────────────

type registerRequest struct {
	Chain   string `json:"chain"`
	RPCURL  string `json:"rpc_url"`
	RESTURL string `json:"rest_url"`
	Note    string `json:"note"`
}

// HandleRegisteredChains handles GET (list) and POST (add) for registered chains.
func (h *Handlers) HandleRegisteredChains(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		chains, err := h.svc.DB().ListRegisteredChains()
		if err != nil {
			log.Printf("[fleet/api] list registered: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"registered_chains": chains})

	case http.MethodPost:
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Chain == "" || req.RPCURL == "" {
			http.Error(w, "chain and rpc_url required", http.StatusBadRequest)
			return
		}
		if err := h.svc.DB().AddRegisteredChain(req.Chain, req.RPCURL, req.RESTURL, req.Note); err != nil {
			log.Printf("[fleet/api] add registered chain: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "added", "chain": req.Chain})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleRegisteredChainDelete handles DELETE /api/v1/fleet/chains/registered/{chain}.
func (h *Handlers) HandleRegisteredChainDelete(w http.ResponseWriter, r *http.Request) {
	chain := r.PathValue("chain")
	if chain == "" {
		http.Error(w, "chain required", http.StatusBadRequest)
		return
	}
	if err := h.svc.DB().RemoveRegisteredChain(chain); err != nil {
		log.Printf("[fleet/api] remove registered chain: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.svc.RemoveStatus(chain)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "chain": chain})
}

// ── POST /api/v1/fleet/register ────────────────────────────────────────────────

type vmRegisterRequest struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	KeyPath    string `json:"key_path"`
	Datacenter string `json:"datacenter"`
	Type       string `json:"type"` // validator | sp | relayer
}

// HandleVMRegister handles POST /api/v1/fleet/register — VM self-registration.
func (h *Handlers) HandleVMRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req vmRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Host == "" {
		http.Error(w, "name and host required", http.StatusBadRequest)
		return
	}

	vm := config.VM{
		Name:       req.Name,
		Host:       req.Host,
		Port:       req.Port,
		User:       req.User,
		KeyPath:    req.KeyPath,
		Datacenter: req.Datacenter,
		Type:       req.Type,
	}

	h.svc.RegisterVM(vm)
	log.Printf("[fleet/api] VM %q registered from %s (type=%s)", req.Name, req.Host, req.Type)
	writeJSON(w, http.StatusOK, map[string]string{"status": "registered", "name": req.Name})
}

// ── helper ────────────────────────────────────────────────────────────────────

// HandlePoll triggers an immediate concurrent poll of all chains, waits up to
// 10 s for results, then returns the fresh status map.
func (h *Handlers) HandlePoll(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	h.svc.Poll(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"chains": h.svc.AllStatuses()})
}

// ProbeHostMap returns the set of hosts referenced by configured VMs and
// registered chains. Used by the dashboard probe API to authorize requests.
func (h *Handlers) ProbeHostMap() map[string]struct{} {
	if h == nil || h.svc == nil {
		return nil
	}
	hosts := make(map[string]struct{})

	addHost := func(raw string) {
		if host := parseProbeHost(raw); host != "" {
			hosts[strings.ToLower(host)] = struct{}{}
		}
	}

	cfg := h.svc.Config()
	if cfg != nil {
		for _, vm := range cfg.VMs {
			addHost(vm.Host)
			addHost(vm.LanIP)
			addHost(vm.PublicIP)
			addHost(vm.RPC())
			addHost(vm.REST())
			addHost(vm.DisplayLanIP())
		}
	}

	if regs, err := h.svc.DB().ListRegisteredChains(); err == nil {
		for _, rc := range regs {
			addHost(rc.RPCURL)
			addHost(rc.RESTURL)
		}
	}
	return hosts
}

func parseProbeHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			if host := u.Hostname(); host != "" {
				return host
			}
			if u.Host != "" {
				if host, _, err := net.SplitHostPort(u.Host); err == nil {
					return host
				}
				return u.Host
			}
		}
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	if idx := strings.IndexAny(raw, "/:"); idx >= 0 {
		return raw[:idx]
	}
	return raw
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[fleet/api] encode response: %v", err)
	}
}
