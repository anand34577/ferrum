package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ferrum/internal/needle"
)

// seedBuiltinNeedleProvider makes sure the built-in Needle 2 provider exists
// and is enabled on every startup where a binary is available for the
// running platform (bundled — see internal/needle's go:embed — or
// FERRUM_NEEDLE_BIN), and — only if nothing else has been chosen yet — makes
// its model the initial default so a fresh install has a working assistant
// with zero setup. It is idempotent (matches the existing row by
// base_url = needle.BaseURL rather than inserting a duplicate on every
// restart). It deliberately does NOT re-assert the default on every restart
// once an admin has picked one: doing so silently demoted an explicitly
// configured external provider (e.g. OpenAI) back to the local model on
// every server restart, which is not what "built-in fallback" should mean.
func (s *Server) seedBuiltinNeedleProvider(ctx context.Context) {
	if !s.needle.Available() {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var providerID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM ai_providers WHERE base_url = ?`, needle.BaseURL).Scan(&providerID)
	switch {
	case err == sql.ErrNoRows:
		var dismissed string
		if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, needleProviderDismissedKey).Scan(&dismissed); err == nil && dismissed == "1" {
			return // an admin explicitly deleted it — stay deleted across restarts
		}
		providerID = uuid.NewString()
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO ai_providers (id, name, base_url, api_key_enc, model, is_enabled, is_default, created_at, updated_at)
			VALUES (?, 'Needle 2 (built-in, local)', ?, NULL, '', 1, 0, ?, ?)`,
			providerID, needle.BaseURL, now, now,
		); err != nil {
			slog.Error("seeding built-in needle provider", "error", err)
			return
		}
	case err != nil:
		slog.Error("looking up built-in needle provider", "error", err)
		return
	default:
		// Already registered from a previous run — make sure it's still
		// enabled even if an operator had switched it off.
		if _, err := s.db.ExecContext(ctx, `UPDATE ai_providers SET is_enabled = 1, updated_at = ? WHERE id = ?`, now, providerID); err != nil {
			slog.Error("re-enabling built-in needle provider", "error", err)
		}
	}

	var modelID string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM ai_provider_models WHERE provider_id = ?`, providerID).Scan(&modelID)
	switch {
	case err == sql.ErrNoRows:
		modelID = uuid.NewString()
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO ai_provider_models (id, provider_id, label, model_id, is_default, created_at)
			VALUES (?, ?, 'needle2', 'needle2', 1, ?)`,
			modelID, providerID, now,
		); err != nil {
			slog.Error("seeding built-in needle model", "error", err)
			return
		}
	case err != nil:
		slog.Error("looking up built-in needle model", "error", err)
		return
	}

	// Only claim the default slot if nothing holds it yet (fresh install, or
	// the previous default model row was deleted) — never override a default
	// an admin already set, including on every subsequent restart.
	var anyDefault int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_provider_models WHERE is_default = 1`).Scan(&anyDefault); err != nil {
		slog.Error("checking for an existing default AI model", "error", err)
		return
	}
	if anyDefault > 0 {
		return
	}
	if err := s.clearOtherDefaultModels(ctx, modelID); err != nil {
		slog.Error("clearing other default AI models", "error", err)
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE ai_provider_models SET is_default = 1 WHERE id = ?`, modelID); err != nil {
		slog.Error("setting built-in needle model as default", "error", err)
	}
}

// aiModelDTO is one selectable model under a provider. label is the
// human-facing name shown in pickers; modelID is the exact identifier sent
// to the provider's API — kept distinct since they're frequently different
// (a friendly "GPT-4o mini" vs the wire value "gpt-4o-mini", or a local
// runtime tag like "qwen2.5-coder:7b-instruct-q4_K_M").
type aiModelDTO struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	ModelID   string `json:"modelId"`
	IsDefault bool   `json:"isDefault"`
	CreatedAt string `json:"createdAt"`
}

// aiProviderDTO is what the Settings UI sees — the API key is never
// returned, only whether one is stored (hasApiKey). A provider can carry any
// number of models (Models), added/edited/removed independently of the
// provider's own connection details.
type aiProviderDTO struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	BaseURL   string       `json:"baseUrl"`
	HasAPIKey bool         `json:"hasApiKey"`
	IsEnabled bool         `json:"isEnabled"`
	Models    []aiModelDTO `json:"models"`
	CreatedAt string       `json:"createdAt"`
	UpdatedAt string       `json:"updatedAt"`
}

// usableProviderDTO/usableModelDTO are the trimmed shapes exposed to every
// authenticated user (not just admins) on GET /ai/providers — enough to
// populate the assistant's provider/model picker, nothing sensitive, and
// only from providers/models that are actually usable right now.
type usableModelDTO struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	IsDefault bool   `json:"isDefault"`
}
type usableProviderDTO struct {
	ProviderID   string           `json:"providerId"`
	ProviderName string           `json:"providerName"`
	Models       []usableModelDTO `json:"models"`
}

