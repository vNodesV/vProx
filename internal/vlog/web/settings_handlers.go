package web

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/vNodesV/vProx/internal/configwizard"
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
