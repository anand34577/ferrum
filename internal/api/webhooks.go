package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ferrum/internal/events"
	"ferrum/internal/notify"
)

// webhookDTO is what the list/create/update endpoints return. Secret is
// included only right after creation (see createWebhook) — list responses
// never echo it back, the same convention apikeys.go uses for the plaintext
// key.
type webhookDTO struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Secret     string   `json:"secret,omitempty"`
	EventTypes []string `json:"eventTypes"`
	Active     bool     `json:"active"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
}

// knownEventTypes is the catalog surfaced to the settings UI for building a
// subscription's event-type picker; also used to validate a request's
// eventTypes.
var knownEventTypes = []events.Type{
	events.TypeAlertTriggered,
	events.TypeAlertResolved,
	events.TypeConnectionUp,
	events.TypeConnectionDown,
	events.TypeGuestPowerChanged,
	events.TypeTaskCompleted,
	events.TypeHAStatusChanged,
}

func validEventType(t string) bool {
	for _, known := range knownEventTypes {
		if string(known) == t {
			return true
		}
	}
	return false
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, name, url, event_types, active, created_at, updated_at FROM webhook_subscriptions ORDER BY created_at DESC`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []webhookDTO{}
	for rows.Next() {
		var dto webhookDTO
		var eventTypesJSON string
		var active int
		if err := rows.Scan(&dto.ID, &dto.Name, &dto.URL, &eventTypesJSON, &active, &dto.CreatedAt, &dto.UpdatedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		dto.Active = active == 1
		dto.EventTypes = decodeEventTypes(eventTypesJSON)
		out = append(out, dto)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func decodeEventTypes(raw string) []string {
	var types []string
	_ = json.Unmarshal([]byte(raw), &types) // malformed row → treat as "no filter" for display purposes
	return types
}

type webhookRequest struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	EventTypes []string `json:"eventTypes"`
	Active     *bool    `json:"active,omitempty"` // nil on create means true
}

func (req webhookRequest) validate() (string, bool) {
	if strings.TrimSpace(req.Name) == "" {
		return "name is required", false
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "url must be a valid http(s) URL", false
	}
	for _, t := range req.EventTypes {
		if !validEventType(t) {
			return "unknown event type: " + t, false
		}
	}
	return "", true
}

// generateWebhookSecret returns a random 32-byte hex string used as the
// HMAC signing key — generated server-side (like an API key) rather than
// admin-supplied, so it can't be a guessable/reused value.
func generateWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var req webhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}
	if msg, ok := req.validate(); !ok {
		writeErrorMsg(w, http.StatusBadRequest, msg)
		return
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	eventTypesJSON, err := json.Marshal(req.EventTypes)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	u := userFromContext(r)
	var createdBy *string
	if u != nil {
		createdBy = &u.ID
	}

	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO webhook_subscriptions (id, name, url, secret, event_types, active, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.URL, secret, string(eventTypesJSON), boolToInt(active), createdBy, now, now,
	); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "webhook.create", "settings", req.Name)
	writeJSON(w, http.StatusCreated, webhookDTO{
		ID: id, Name: req.Name, URL: req.URL, Secret: secret,
		EventTypes: req.EventTypes, Active: active, CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req webhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}
	if msg, ok := req.validate(); !ok {
		writeErrorMsg(w, http.StatusBadRequest, msg)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	eventTypesJSON, err := json.Marshal(req.EventTypes)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE webhook_subscriptions SET name = ?, url = ?, event_types = ?, active = ?, updated_at = ?
		WHERE id = ?`,
		req.Name, req.URL, string(eventTypesJSON), boolToInt(active), now, id,
	)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "webhook not found")
		return
	}
	s.audit(r, "webhook.update", "settings", req.Name)
	writeJSON(w, http.StatusOK, webhookDTO{
		ID: id, Name: req.Name, URL: req.URL,
		EventTypes: req.EventTypes, Active: active, UpdatedAt: now,
	})
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM webhook_subscriptions WHERE id = ?`, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "webhook not found")
		return
	}
	s.audit(r, "webhook.delete", "settings", id)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// testWebhook is POST /api/v1/settings/webhooks/{id}/test — delivers a
// synthetic events.TypeTest event to this one subscription immediately
// (bypassing its event-type filter and active flag, since testing an
// inactive subscription while setting it up is exactly the point) and
// reports whether the receiving endpoint accepted it.
func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeErrorMsg(w, http.StatusServiceUnavailable, "webhook dispatcher is not available")
		return
	}
	id := chi.URLParam(r, "id")
	var sub notify.WebhookSubscription
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, name, url, secret FROM webhook_subscriptions WHERE id = ?`, id,
	).Scan(&sub.ID, &sub.Name, &sub.URL, &sub.Secret)
	if err == sql.ErrNoRows {
		writeErrorMsg(w, http.StatusNotFound, "webhook not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	s.audit(r, "webhook.test", "settings", sub.Name)
	if err := s.webhooks.SendTestEvent(r.Context(), sub); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"delivered": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"delivered": true})
}

type webhookDeliveryDTO struct {
	ID         string `json:"id"`
	EventType  string `json:"eventType"`
	EventID    string `json:"eventId"`
	Attempt    int    `json:"attempt"`
	StatusCode *int   `json:"statusCode,omitempty"`
	Error      string `json:"error,omitempty"`
	Success    bool   `json:"success"`
	CreatedAt  string `json:"createdAt"`
}

// listWebhookDeliveries is GET /api/v1/settings/webhooks/{id}/deliveries —
// the last N attempts (see notify.deliveryLogRetainedRows) for one
// subscription, newest first, for diagnosing "why didn't my endpoint get
// the event".
func (s *Server) listWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var exists int
	if err := s.db.QueryRowContext(r.Context(), `SELECT 1 FROM webhook_subscriptions WHERE id = ?`, id).Scan(&exists); err == sql.ErrNoRows {
		writeErrorMsg(w, http.StatusNotFound, "webhook not found")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, event_type, event_id, attempt, status_code, error, success, created_at
		FROM webhook_deliveries WHERE subscription_id = ? ORDER BY created_at DESC`, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []webhookDeliveryDTO{}
	for rows.Next() {
		var d webhookDeliveryDTO
		var success int
		var errCol sql.NullString
		if err := rows.Scan(&d.ID, &d.EventType, &d.EventID, &d.Attempt, &d.StatusCode, &errCol, &success, &d.CreatedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		d.Success = success == 1
		if errCol.Valid {
			d.Error = errCol.String
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