func (s *Server) loadModelsFor(ctx context.Context, providerID string) ([]aiModelDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, label, model_id, is_default, created_at FROM ai_provider_models
		WHERE provider_id = ? ORDER BY created_at`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := []aiModelDTO{}
	for rows.Next() {
		var m aiModelDTO
		var isDefault int
		if err := rows.Scan(&m.ID, &m.Label, &m.ModelID, &isDefault, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.IsDefault = isDefault == 1
		models = append(models, m)
	}
	return models, rows.Err()
}

func (s *Server) listAIProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, base_url, api_key_enc, is_enabled, created_at, updated_at
		FROM ai_providers ORDER BY name`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := []aiProviderDTO{}
	for rows.Next() {
		var p aiProviderDTO
		var apiKeyEnc sql.NullString
		var enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.BaseURL, &apiKeyEnc, &enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		p.HasAPIKey = apiKeyEnc.Valid && apiKeyEnc.String != ""
		p.IsEnabled = enabled == 1
		out = append(out, p)
	}
	for i := range out {
		models, err := s.loadModelsFor(r.Context(), out[i].ID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		out[i].Models = models
	}
	writeJSON(w, http.StatusOK, out)
}

// listUsableAIProviders is the non-admin-gated counterpart used by the AI
// Assistant page to populate its picker — enabled providers and their
// models only.
func (s *Server) listUsableAIProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT p.id, p.name, m.id, m.label, m.is_default
		FROM ai_providers p JOIN ai_provider_models m ON m.provider_id = p.id
		WHERE p.is_enabled = 1 ORDER BY p.name, m.created_at`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	byProvider := map[string]*usableProviderDTO{}
	order := []string{}
	for rows.Next() {
		var providerID, providerName, modelID, label string
		var isDefault int
		if err := rows.Scan(&providerID, &providerName, &modelID, &label, &isDefault); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		p, ok := byProvider[providerID]
		if !ok {
			p = &usableProviderDTO{ProviderID: providerID, ProviderName: providerName, Models: []usableModelDTO{}}
			byProvider[providerID] = p
			order = append(order, providerID)
		}
		p.Models = append(p.Models, usableModelDTO{ID: modelID, Label: label, IsDefault: isDefault == 1})
	}

	out := make([]usableProviderDTO, 0, len(order))
	for _, id := range order {
		out = append(out, *byProvider[id])
	}
	writeJSON(w, http.StatusOK, out)
}

type aiProviderRequest struct {
	Name      string `json:"name"`
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey,omitempty"`
	IsEnabled *bool  `json:"isEnabled,omitempty"`
}

func validateAIProviderRequest(name, baseURL string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if needle.IsBuiltin(baseURL) {
		return nil
	}
	if baseURL == "" || (!strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://")) {
		return fmt.Errorf("baseUrl must start with http:// or https://")
	}
	return nil
}

