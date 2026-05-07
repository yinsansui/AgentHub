package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultLLMProvider    = "anthropic"
	defaultLLMAPIProtocol = "anthropic-messages"
	llmModelSourceManual  = "manual"
	llmModelSourceRemote  = "discovered"
)

type upsertWorkspaceLLMConnectionRequest struct {
	Provider       string  `json:"provider,omitempty"`
	APIProtocol    string  `json:"apiProtocol"`
	BaseURL        string  `json:"baseUrl"`
	APIKey         string  `json:"apiKey"`
	DefaultModelID *string `json:"defaultModelId,omitempty"`
}

type upsertWorkspaceLLMModelRequest struct {
	ModelID string         `json:"modelId"`
	Source  string         `json:"source,omitempty"`
	Enabled *bool          `json:"enabled,omitempty"`
	Raw     map[string]any `json:"raw,omitempty"`
}

func (s *Server) handleGetWorkspaceLLMConnection(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r.Context())
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	connection, ok, err := s.store.GetWorkspaceLLMConnection(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "llm connection not configured", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"connection": redactLLMConnection(sanitizeConnectionForUser(userID, connection)), "apiKeySet": connection.APIKey != ""})
}

func (s *Server) handlePutWorkspaceLLMConnection(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r.Context())
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	var req upsertWorkspaceLLMConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing, hasExisting, err := s.store.GetWorkspaceLLMConnection(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var existingConnection *LLMConnection
	if hasExisting {
		existingConnection = &existing
	}
	connection, err := buildWorkspaceLLMConnection(workspaceID, req, existingConnection)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.UpsertWorkspaceLLMConnection(r.Context(), workspaceID, connection)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"connection": redactLLMConnection(sanitizeConnectionForUser(userID, saved)), "apiKeySet": saved.APIKey != ""})
}

func (s *Server) handleListWorkspaceLLMModels(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	models, err := s.store.ListWorkspaceLLMModels(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"models": models})
}

