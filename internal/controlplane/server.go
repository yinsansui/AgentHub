package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"agenthub/internal/driver"
	dockerdriver "agenthub/internal/driver/docker"
	"agenthub/pkg/protocol"
	"agenthub/pkg/sse"
)

type Server struct {
	config Config
	driver driver.Driver
	pods   *AgentPodClient
	store  *EventStore

	mu     sync.RWMutex
	tokens map[string]string
}

func NewServer(config Config) *Server {
	server := &Server{
		config: config,
		driver: dockerdriver.NewDockerAgentPodDriver(driver.Config{DockerSocket: config.DockerSocket, DockerNetwork: config.DockerNetwork, AgentPodImage: config.AgentPodImage, WorkspaceRoot: config.WorkspaceRoot}),
		pods:   NewAgentPodClient(config.AgentPodBaseURLTemplate),
		store:  NewEventStore(config.StatePath),
		tokens: map[string]string{},
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
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
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
	s.setToken(workspaceID, req.Token)
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

func (s *Server) handleTurn(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	var turn protocol.TurnRequest
	if err := json.NewDecoder(r.Body).Decode(&turn); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	turn.WorkspaceID = workspaceID
	if turn.SessionID == "" {
		turn.SessionID = "sess_" + time.Now().UTC().Format("20060102150405.000000000")
	}
	if turn.RunID == "" {
		turn.RunID = "run_" + time.Now().UTC().Format("20060102150405.000000000")
	}
	if turn.Source == "" {
		turn.Source = "api"
	}
	if turn.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	token, ok := s.token(workspaceID)
	if !ok && s.config.DevAgentPodToken != "" {
		token = s.config.DevAgentPodToken
		ok = true
	}
	if !ok {
		http.Error(w, "workspace has no active agent-pod token; call /workspaces/{id}/start first", http.StatusConflict)
		return
	}
	sse.SetHeaders(w)
	err := s.pods.Turn(r.Context(), workspaceID, token, turn, func(event protocol.UniversalEvent) error {
		if err := s.store.Append(context.Background(), event); err != nil {
			return err
		}
		return sse.WriteEvent(w, event)
	})
	if err != nil {
		event := protocol.NewEvent(protocol.EventError, turn)
		event.Error = &protocol.EventErrorPayload{Message: err.Error()}
		_ = s.store.Append(context.Background(), event)
		_ = sse.WriteEvent(w, event)
	}
}

func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListBySession(r.PathValue("sessionId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"events": events})
}

func (s *Server) setToken(workspaceID, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[workspaceID] = token
}

func (s *Server) token(workspaceID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.tokens[workspaceID]
	return token, ok
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