func (s *Server) createAIProvider(w http.ResponseWriter, r *http.Request) {
	var req aiProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}
	if err := validateAIProviderRequest(req.Name, req.BaseURL); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, err.Error())
		return
	}

	var apiKeyEnc sql.NullString
	if req.APIKey != "" {
		enc, err := s.secrets.Encrypt(req.APIKey)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		apiKeyEnc = sql.NullString{String: enc, Valid: true}
	}
	enabled := true
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}

	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO ai_providers (id, name, base_url, api_key_enc, model, is_enabled, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, 0, ?, ?)`,
		id, req.Name, req.BaseURL, apiKeyEnc, boolToInt(enabled), now, now,
	); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.audit(r, "ai.provider.create", "settings", req.Name)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) updateAIProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req aiProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	sets := []string{}
	args := []any{}
	set := func(col string, val any) {
		sets = append(sets, col+" = ?")
		args = append(args, val)
	}

	if req.Name != "" {
		set("name", req.Name)
	}
	if req.BaseURL != "" {
		if !needle.IsBuiltin(req.BaseURL) && !strings.HasPrefix(req.BaseURL, "http://") && !strings.HasPrefix(req.BaseURL, "https://") {
			writeErrorMsg(w, http.StatusBadRequest, "baseUrl must start with http:// or https://")
			return
		}
		set("base_url", req.BaseURL)
	}
	if req.APIKey != "" {
		enc, err := s.secrets.Encrypt(req.APIKey)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		set("api_key_enc", enc)
	}
	if req.IsEnabled != nil {
		set("is_enabled", boolToInt(*req.IsEnabled))
	}
	if len(sets) == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "no fields to update")
		return
	}
	set("updated_at", time.Now().UTC().Format(time.RFC3339))

	query := "UPDATE ai_providers SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	args = append(args, id)
	res, err := s.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "ai provider not found")
		return
	}
	s.audit(r, "ai.provider.update", "settings", id)
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// needleProviderDismissedKey persists that an admin explicitly deleted the
// built-in Needle 2 provider, so seedBuiltinNeedleProvider doesn't silently
// resurrect it on the next server restart — deleting it should stay deleted,
// same as any other provider, not just until the process restarts.
const needleProviderDismissedKey = "ai.needle_provider_dismissed"

func (s *Server) deleteAIProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var baseURL string
	if err := s.db.QueryRowContext(r.Context(), `SELECT base_url FROM ai_providers WHERE id = ?`, id).Scan(&baseURL); err != nil && err != sql.ErrNoRows {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	res, err := s.db.ExecContext(r.Context(), `DELETE FROM ai_providers WHERE id = ?`, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "ai provider not found")
		return
	}

	if needle.IsBuiltin(baseURL) {
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := s.db.ExecContext(r.Context(),
			`INSERT INTO settings (key, value, updated_at) VALUES (?, '1', ?)
			 ON CONFLICT(key) DO UPDATE SET value = '1', updated_at = excluded.updated_at`,
			needleProviderDismissedKey, now,
		); err != nil {
			slog.Error("recording needle provider dismissal", "error", err)
		}
	}

	s.audit(r, "ai.provider.delete", "settings", id)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// --- models (child rows of a provider) ---

type aiModelRequest struct {
	Label     string `json:"label"`
	ModelID   string `json:"modelId"`
	IsDefault *bool  `json:"isDefault,omitempty"`
}

// clearOtherDefaultModels unsets is_default on every other model so at most
// one stays the global default — enforced here rather than a DB constraint,
// same spirit as the rest of Ferrum's settings handlers.
func (s *Server) clearOtherDefaultModels(ctx context.Context, exceptID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ai_provider_models SET is_default = 0 WHERE id != ?`, exceptID)
	return err
}

func (s *Server) addAIModel(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")
	var req aiModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}
	if req.Label == "" || req.ModelID == "" {
		writeErrorMsg(w, http.StatusBadRequest, "label and modelId are required")
		return
	}

	var exists int
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM ai_providers WHERE id = ?`, providerID).Scan(&exists); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if exists == 0 {
		writeErrorMsg(w, http.StatusNotFound, "ai provider not found")
		return
	}

	isDefault := req.IsDefault != nil && *req.IsDefault
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO ai_provider_models (id, provider_id, label, model_id, is_default, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, providerID, req.Label, req.ModelID, boolToInt(isDefault), now,
	); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if isDefault {
		if err := s.clearOtherDefaultModels(r.Context(), id); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.audit(r, "ai.model.create", "settings", req.Label)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) updateAIModel(w http.ResponseWriter, r *http.Request) {
	modelID := chi.URLParam(r, "modelId")
	var req aiModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}

	sets := []string{}
	args := []any{}
	set := func(col string, val any) {
		sets = append(sets, col+" = ?")
		args = append(args, val)
	}
	if req.Label != "" {
		set("label", req.Label)
	}
	if req.ModelID != "" {
		set("model_id", req.ModelID)
	}
	if req.IsDefault != nil {
		set("is_default", boolToInt(*req.IsDefault))
	}
	if len(sets) == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "no fields to update")
		return
	}

	query := "UPDATE ai_provider_models SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	args = append(args, modelID)
	res, err := s.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "model not found")
		return
	}
	if req.IsDefault != nil && *req.IsDefault {
		if err := s.clearOtherDefaultModels(r.Context(), modelID); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.audit(r, "ai.model.update", "settings", modelID)
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

func (s *Server) deleteAIModel(w http.ResponseWriter, r *http.Request) {
	modelID := chi.URLParam(r, "modelId")
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM ai_provider_models WHERE id = ?`, modelID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "model not found")
		return
	}
	s.audit(r, "ai.model.delete", "settings", modelID)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// loadModelForChat resolves a model row into everything the chat/test
// pipeline needs: the provider's connection details plus the exact model_id
// string to send upstream. Only returns rows for enabled providers.
func (s *Server) loadModelForChat(ctx context.Context, modelRowID string) (baseURL, apiKey, modelID string, err error) {
	var apiKeyEnc sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT p.base_url, p.api_key_enc, m.model_id
		FROM ai_provider_models m JOIN ai_providers p ON p.id = m.provider_id
		WHERE m.id = ? AND p.is_enabled = 1`, modelRowID).
		Scan(&baseURL, &apiKeyEnc, &modelID)
	if err != nil {
		return "", "", "", err
	}
	if apiKeyEnc.Valid && apiKeyEnc.String != "" {
		if apiKey, err = s.secrets.Decrypt(apiKeyEnc.String); err != nil {
			return "", "", "", err
		}
	}
	return baseURL, apiKey, modelID, nil
}

// defaultModelRowID returns the single global default model — the one used
// when a chat request doesn't specify which model to use.
func (s *Server) defaultModelRowID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT m.id FROM ai_provider_models m JOIN ai_providers p ON p.id = m.provider_id
		WHERE m.is_default = 1 AND p.is_enabled = 1 LIMIT 1`).Scan(&id)
	return id, err
}

