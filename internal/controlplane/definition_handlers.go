package controlplane

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"agenthub/pkg/protocol"
)

type upsertWorkspaceSkillRequest struct {
	Name        string                    `json:"name,omitempty"`
	Description string                    `json:"description,omitempty"`
	Files       []upsertWorkspaceFileBody `json:"files"`
}

type upsertWorkspaceFileBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type upsertWorkspaceMCPServerRequest struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	Transport string            `json:"transport,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

func (s *Server) handleListWorkspaceSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := s.store.ListWorkspaceSkills(r.Context(), r.PathValue("workspaceId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"skills": skills})
}

func (s *Server) handleGetWorkspaceSkill(w http.ResponseWriter, r *http.Request) {
	skill, ok, err := s.store.GetWorkspaceSkill(r.Context(), r.PathValue("workspaceId"), r.PathValue("slug"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"skill": skill})
}

func (s *Server) handlePutWorkspaceSkill(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	slug := r.PathValue("slug")
	var req upsertWorkspaceSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	skill, err := buildWorkspaceSkillDefinition(workspaceID, slug, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.UpsertWorkspaceSkill(r.Context(), workspaceID, skill)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"skill": saved})
}

func (s *Server) handleDeleteWorkspaceSkill(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.store.DeleteWorkspaceSkill(r.Context(), r.PathValue("workspaceId"), r.PathValue("slug"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"deleted": true})
}

func (s *Server) handleListWorkspaceMCPServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.store.ListWorkspaceMCPServers(r.Context(), r.PathValue("workspaceId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"mcpServers": servers})
}

func (s *Server) handleGetWorkspaceMCPServer(w http.ResponseWriter, r *http.Request) {
	server, ok, err := s.store.GetWorkspaceMCPServer(r.Context(), r.PathValue("workspaceId"), r.PathValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "mcp server not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"mcpServer": server})
}

func (s *Server) handlePutWorkspaceMCPServer(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	name := r.PathValue("name")
	var req upsertWorkspaceMCPServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	server, err := buildWorkspaceMCPServerDefinition(workspaceID, name, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.UpsertWorkspaceMCPServer(r.Context(), workspaceID, server)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"mcpServer": saved})
}

func (s *Server) handleDeleteWorkspaceMCPServer(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.store.DeleteWorkspaceMCPServer(r.Context(), r.PathValue("workspaceId"), r.PathValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "mcp server not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"deleted": true})
}

func buildWorkspaceSkillDefinition(workspaceID, slug string, req upsertWorkspaceSkillRequest) (SkillDefinitionWithFiles, error) {
	if err := validateSafeSegment(slug, "skill slug"); err != nil {
		return SkillDefinitionWithFiles{}, err
	}
	if len(req.Files) == 0 {
		return SkillDefinitionWithFiles{}, errors.New("files are required")
	}
	files := make([]SkillFile, 0, len(req.Files))
	seenPaths := map[string]bool{}
	for _, file := range req.Files {
		path, err := cleanDefinitionFilePath(file.Path)
		if err != nil {
			return SkillDefinitionWithFiles{}, err
		}
		if seenPaths[path] {
			return SkillDefinitionWithFiles{}, errors.New("duplicate file path: " + path)
		}
		seenPaths[path] = true
		files = append(files, SkillFile{
			ID:          stableDefinitionID("skill_file", workspaceID, slug, path),
			Path:        path,
			Content:     file.Content,
			ContentHash: hashStrings(file.Content),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	definitionID := stableDefinitionID("skill", protocol.SkillSourceWorkspace, "workspace", workspaceID, slug)
	for idx := range files {
		files[idx].SkillID = definitionID
	}
	return SkillDefinitionWithFiles{
		Definition: SkillDefinition{
			ID:          definitionID,
			Slug:        slug,
			Source:      protocol.SkillSourceWorkspace,
			ScopeType:   "workspace",
			ScopeID:     workspaceID,
			Name:        strings.TrimSpace(req.Name),
			Description: strings.TrimSpace(req.Description),
			ContentHash: hashSkillDefinition(req.Name, req.Description, files),
		},
		Files: files,
	}, nil
}

func buildWorkspaceMCPServerDefinition(workspaceID, name string, req upsertWorkspaceMCPServerRequest) (MCPServerDefinitionWithEnv, error) {
	if err := validateSafeSegment(name, "mcp server name"); err != nil {
		return MCPServerDefinitionWithEnv{}, err
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return MCPServerDefinitionWithEnv{}, errors.New("command is required")
	}
	transport := strings.TrimSpace(req.Transport)
	if transport == "" {
		transport = "stdio"
	}
	serverID := stableDefinitionID("mcp", protocol.SkillSourceWorkspace, "workspace", workspaceID, name)
	envNames := make([]string, 0, len(req.Env))
	for envName := range req.Env {
		envNames = append(envNames, envName)
	}
	sort.Strings(envNames)
	env := make([]MCPServerEnv, 0, len(envNames))
	seenEnv := map[string]bool{}
	for _, envName := range envNames {
		trimmed := strings.TrimSpace(envName)
		if trimmed == "" {
			return MCPServerDefinitionWithEnv{}, errors.New("env name is required")
		}
		if seenEnv[trimmed] {
			return MCPServerDefinitionWithEnv{}, errors.New("duplicate env name: " + trimmed)
		}
		seenEnv[trimmed] = true
		env = append(env, MCPServerEnv{
			ID:       stableDefinitionID("mcp_env", serverID, trimmed),
			ServerID: serverID,
			Name:     trimmed,
			Value:    req.Env[envName],
		})
	}
	args := append([]string(nil), req.Args...)
	return MCPServerDefinitionWithEnv{
		Definition: MCPServerDefinition{
			ID:          serverID,
			Name:        name,
			Source:      protocol.SkillSourceWorkspace,
			ScopeType:   "workspace",
			ScopeID:     workspaceID,
			Command:     command,
			Args:        args,
			Transport:   transport,
			ContentHash: hashMCPDefinition(command, args, transport, env),
		},
		Env: env,
	}, nil
}

func validateSafeSegment(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New(field + " is required")
	}
	if value == "." || value == ".." || strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return errors.New(field + " must be a safe path segment")
	}
	return nil
}

func cleanDefinitionFilePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("file path is required")
	}
	if filepath.IsAbs(path) {
		return "", errors.New("file path must be relative")
	}
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == ".." {
			return "", errors.New("file path must stay inside the skill directory")
		}
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		return "", errors.New("file path must stay inside the skill directory")
	}
	return cleaned, nil
}

func stableDefinitionID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(sum[:])[:24]
}

func hashSkillDefinition(name, description string, files []SkillFile) string {
	parts := []string{strings.TrimSpace(name), strings.TrimSpace(description)}
	for _, file := range files {
		parts = append(parts, file.Path, file.ContentHash)
	}
	return hashStrings(parts...)
}

func hashMCPDefinition(command string, args []string, transport string, env []MCPServerEnv) string {
	parts := []string{command, transport}
	parts = append(parts, args...)
	for _, item := range env {
		parts = append(parts, item.Name, item.Value)
	}
	return hashStrings(parts...)
}

func hashStrings(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
