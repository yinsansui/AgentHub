package agentpod

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"

	"agenthub/internal/runtime"
	piagent "agenthub/internal/runtime/pi"
	"agenthub/pkg/protocol"
	"agenthub/pkg/sse"
)

type Server struct {
	workspaceID  string
	runtimeID    string
	token        string
	workspaceDir string
	adapter      runtime.Runtime

	mu    sync.Mutex
	turns map[string]trackedTurn
}

type trackedTurn struct {
	runID  string
	cancel context.CancelFunc
}

func NewServer(workspaceID, runtimeID, token string, adapter runtime.Runtime) *Server {
	return &Server{workspaceID: workspaceID, runtimeID: runtimeID, token: token, workspaceDir: "/workspace", adapter: adapter, turns: map[string]trackedTurn{}}
}

func NewServerFromEnv() *Server {
	workspaceID := env("WORKSPACE_ID", "local")
	runtimeID := env("RUNTIME_ID", "pi-agent")
	token := os.Getenv("AGENTHUB_INTERNAL_TOKEN")
	workspaceDir := env("WORKSPACE_DIR", "/workspace")
	adapter := runtime.Runtime(piagent.PiCLIAdapter{Command: os.Getenv("PI_AGENT_COMMAND"), WorkDir: workspaceDir})
	server := NewServer(workspaceID, runtimeID, token, adapter)
	server.workspaceDir = workspaceDir
	return server
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /info", s.withAuth(s.handleInfo))
	mux.HandleFunc("POST /sessions/{sessionId}/prepare", s.withAuth(s.handlePrepareSession))
	mux.HandleFunc("POST /turn", s.withAuth(s.handleTurn))
	mux.HandleFunc("POST /sessions/{sessionId}/reconnect", s.withAuth(s.handleReconnect))
	mux.HandleFunc("POST /sessions/{sessionId}/cancel", s.withAuth(s.handleCancel))
	mux.HandleFunc("POST /shutdown", s.withAuth(s.handleShutdown))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"workspaceId": s.workspaceID, "runtimeId": s.runtimeID, "capabilities": []string{"prepare-session", "turn", "cancel", "sse-universal-event"}})
}

func (s *Server) handlePrepareSession(w http.ResponseWriter, r *http.Request) {
	var req protocol.PrepareSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.WorkspaceID == "" {
		req.WorkspaceID = s.workspaceID
	}
	if req.SessionID == "" {
		req.SessionID = r.PathValue("sessionId")
	}
	if req.TaskID == "" || req.SessionID == "" {
		http.Error(w, "taskId and sessionId are required", http.StatusBadRequest)
		return
	}
	sessionDir, err := s.prepareSession(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"prepared": true, "workspaceId": req.WorkspaceID, "taskId": req.TaskID, "sessionId": req.SessionID, "sessionCwd": sessionDir})
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
	if req.RunID == "" || req.TaskID == "" || req.SessionID == "" || req.Message == "" {
		http.Error(w, "taskId, sessionId, runId and message are required", http.StatusBadRequest)
		return
	}
	req.SessionCWD = s.sessionDir(req.TaskID, req.SessionID)
	ctx, cancel := context.WithCancel(r.Context())
	s.track(req.SessionID, req.RunID, cancel)
	defer s.untrack(req.SessionID, req.RunID)
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
	var req protocol.InterruptRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.ExpectedRunID == "" {
		http.Error(w, "expectedRunId is required", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	turn, ok := s.turns[sessionID]
	s.mu.Unlock()
	if !ok {
		_ = json.NewEncoder(w).Encode(map[string]any{"cancelled": false, "sessionId": sessionID, "expectedRunId": req.ExpectedRunID, "reason": "not_running"})
		return
	}
	if turn.runID != req.ExpectedRunID {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"cancelled": false, "sessionId": sessionID, "expectedRunId": req.ExpectedRunID, "activeRunId": turn.runID, "reason": "run_mismatch"})
		return
	}
	turn.cancel()
	_ = json.NewEncoder(w).Encode(map[string]any{"cancelled": true, "sessionId": sessionID, "runId": req.ExpectedRunID})
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

func (s *Server) track(sessionID, runID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns[sessionID] = trackedTurn{runID: runID, cancel: cancel}
}

func (s *Server) untrack(sessionID, runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if turn, ok := s.turns[sessionID]; ok && turn.runID != runID {
		return
	}
	delete(s.turns, sessionID)
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
