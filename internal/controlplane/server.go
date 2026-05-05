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

	mu     sync.RWMutex
	tokens map[string]string
}

func NewServer(config Config) *Server {
	driverConfig := driver.Config{
		DockerSocket:  config.DockerSocket,
		DockerNetwork: config.DockerNetwork,
		AgentPodImage: config.AgentPodImage,
		WorkspaceRoot: config.WorkspaceRoot,
	}
	server := &Server{
		config: config,
		driver: dockerdriver.NewDockerAgentPodDriver(driverConfig),
		pods:   NewAgentPodClient(config.AgentPodBaseURLTemplate),
		hub:    NewEventHub(),
		tokens: map[string]string{},
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
	mux.HandleFunc("POST /workspaces/{workspaceId}/start", s.handleStartWorkspace)
	mux.HandleFunc("POST /workspaces/{workspaceId}/stop", s.handleStopWorkspace)
	mux.HandleFunc("GET /workspaces/{workspaceId}/pod", s.handleInspectWorkspace)
	mux.HandleFunc("GET /workspaces/{workspaceId}/logs", s.handleWorkspaceLogs)
	mux.HandleFunc("POST /workspaces/{workspaceId}/turn", s.handleTurn)
	mux.HandleFunc("GET /sessions/{sessionId}/events", s.handleSessionEvents)
	mux.HandleFunc("GET /sessions/{sessionId}/stream", s.handleSessionStream)
	mux.HandleFunc("GET /sessions/{sessionId}/messages", s.handleSessionMessages)
	return mux
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

func (s *Server) handleStartWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	var req driver.AgentPodSpec
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.WorkspaceID = workspaceID
	if req.Token == "" {
		token, err := driver.NewToken()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Token = token
	}
	info, err := s.driver.Start(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.config.DatabaseURL != "" {
		if err := s.store.SaveWorkspaceToken(r.Context(), workspaceID, req.Token); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		s.setToken(workspaceID, req.Token)
	}
	writeJSON(w, map[string]any{"pod": info, "tokenStored": true})
}

func (s *Server) handleStopWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if err := s.driver.Stop(r.Context(), workspaceID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"stopped": true, "workspaceId": workspaceID})
}

func (s *Server) handleInspectWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	info, err := s.driver.Inspect(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, info)
}

func (s *Server) handleWorkspaceLogs(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
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
