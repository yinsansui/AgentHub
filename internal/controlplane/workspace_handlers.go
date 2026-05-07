package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const defaultWorkspaceName = "Default 工作空间"

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r.Context())
	limit, _ := parseIntQuery(r, "limit", 100)
	offset, _ := parseIntQuery(r, "offset", 0)
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	workspaces, err := s.store.ListWorkspaces(r.Context(), userID, limit, offset)
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
	writeJSON(w, map[string]any{"workspaces": sanitizeWorkspacesForUser(userID, workspaces), "limit": limit, "offset": offset})
}

func (s *Server) ensureDefaultWorkspace(ctx context.Context) (WorkspaceProjection, error) {
	userID := currentUserID(ctx)
	workspace, err := s.store.CreateWorkspace(ctx, userID, defaultWorkspaceName)
	if err != nil {
		return WorkspaceProjection{}, err
	}
	return sanitizeWorkspaceForUser(userID, workspace), nil
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	name, err := decodeWorkspaceNameRequest(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.CreateWorkspace(r.Context(), currentUserID(r.Context()), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"workspace": sanitizeWorkspaceForUser(currentUserID(r.Context()), saved)})
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r.Context())
	workspaceID, err := workspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	workspace, ok, err := s.store.GetWorkspace(r.Context(), userID, workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"workspace": sanitizeWorkspaceForUser(userID, workspace)})
}

func (s *Server) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r.Context())
	workspaceID, err := workspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name, err := decodeWorkspaceNameRequest(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, ok, err := s.store.UpdateWorkspace(r.Context(), userID, workspaceID, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"workspace": sanitizeWorkspaceForUser(userID, saved)})
}

func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r.Context())
	workspaceID, err := workspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok, err := s.store.GetWorkspace(r.Context(), userID, workspaceID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	if _, ok := s.workspaceToken(r.Context(), workspaceID); ok {
		if err := s.driver.Stop(r.Context(), workspaceID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.driver.Remove(r.Context(), workspaceID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	deleted, err := s.store.DeleteWorkspace(r.Context(), userID, workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	s.clearToken(workspaceID)
	response := map[string]any{"deleted": true}
	remaining, err := s.store.ListWorkspaces(r.Context(), userID, 1, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(remaining) == 0 {
		replacement, err := s.ensureDefaultWorkspace(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		response["replacementWorkspace"] = replacement
	}
	writeJSON(w, response)
}

func decodeWorkspaceNameRequest(body io.Reader) (string, error) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(&fields); err != nil {
		if errors.Is(err, io.EOF) {
			return "", errors.New("name is required")
		}
		return "", err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return "", errors.New("request body must contain a single JSON object")
	}
	if fields == nil {
		return "", errors.New("request body must be a JSON object")
	}
	for field := range fields {
		if field != "name" {
			return "", errors.New("field " + field + " is not allowed")
		}
	}
	rawName, ok := fields["name"]
	if !ok {
		return "", errors.New("name is required")
	}
	var name string
	if err := json.Unmarshal(rawName, &name); err != nil {
		return "", errors.New("name must be a string")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	return name, nil
}