// modelListResponse is the subset of the OpenAI-compatible GET /models
// response shape every provider in scope (OpenAI, LocalAI, LM Studio,
// Ollama's OpenAI shim, OpenRouter, ...) implements.
type modelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type providerTestResult struct {
	OK     bool     `json:"ok"`
	Via    string   `json:"via"`
	Models []string `json:"models,omitempty"`
}

// testProviderConnection is a lightweight reachability + model-discovery
// check shared by both the by-id test (an already-saved provider) and the
// ad-hoc test (the Settings dialog, before the provider is even saved): try
// the OpenAI-compatible GET /models endpoint first — which also lets the
// dialog populate a model picker instead of a free-text field — and fall
// back to a minimal 1-token chat completion for runtimes that only
// implement /chat/completions and don't expose /models at all.
func (s *Server) testProviderConnection(ctx context.Context, baseURL, apiKey, model string) (providerTestResult, error) {
	if needle.IsBuiltin(baseURL) {
		models, err := s.needle.TestConnection(ctx)
		if err != nil {
			return providerTestResult{}, err
		}
		return providerTestResult{OK: true, Via: "needle", Models: models}, nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	base := strings.TrimRight(baseURL, "/")

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if resp, err := client.Do(req); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 300 {
			var parsed modelListResponse
			models := []string{}
			if json.NewDecoder(resp.Body).Decode(&parsed) == nil {
				for _, m := range parsed.Data {
					if m.ID != "" {
						models = append(models, m.ID)
					}
				}
				sort.Strings(models)
			}
			return providerTestResult{OK: true, Via: "models", Models: models}, nil
		}
	}

	// Fall back to a trivial chat completion — needs a model name to send.
	if model == "" {
		return providerTestResult{}, fmt.Errorf("could not reach %s/models, and no model name was given to try a chat completion instead", base)
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
	})
	chatReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	chatReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		chatReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	chatResp, err := client.Do(chatReq)
	if err != nil {
		return providerTestResult{}, fmt.Errorf("could not reach provider: %w", err)
	}
	defer chatResp.Body.Close()
	if chatResp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(chatResp.Body, 2<<10))
		return providerTestResult{}, fmt.Errorf("provider returned %d: %s", chatResp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return providerTestResult{OK: true, Via: "chat/completions"}, nil
}

// testAIProvider re-tests an already-saved provider by ID, using one of its
// configured models (if any) for the chat-completion fallback path.
func (s *Server) testAIProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var baseURL string
	var apiKeyEnc sql.NullString
	if err := s.db.QueryRowContext(r.Context(), `SELECT base_url, api_key_enc FROM ai_providers WHERE id = ? AND is_enabled = 1`, id).
		Scan(&baseURL, &apiKeyEnc); err != nil {
		writeErrorMsg(w, http.StatusNotFound, "ai provider not found or disabled")
		return
	}
	var apiKey string
	if apiKeyEnc.Valid && apiKeyEnc.String != "" {
		var err error
		if apiKey, err = s.secrets.Decrypt(apiKeyEnc.String); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	var sampleModel string
	_ = s.db.QueryRowContext(r.Context(), `SELECT model_id FROM ai_provider_models WHERE provider_id = ? LIMIT 1`, id).Scan(&sampleModel)

	result, err := s.testProviderConnection(r.Context(), baseURL, apiKey, sampleModel)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type adHocTestRequest struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey,omitempty"`
	Model   string `json:"model,omitempty"`
}

// testAIProviderAdHoc lets the "Add provider" dialog verify reachability and
// discover available models *before* the provider is saved — the same flow
// tools like Open WebUI offer, so admins don't have to already know their
// runtime's exact model name.
func (s *Server) testAIProviderAdHoc(w http.ResponseWriter, r *http.Request) {
	var req adHocTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}
	if !needle.IsBuiltin(req.BaseURL) && (req.BaseURL == "" || (!strings.HasPrefix(req.BaseURL, "http://") && !strings.HasPrefix(req.BaseURL, "https://"))) {
		writeErrorMsg(w, http.StatusBadRequest, "baseUrl must start with http:// or https://")
		return
	}
	result, err := s.testProviderConnection(r.Context(), req.BaseURL, req.APIKey, req.Model)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
