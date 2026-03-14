package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/vNodesV/vProx/internal/configwizard"
	fleetcfg "github.com/vNodesV/vProx/internal/fleet/config"
	vopscfg "github.com/vNodesV/vProx/internal/vops/config"
)

type settingsData struct {
	pageBase
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, _ *http.Request) {
	data := settingsData{pageBase: s.newPageBase()}
	if err := s.pages["settings.html"].ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("[web] settings render: %v", err)
	}
}

func (s *Server) handleAPISettingsCurrent(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, configwizard.CurrentSnapshot(s.home, r.URL.Query().Get("mode")))
}

func (s *Server) handleAPISettingsImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	var req struct {
		Step string `json:"step"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}
	fields, normalizedPath, err := configwizard.ImportStepFieldsFromPath(req.Step, req.Path)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"step":        req.Step,
		"source_path": normalizedPath,
		"fields":      fields,
	})
}

func (s *Server) handleAPISettingsRemove(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	var req struct {
		Step   string `json:"step"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}
	if err := configwizard.RemoveStepEntry(s.home, req.Step, req.Target); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPISettingsApply(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	var req struct {
		Steps []string `json:"steps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	stepSet := make(map[string]struct{}, len(req.Steps))
	for _, raw := range req.Steps {
		step := strings.ToLower(strings.TrimSpace(raw))
		if step == "" {
			continue
		}
		stepSet[step] = struct{}{}
	}

	requires := make(map[string]struct{})
	softReloaded := make([]string, 0, 1)

	_, needsFleet := stepSet["fleet"]
	_, needsChain := stepSet["chain"]
	_, needsInfra := stepSet["infra"]
	if needsFleet || needsChain || needsInfra {
		if s.fleetSvc != nil {
			defs := fleetcfg.FleetDefaults{
				User:    s.cfg.VOps.Push.Defaults.User,
				KeyPath: s.cfg.VOps.Push.Defaults.KeyPath,
			}
			runtimeCfg, err := fleetcfg.LoadRuntimeConfig(s.home, defs, s.cfg.VOps.Push.ChainsDir, s.cfg.VOps.Push.InfraDir)
			if err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "fleet reload failed: " + err.Error()})
				return
			}
			s.fleetSvc.SetConfig(runtimeCfg)
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			s.fleetSvc.Poll(ctx)
			softReloaded = append(softReloaded, "fleet")
		} else {
			requires["vops"] = struct{}{}
		}
	}

	if _, ok := stepSet["vops"]; ok {
		requires["vops"] = struct{}{}
	}
	if _, ok := stepSet["backup"]; ok {
		requires["vops"] = struct{}{}
	}
	if _, ok := stepSet["ports"]; ok {
		requires["vprox"] = struct{}{}
	}
	if _, ok := stepSet["settings"]; ok {
		requires["vprox"] = struct{}{}
	}

	restartTargets := make([]string, 0, len(requires))
	for target := range requires {
		restartTargets = append(restartTargets, target)
	}
	sort.Strings(restartTargets)
	sort.Strings(softReloaded)

	message := "No runtime changes were applied."
	switch {
	case len(softReloaded) > 0 && len(restartTargets) == 0:
		message = "Settings applied with soft reload."
	case len(softReloaded) > 0 && len(restartTargets) > 0:
		message = "Fleet reloaded. Some modules still require a service restart."
	case len(restartTargets) > 0:
		message = "Changes saved. Service restart required to apply all updates."
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"applied_steps":    req.Steps,
		"soft_reloaded":    softReloaded,
		"requires_restart": restartTargets,
		"message":          message,
	})
}

func (s *Server) handleAPISettingsSave(step string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
			return
		}
		if err := configwizard.ApplyFields(s.home, step, fields); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleAPISettingsPreferences persists UI preferences (theme) to vops.toml,
// updates the in-memory config, and sets a vprox_theme cookie for flash-free load.
func (s *Server) handleAPISettingsPreferences(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req struct {
		Theme string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.Theme != "vnodes" && req.Theme != "dark-blue" && req.Theme != "light-blue" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown theme: must be vnodes, dark-blue, or light-blue"})
		return
	}

	// Load, patch, and write back vops.toml.
	cfgPath := filepath.Join(s.home, "config", "vops", "vops.toml")
	cfg, err := vopscfg.Load(cfgPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load vops.toml"})
		return
	}
	cfg.VOps.UI.Theme = req.Theme

	data, err := toml.Marshal(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not marshal config"})
		return
	}
	// Ensure parent directory exists (in case vops.toml doesn't exist yet).
	if mkErr := os.MkdirAll(filepath.Dir(cfgPath), 0o755); mkErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create config directory"})
		return
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not write vops.toml"})
		return
	}

	// Update in-memory config so next page render picks up the new theme.
	s.cfg.VOps.UI.Theme = req.Theme

	// Set a cookie for flash-free theme on page reload.
	http.SetCookie(w, &http.Cookie{
		Name:     "vprox_theme",
		Value:    req.Theme,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "theme": req.Theme})
}
