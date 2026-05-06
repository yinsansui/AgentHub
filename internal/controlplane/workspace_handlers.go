package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type upsertWorkspaceRequest struct {
	WorkspaceID string         `json:"workspaceId,omitempty"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

const (
	defaultWorkspaceID   = "ws_dev"
	defaultWorkspaceName = "Default 工作空间"
)

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	limit, _ := parseIntQuery(r, "limit", 100)
	offset, _ := parseIntQuery(r, "offset", 0)
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	workspaces, err := s.store.ListWorkspaces(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if offset == 0 && len(workspaces) == 0 {
		workspace, err := s.ensureDefaultWorkspace(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		workspaces = []WorkspaceProjection{workspace}
	}
	if workspaces == nil {
		workspaces = []WorkspaceProjection{}
	}
	writeJSON(w, map[string]any{"workspaces": workspaces, "limit": limit, "offset": offset})
}

func (s *Server) ensureDefaultWorkspace(ctx context.Context) (WorkspaceProjection, error) {
	workspace := WorkspaceProjection{
		WorkspaceID: defaultWorkspaceID,
		Name:        defaultWorkspaceName,
		Metadata:    map[string]any{},
	}
	saved, err := s.store.CreateWorkspace(ctx, workspace)
	if err == nil {
		return saved, nil
	}
	if !errors.Is(err, ErrWorkspaceExists) {
		return WorkspaceProjection{}, err
	}
	existing, ok, err := s.store.GetWorkspace(ctx, defaultWorkspaceID)
	if err != nil {
		return WorkspaceProjection{}, err
	}
	if !ok {
		return WorkspaceProjection{}, ErrWorkspaceExists
	}
	return existing, nil
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req upsertWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	workspace, err := buildWorkspaceProjection(strings.TrimSpace(req.WorkspaceID), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.CreateWorkspace(r.Context(), workspace)
	if errors.Is(err, ErrWorkspaceExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"workspace": saved})
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	workspace, ok, err := s.store.GetWorkspace(r.Context(), r.PathValue("workspaceId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"workspace": workspace})
}

func (s *Server) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	var req upsertWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspaceID != "" && req.WorkspaceID != workspaceID {
		http.Error(w, "workspaceId in body must match path", http.StatusBadRequest)
		return
	}
	workspace, err := buildWorkspaceProjection(workspaceID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, ok, err := s.store.UpdateWorkspace(r.Context(), workspaceID, workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"workspace": saved})
}

func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.store.DeleteWorkspace(r.Context(), r.PathValue("workspaceId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"deleted": true})
}

func buildWorkspaceProjection(workspaceID string, req upsertWorkspaceRequest) (WorkspaceProjection, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if err := validateSafeSegment(workspaceID, "workspaceId"); err != nil {
		return WorkspaceProjection{}, err
	}
	metadata := req.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	return WorkspaceProjection{
		WorkspaceID: workspaceID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Metadata:    cloneMetadata(metadata),
	}, nil
}
