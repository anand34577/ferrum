package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"ferrum/internal/digest"
)

type digestSettingsResponse struct {
	Enabled       bool     `json:"enabled"`
	IntervalHours int      `json:"intervalHours"`
	Recipients    []string `json:"recipients"`
	LastSentAt    string   `json:"lastSentAt,omitempty"`
}

func toDigestSettingsResponse(s digest.Settings) digestSettingsResponse {
	resp := digestSettingsResponse{
		Enabled: s.Enabled, IntervalHours: s.IntervalHours, Recipients: s.Recipients,
	}
	if resp.Recipients == nil {
		resp.Recipients = []string{}
	}
	if !s.LastSentAt.IsZero() {
		resp.LastSentAt = s.LastSentAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return resp
}

func (s *Server) getDigestSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := digest.LoadSettings(r.Context(), s.db)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toDigestSettingsResponse(settings))
}

type digestSettingsPatch struct {
	Enabled       *bool     `json:"enabled"`
	IntervalHours *int      `json:"intervalHours"`
	Recipients    *[]string `json:"recipients"`
}

func (s *Server) putDigestSettings(w http.ResponseWriter, r *http.Request) {
	var patch digestSettingsPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	settings, err := digest.LoadSettings(r.Context(), s.db)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	if patch.IntervalHours != nil {
		// One day to one year — anything shorter is spam, anything longer
		// isn't really "periodic" anymore.
		if *patch.IntervalHours < 24 || *patch.IntervalHours > 8760 {
			writeErrorMsg(w, http.StatusBadRequest, "digest interval must be between 24 and 8760 hours")
			return
		}
		settings.IntervalHours = *patch.IntervalHours
	}
	if patch.Recipients != nil {
		cleaned := make([]string, 0, len(*patch.Recipients))
		for _, addr := range *patch.Recipients {
			if addr = strings.TrimSpace(addr); addr != "" {
				cleaned = append(cleaned, addr)
			}
		}
		settings.Recipients = cleaned
	}
	if patch.Enabled != nil {
		settings.Enabled = *patch.Enabled
	}

	if err := digest.SaveSettings(r.Context(), s.db, settings); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "settings.digest", "settings", "updated")
	writeJSON(w, http.StatusOK, toDigestSettingsResponse(settings))
}

// sendDigestNow triggers an immediate digest send — useful both to verify
// delivery is actually configured correctly and to get an off-cycle summary
// on demand. Reuses the same Scheduler the periodic job runs on, so this is
// exactly the message a scheduled run would have sent.
func (s *Server) sendDigestNow(w http.ResponseWriter, r *http.Request) {
	if s.digestScheduler == nil {
		writeErrorMsg(w, http.StatusServiceUnavailable, "digest scheduler is not running")
		return
	}
	if err := s.digestScheduler.SendNow(r.Context()); err != nil {
		writeErrorMsg(w, http.StatusBadGateway, err.Error())
		return
	}
	s.audit(r, "settings.digest.send_now", "settings", "sent")
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}
