package agentpod

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"

	"agenthub/internal/protocol"
	"agenthub/internal/sse"
)

type Server struct {
	workspaceID string
	runtimeID   string
	token       string
	adapter     Adapter

	mu    sync.Mutex
	turns map[string]context.CancelFunc
}

func NewServer(workspaceID, runtimeID, token string, adapter Adapter) *Server {
	return &Server{workspaceID: workspaceID, runtimeID: runtimeID, token: token, adapter: adapter, turns: map[string]context.CancelFunc{}}
}

func NewServerFromEnv() *Server {
	workspaceID := env("WORKSPACE_ID", "local")
	runtimeID := env("RUNTIME_ID", "pi-agent")
	token := os.Getenv("AGENTHUB_INTERNAL_TOKEN")
	adapter := Adapter(PiCLIAdapter{Command: os.Getenv("PI_AGENT_COMMAND"), WorkDir: env("WORKSPACE_DIR", "/workspace")})
	return NewServer(workspaceID, runtimeID, token, adapter)
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /info", s.withAuth(s.handleInfo))
	mux.HandleFunc("POST /turn", s.withAuth(s.handleTurn))
	mux.HandleFunc("POST /sessions/{sessionId}/reconnect", s.withAuth(s.handleReconnect))
	mux.HandleFunc("POST /sessions/{sessionId}/cancel", s.withAuth(s.handleCancel))
	mux.HandleFunc("POST /reload", s.withAuth(s.handleReload))
	mux.HandleFunc("POST /shutdown", s.withAuth(s.handleShutdown))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"workspaceId": s.workspaceID, "runtimeId": s.runtimeID, "capabilities": []string{"turn", "cancel", "reload", "sse-universal-event"}})
}

func (s *Server) handleTurn(w http.ResponseWriter, r *http.Request) {
	var req protocol.TurnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == "" {
		req.WorkspaceID = s.workspaceID
	}
	if req.Source == "" {
		req.Source = "api"
	}
	if req.RunID == "" || req.SessionID == "" || req.Message == "" {
		http.Error(w, "sessionId, runId and message are required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	s.track(req.SessionID, cancel)
	defer s.untrack(req.SessionID)
	sse.SetHeaders(w)
	err := s.adapter.RunTurn(ctx, req, func(event protocol.UniversalEvent) error {
		return sse.WriteEvent(w, event)
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		_ = sse.WriteEvent(w, protocol.UniversalEvent{Type: protocol.EventError, WorkspaceID: req.WorkspaceID, SessionID: req.SessionID, RunID: req.RunID, Error: &protocol.EventErrorPayload{Message: err.Error()}})
	}
}

func (s *Server) handleReconnect(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "no reconnectable sink for this minimal adapter yet", http.StatusConflict)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	s.mu.Lock()
	cancel := s.turns[sessionID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		_ = json.NewEncoder(w).Encode(map[string]any{"cancelled": true, "sessionId": sessionID})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"cancelled": false, "sessionId": sessionID, "reason": "not_running"})
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"reloaded": true, "scope": "next_turn"})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"shutdown": "accepted"})
	go func() { os.Exit(0) }()
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" {
			http.Error(w, "AGENTHUB_INTERNAL_TOKEN is required", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			http.Error(w, "invalid internal token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) track(sessionID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns[sessionID] = cancel
}

func (s *Server) untrack(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.turns, sessionID)
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
