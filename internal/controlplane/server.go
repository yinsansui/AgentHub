package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"agenthub/internal/driver"
	dockerdriver "agenthub/internal/driver/docker"
)

type Server struct {
	config Config
	driver driver.Driver
	pods   *AgentPodClient
	store  EventStore
	hub    *EventHub

	mu           sync.RWMutex
	tokens       map[string]string
	authSessions map[string]AuthSession
}

func NewServer(config Config) *Server {
	driverConfig := driver.Config{
		DockerSocket:  config.DockerSocket,
		DockerNetwork: config.DockerNetwork,
		AgentPodImage: config.AgentPodImage,
		WorkspaceRoot: config.WorkspaceRoot,
	}
	server := &Server{
		config:       config,
		driver:       dockerdriver.NewDockerAgentPodDriver(driverConfig),
		pods:         NewAgentPodClient(config.AgentPodBaseURLTemplate),
		hub:          NewEventHub(),
		tokens:       map[string]string{},
		authSessions: map[string]AuthSession{},
	}
	if config.DatabaseURL == "" {
		server.store = NewMemoryStore()
	} else {
		store, err := NewStore(context.Background(), config.DatabaseURL)
		if err != nil {
			panic(err)
		}
		server.store = store
	}
	return server
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("GET /auth/me", s.handleMe)
	mux.HandleFunc("GET /workspaces", s.handleListWorkspaces)
	mux.HandleFunc("POST /workspaces", s.handleCreateWorkspace)
	mux.HandleFunc("GET /workspaces/{workspaceId}", s.handleGetWorkspace)
	mux.HandleFunc("PUT /workspaces/{workspaceId}", s.handleUpdateWorkspace)
	mux.HandleFunc("DELETE /workspaces/{workspaceId}", s.handleDeleteWorkspace)
	mux.HandleFunc("POST /workspaces/{workspaceId}/stop", s.handleStopWorkspace)
	mux.HandleFunc("GET /workspaces/{workspaceId}/pod", s.handleInspectWorkspace)
	mux.HandleFunc("GET /workspaces/{workspaceId}/logs", s.handleWorkspaceLogs)
	mux.HandleFunc("GET /workspaces/{workspaceId}/llm-connection", s.handleGetWorkspaceLLMConnection)
	mux.HandleFunc("PUT /workspaces/{workspaceId}/llm-connection", s.handlePutWorkspaceLLMConnection)
	mux.HandleFunc("GET /workspaces/{workspaceId}/llm-models", s.handleListWorkspaceLLMModels)
	mux.HandleFunc("PUT /workspaces/{workspaceId}/llm-models", s.handlePutWorkspaceLLMModel)
	mux.HandleFunc("POST /workspaces/{workspaceId}/llm-models:refresh", s.handleRefreshWorkspaceLLMModels)
	mux.HandleFunc("GET /workspaces/{workspaceId}/skills", s.handleListWorkspaceSkills)
	mux.HandleFunc("PUT /workspaces/{workspaceId}/skills/{slug}", s.handlePutWorkspaceSkill)
	mux.HandleFunc("GET /workspaces/{workspaceId}/skills/{slug}", s.handleGetWorkspaceSkill)
	mux.HandleFunc("DELETE /workspaces/{workspaceId}/skills/{slug}", s.handleDeleteWorkspaceSkill)
	mux.HandleFunc("GET /workspaces/{workspaceId}/mcp-servers", s.handleListWorkspaceMCPServers)
	mux.HandleFunc("PUT /workspaces/{workspaceId}/mcp-servers/{name}", s.handlePutWorkspaceMCPServer)
	mux.HandleFunc("GET /workspaces/{workspaceId}/mcp-servers/{name}", s.handleGetWorkspaceMCPServer)
	mux.HandleFunc("DELETE /workspaces/{workspaceId}/mcp-servers/{name}", s.handleDeleteWorkspaceMCPServer)
	mux.HandleFunc("GET /workspaces/{workspaceId}/plugins", s.handleListWorkspacePlugins)
	mux.HandleFunc("PUT /workspaces/{workspaceId}/plugins/{pluginId}/install", s.handlePutWorkspacePluginInstall)
	mux.HandleFunc("GET /workspaces/{workspaceId}/sessions", s.handleListWorkspaceSessions)
	mux.HandleFunc("POST /workspaces/{workspaceId}/sessions", s.handleCreateWorkspaceSession)
	mux.HandleFunc("POST /sessions/{sessionId}/turns", s.handleCreateSessionTurn)
	mux.HandleFunc("GET /sessions/{sessionId}/events", s.handleSessionEvents)
	mux.HandleFunc("GET /sessions/{sessionId}/stream", s.handleSessionStream)
	mux.HandleFunc("GET /sessions/{sessionId}/messages", s.handleSessionMessages)
	mux.HandleFunc("GET /sessions/{sessionId}/state", s.handleSessionState)
	mux.HandleFunc("DELETE /sessions/{sessionId}", s.handleDeleteSession)
	mux.HandleFunc("POST /sessions/{sessionId}/interrupt", s.handleSessionInterrupt)
	return s.withAuth(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if s.config.DatabaseURL != "" {
		if err := s.store.Ping(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) ensureWorkspaceStarted(ctx context.Context, workspaceID string) (string, driver.AgentPodInfo, error) {
	req := driver.AgentPodSpec{WorkspaceID: workspaceID}
	if req.Token == "" {
		token, err := driver.NewToken()
		if err != nil {
			return "", driver.AgentPodInfo{}, err
		}
		req.Token = token
	}
	info, err := s.driver.Start(ctx, req)
	if err != nil {
		return "", driver.AgentPodInfo{}, err
	}
	if s.config.DatabaseURL != "" {
		if err := s.store.SaveWorkspaceToken(ctx, workspaceID, req.Token); err != nil {
			return "", driver.AgentPodInfo{}, err
		}
	} else {
		s.setToken(workspaceID, req.Token)
	}
	return req.Token, info, nil
}

func (s *Server) handleStopWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	if err := s.driver.Stop(r.Context(), workspaceID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"stopped": true, "workspaceId": workspaceID})
}

func (s *Server) handleInspectWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	info, err := s.driver.Inspect(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	info.WorkspaceID = workspaceID
	writeJSON(w, info)
}

func (s *Server) handleWorkspaceLogs(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	logs, err := s.driver.Logs(r.Context(), workspaceID, r.URL.Query().Get("tail"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(logs)
}

func (s *Server) setToken(workspaceID, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[workspaceID] = token
}

func (s *Server) clearToken(workspaceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, workspaceID)
}

func (s *Server) workspaceToken(ctx context.Context, workspaceID string) (string, bool) {
	if s.config.DatabaseURL != "" {
		token, ok, err := s.store.WorkspaceToken(ctx, workspaceID)
		if err != nil || !ok {
			return "", false
		}
		return token, true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.tokens[workspaceID]
	return token, ok
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