func (s *Server) handlePutWorkspaceLLMModel(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	var req upsertWorkspaceLLMModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	model, err := buildWorkspaceLLMModel(workspaceID, "", req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.UpsertWorkspaceLLMModel(r.Context(), workspaceID, model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !saved.Enabled {
		connection, ok, err := s.store.GetWorkspaceLLMConnection(r.Context(), workspaceID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if ok && connection.DefaultModelID == saved.ModelID {
			connection.DefaultModelID = ""
			if _, err := s.store.UpsertWorkspaceLLMConnection(r.Context(), workspaceID, connection); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	writeJSON(w, map[string]any{"model": saved})
}

func (s *Server) handleRefreshWorkspaceLLMModels(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	connection, ok, err := s.store.GetWorkspaceLLMConnection(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "llm connection not configured", http.StatusConflict)
		return
	}
	discovered, err := fetchLLMModels(r.Context(), connection)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	existingModels, err := s.store.ListWorkspaceLLMModels(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	enabledByID := map[string]bool{}
	for _, model := range existingModels {
		enabledByID[model.ModelID] = model.Enabled
	}
	saved := make([]LLMConnectionModel, 0, len(discovered))
	now := time.Now().UTC()
	for _, item := range discovered {
		enabled := enabledByID[item.ModelID]
		model, err := buildWorkspaceLLMModel(workspaceID, connection.ID, upsertWorkspaceLLMModelRequest{
			ModelID: item.ModelID,
			Source:  llmModelSourceRemote,
			Enabled: &enabled,
			Raw:     item.Raw,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		model.LastSeenAt = &now
		stored, err := s.store.UpsertWorkspaceLLMModel(r.Context(), workspaceID, model)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		saved = append(saved, stored)
	}
	models, err := s.store.ListWorkspaceLLMModels(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"models": models})
}

func buildWorkspaceLLMConnection(workspaceID string, req upsertWorkspaceLLMConnectionRequest, existing *LLMConnection) (LLMConnection, error) {
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		if existing != nil && strings.TrimSpace(existing.Provider) != "" {
			provider = existing.Provider
		} else {
			provider = defaultLLMProvider
		}
	}
	apiProtocol := strings.TrimSpace(req.APIProtocol)
	if apiProtocol == "" {
		if existing != nil && strings.TrimSpace(existing.APIProtocol) != "" {
			apiProtocol = existing.APIProtocol
		} else {
			apiProtocol = defaultLLMAPIProtocol
		}
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		return LLMConnection{}, errors.New("baseUrl is required")
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" && existing != nil {
		apiKey = existing.APIKey
	}
	if apiKey == "" {
		return LLMConnection{}, errors.New("apiKey is required")
	}
	defaultModelID := ""
	if existing != nil {
		defaultModelID = existing.DefaultModelID
	}
	if req.DefaultModelID != nil {
		defaultModelID = strings.TrimSpace(*req.DefaultModelID)
	}
	return LLMConnection{
		ID:             stableDefinitionID("llm_conn", "", workspaceID),
		UserID:         "",
		WorkspaceID:    workspaceID,
		Provider:       provider,
		APIProtocol:    apiProtocol,
		BaseURL:        baseURL,
		APIKey:         apiKey,
		DefaultModelID: defaultModelID,
	}, nil
}

func buildWorkspaceLLMModel(workspaceID, connectionID string, req upsertWorkspaceLLMModelRequest) (LLMConnectionModel, error) {
	modelID := strings.TrimSpace(req.ModelID)
	if modelID == "" {
		return LLMConnectionModel{}, errors.New("modelId is required")
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = llmModelSourceManual
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	raw := req.Raw
	if raw == nil {
		raw = map[string]any{}
	}
	return LLMConnectionModel{
		ID:           stableDefinitionID("llm_model", "", workspaceID, modelID),
		ConnectionID: connectionID,
		ModelID:      modelID,
		Source:       source,
		Enabled:      enabled,
		Raw:          raw,
	}, nil
}

type discoveredLLMModel struct {
	ModelID string
	Raw     map[string]any
}

func fetchLLMModels(ctx context.Context, connection LLMConnection) ([]discoveredLLMModel, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(connection.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("baseUrl is required")
	}
	var lastErr error
	for _, endpoint := range modelListEndpoints(baseURL, connection.APIProtocol) {
		models, err := fetchLLMModelsFromEndpoint(ctx, connection, endpoint)
		if err == nil {
			return models, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("no model list endpoint configured")
}

func modelListEndpoints(baseURL, apiProtocol string) []string {
	endpoints := []string{baseURL + "/models"}
	if apiProtocol == "anthropic-messages" && !strings.HasSuffix(baseURL, "/v1") {
		endpoints = append(endpoints, baseURL+"/v1/models")
	}
	return endpoints
}

func fetchLLMModelsFromEndpoint(ctx context.Context, connection LLMConnection, endpoint string) ([]discoveredLLMModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if connection.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+connection.APIKey)
		req.Header.Set("x-api-key", connection.APIKey)
	}
	if connection.APIProtocol == "anthropic-messages" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, errors.New("models endpoint returned " + resp.Status + ": " + strings.TrimSpace(string(body)))
	}
	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return parseLLMModelsPayload(payload)
}

func parseLLMModelsPayload(payload any) ([]discoveredLLMModel, error) {
	var items []any
	switch typed := payload.(type) {
	case []any:
		items = typed
	case map[string]any:
		data, ok := typed["data"].([]any)
		if !ok {
			return nil, errors.New("models response must contain data array")
		}
		items = data
	default:
		return nil, errors.New("models response must be an array or object")
	}
	models := make([]discoveredLLMModel, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		modelID := firstString(raw, "id", "model", "name")
		if modelID == "" {
			continue
		}
		models = append(models, discoveredLLMModel{ModelID: modelID, Raw: raw})
	}
	return models, nil
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func redactLLMConnection(connection LLMConnection) LLMConnection {
	connection.APIKey = ""
	return connection
}
