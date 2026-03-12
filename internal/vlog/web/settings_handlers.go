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
