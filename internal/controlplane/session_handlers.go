package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agenthub/pkg/protocol"
	"agenthub/pkg/sse"
)

func (s *Server) handleCreateWorkspaceSession(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	var req protocol.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sessionID := newSessionID()
	taskID := newTaskID()

	var turn protocol.TurnRequest
	if req.FirstTurn != nil {
		if strings.TrimSpace(req.FirstTurn.Message) == "" {
			http.Error(w, "firstTurn.message is required", http.StatusBadRequest)
			return
		}
		turn = turnRequestFromFirstTurn(workspaceID, taskID, sessionID, req.FirstTurn)
	}
	modelID, err := s.resolveSessionModelID(r.Context(), workspaceID, req.ModelID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.ModelID = modelID

	token, ok := s.workspaceExecutionToken(r.Context(), workspaceID)
	if !ok {
		http.Error(w, "workspace has no active agent-pod token; call /workspaces/{id}/start first", http.StatusConflict)
		return
	}
	skills, err := s.resolveSessionSkills(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mcpServers, err := s.resolveSessionMCPServers(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.pods.PrepareSession(r.Context(), workspaceID, token, protocol.PrepareSessionRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		SessionID:   sessionID,
		Skills:      skills,
		MCPServers:  mcpServers,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	task, session, err := s.store.CreateSession(r.Context(), workspaceID, taskID, sessionID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.FirstTurn == nil {
		writeJSON(w, map[string]any{"task": task, "session": session})
		return
	}

	run, err := s.startTurnForSession(r.Context(), workspaceID, token, turn)
	if err != nil {
		var conflict *ActiveRunConflict
		if errors.As(err, &conflict) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"error": "active_run_exists", "activeRun": conflict.ActiveRun})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"task": task, "session": session, "run": run, "streamUrl": "/sessions/" + session.SessionID + "/stream"})
}

func (s *Server) handleCreateSessionTurn(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	var req protocol.CreateTurnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	session, ok, err := s.store.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	token, ok := s.workspaceExecutionToken(r.Context(), session.WorkspaceID)
	if !ok {
		http.Error(w, "workspace has no active agent-pod token; call /workspaces/{id}/start first", http.StatusConflict)
		return
	}

	run, err := s.startTurnForSession(r.Context(), session.WorkspaceID, token, turnRequestFromCreateTurn(session, req))
	if err != nil {
		var conflict *ActiveRunConflict
		if errors.As(err, &conflict) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"error": "active_run_exists", "activeRun": conflict.ActiveRun})
			return
		}
		if errors.Is(err, ErrSessionNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"session": session, "run": run, "streamUrl": "/sessions/" + session.SessionID + "/stream"})
}

func (s *Server) startTurnForSession(ctx context.Context, workspaceID, token string, turn protocol.TurnRequest) (SessionRun, error) {
	runtimeEnv, err := s.runtimeEnvForTurn(ctx, turn)
	if err != nil {
		return SessionRun{}, err
	}
	turn.RuntimeEnv = runtimeEnv
	run, err := s.store.StartRun(ctx, turn)
	if err != nil {
		return SessionRun{}, err
	}
	if _, err := s.appendAndPublish(ctx, runLifecycleEvent(run, protocol.EventRunStarted, "started", nil)); err != nil {
		_, _ = s.store.FinishRun(context.Background(), turn.SessionID, turn.RunID, RunStatusFailed, &protocol.EventErrorPayload{Code: "run_start_event_failed", Message: err.Error()})
		return SessionRun{}, err
	}
	stored, err := s.appendAndPublish(ctx, userMessageEvent(turn))
	if err != nil {
		_, _ = s.store.FinishRun(context.Background(), turn.SessionID, turn.RunID, RunStatusFailed, &protocol.EventErrorPayload{Code: "user_message_event_failed", Message: err.Error()})
		return SessionRun{}, err
	}
	run.LastEventID = stored.ID
	go func() {
		runCtx := context.Background()
		cancel := func() {}
		if s.config.RunTimeout > 0 {
			runCtx, cancel = context.WithTimeout(context.Background(), s.config.RunTimeout)
		}
		defer cancel()
		var runtimeError *protocol.EventErrorPayload
		err := s.pods.Turn(runCtx, workspaceID, token, turn, func(event protocol.UniversalEvent) error {
			if event.Type == protocol.EventError && event.Error != nil && runtimeError == nil {
				copied := *event.Error
				runtimeError = &copied
			}
			_, err := s.appendAndPublish(context.Background(), event)
			return err
		})
		timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
		if timedOut && runtimeError == nil {
			runtimeError = timeoutRunError(s.config.RunTimeout)
			event := protocol.NewEvent(protocol.EventError, turn)
			event.Error = runtimeError
			_, _ = s.appendAndPublish(context.Background(), event)
		} else if err != nil && runtimeError == nil {
			runtimeError = &protocol.EventErrorPayload{Message: err.Error()}
			event := protocol.NewEvent(protocol.EventError, turn)
			event.Error = runtimeError
			_, _ = s.appendAndPublish(context.Background(), event)
		}
		s.finishRunAfterTurn(context.Background(), run, err, runtimeError, timedOut)
	}()
	return run, nil
}

func (s *Server) resolveSessionModelID(ctx context.Context, workspaceID, requestedModelID string) (string, error) {
	if _, ok, err := s.store.GetWorkspaceLLMConnection(ctx, workspaceID); err != nil {
		return "", err
	} else if !ok {
		return "", errors.New("llm connection is not configured")
	}
	models, err := s.store.ListWorkspaceLLMModels(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	requestedModelID = strings.TrimSpace(requestedModelID)
	if requestedModelID != "" {
		for _, model := range models {
			if model.ModelID == requestedModelID && model.Enabled {
				return requestedModelID, nil
			}
		}
		return "", errors.New("model is not enabled for this workspace: " + requestedModelID)
	}
	for _, model := range models {
		if model.Enabled {
			return model.ModelID, nil
		}
	}
	return "", errors.New("no enabled llm model configured for this workspace")
}

func (s *Server) runtimeEnvForTurn(ctx context.Context, turn protocol.TurnRequest) (map[string]string, error) {
	session, ok, err := s.store.GetSession(ctx, turn.SessionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSessionNotFound
	}
	if strings.TrimSpace(session.ModelID) == "" {
		return nil, errors.New("session modelId is empty")
	}
	connection, ok, err := s.store.GetWorkspaceLLMConnection(ctx, session.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("llm connection is not configured")
	}
	return map[string]string{
		"AGENTHUB_PI_PROVIDER": connection.Provider,
		"AGENTHUB_PI_API":      connection.APIProtocol,
		"AGENTHUB_PI_BASE_URL": connection.BaseURL,
		"AGENTHUB_PI_MODEL":    session.ModelID,
		"AGENTHUB_PI_API_KEY":  connection.APIKey,
	}, nil
}

func (s *Server) workspaceExecutionToken(ctx context.Context, workspaceID string) (string, bool) {
	token, ok := s.workspaceToken(ctx, workspaceID)
	if !ok && s.config.DevAgentPodToken != "" {
		return s.config.DevAgentPodToken, true
	}
	return token, ok
}

func turnRequestFromFirstTurn(workspaceID, taskID, sessionID string, firstTurn *protocol.FirstTurnRequest) protocol.TurnRequest {
	turn := protocol.TurnRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		SessionID:   sessionID,
		Message:     firstTurn.Message,
		Source:      firstTurn.Source,
	}
	normalizeTurnRequest(&turn)
	return turn
}

func turnRequestFromCreateTurn(session SessionProjection, req protocol.CreateTurnRequest) protocol.TurnRequest {
	turn := protocol.TurnRequest{
		WorkspaceID: session.WorkspaceID,
		TaskID:      session.TaskID,
		SessionID:   session.SessionID,
		Message:     req.Message,
		Source:      req.Source,
	}
	normalizeTurnRequest(&turn)
	return turn
}

func normalizeTurnRequest(turn *protocol.TurnRequest) {
	if turn.RunID == "" {
		turn.RunID = newRunID()
	}
	if turn.Source == "" {
		turn.Source = "api"
	}
}

func newSessionID() string {
	return "sess_" + time.Now().UTC().Format("20060102150405.000000000")
}

func newTaskID() string {
	return "task_" + time.Now().UTC().Format("20060102150405.000000000")
}

func newRunID() string {
	return "run_" + time.Now().UTC().Format("20060102150405.000000000")
}

func userMessageEvent(turn protocol.TurnRequest) protocol.UniversalEvent {
	messageID := "user_" + turn.RunID
	event := protocol.NewEvent(protocol.EventMessageCompleted, turn)
	event.MessageID = messageID
	event.Role = "user"
	event.Content = []protocol.UniversalBlock{{Type: "text", Text: turn.Message}}
	return event
}

func runLifecycleEvent(run SessionRun, eventType, reason string, eventError *protocol.EventErrorPayload) protocol.UniversalEvent {
	event := protocol.UniversalEvent{
		Type:        eventType,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		WorkspaceID: run.WorkspaceID,
		TaskID:      run.TaskID,
		SessionID:   run.SessionID,
		RunID:       run.RunID,
		Error:       eventError,
		Metadata: map[string]any{
			"runStatus": run.Status,
		},
	}
	if reason != "" {
		event.Metadata["reason"] = reason
	}
	return event
}

func runTerminalEventType(status string) string {
	switch status {
	case RunStatusCompleted:
		return protocol.EventRunCompleted
	case RunStatusCancelled:
		return protocol.EventRunCancelled
	case RunStatusTimedOut:
		return protocol.EventRunTimedOut
	default:
		return protocol.EventRunFailed
	}
}

func timeoutRunError(timeout time.Duration) *protocol.EventErrorPayload {
	message := "run timed out"
	if timeout > 0 {
		message = "run timed out after " + timeout.String()
	}
	return &protocol.EventErrorPayload{Code: "run_timeout", Message: message}
}

func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	afterID, err := parseInt64Query(r, "after", 0)
	if err != nil {
		http.Error(w, "after must be an integer event id", http.StatusBadRequest)
		return
	}
	limit, err := parseIntQuery(r, "limit", DefaultEventReplayLimit)
	if err != nil {
		http.Error(w, "limit must be an integer", http.StatusBadRequest)
		return
	}
	events, err := s.store.ListEventsBySession(r.Context(), r.PathValue("sessionId"), afterID, normalizeEventLimit(limit))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	nextCursor := afterID
	if len(events) > 0 {
		nextCursor = events[len(events)-1].ID
	}
	writeJSON(w, map[string]any{"events": events, "nextCursor": nextCursor})
}

func (s *Server) handleSessionStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	afterID, err := streamAfterID(r)
	if err != nil {
		http.Error(w, "Last-Event-ID/after must be an integer event id", http.StatusBadRequest)
		return
	}
	events, unsubscribe := s.hub.Subscribe(sessionID)
	defer unsubscribe()

	sse.SetHeaders(w)
	cursor := afterID
	if err := s.writeReplay(w, r, sessionID, &cursor); err != nil {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.ID <= cursor {
				continue
			}
			if err := sse.WriteEventWithID(w, event.ID, event.Payload); err != nil {
				return
			}
			cursor = event.ID
		case <-ticker.C:
			if err := s.writeReplay(w, r, sessionID, &cursor); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := s.store.ListMessagesBySession(r.Context(), r.PathValue("sessionId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"messages": messages})
}

func (s *Server) handleSessionState(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.SessionState(r.Context(), r.PathValue("sessionId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, state)
}

func (s *Server) handleSessionInterrupt(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	var req protocol.InterruptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ExpectedRunID == "" {
		http.Error(w, "expectedRunId is required", http.StatusBadRequest)
		return
	}
	if req.Reason == "" {
		req.Reason = "user_stop"
	}
	result, err := s.store.RequestRunInterrupt(r.Context(), sessionID, req.ExpectedRunID, req.Reason)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !result.Interrupted {
		if result.Reason != "already_terminal" {
			w.WriteHeader(http.StatusConflict)
		}
		writeJSON(w, result)
		return
	}

	cancelDelivered := false
	var cancelError string
	if result.Run != nil {
		if result.Reason != "already_cancelling" {
			if _, eventErr := s.appendAndPublish(context.Background(), runLifecycleEvent(*result.Run, protocol.EventRunCancelling, req.Reason, nil)); eventErr != nil {
				cancelError = "append cancelling event failed: " + eventErr.Error()
			}
		}
		token, ok := s.workspaceToken(r.Context(), result.Run.WorkspaceID)
		if !ok && s.config.DevAgentPodToken != "" {
			token = s.config.DevAgentPodToken
			ok = true
		}
		if ok {
			err = s.pods.Cancel(r.Context(), result.Run.WorkspaceID, token, sessionID, req)
			if err == nil {
				cancelDelivered = true
			}
		} else {
			err = errors.New("workspace has no active agent-pod token")
		}
		if err != nil {
			cancelError = err.Error()
			event := protocol.NewEvent(protocol.EventError, protocol.TurnRequest{WorkspaceID: result.Run.WorkspaceID, TaskID: result.Run.TaskID, SessionID: sessionID, RunID: req.ExpectedRunID})
			event.Error = &protocol.EventErrorPayload{Message: "cancel request failed: " + cancelError}
			_, _ = s.appendAndPublish(context.Background(), event)
		}
	}
	writeJSON(w, map[string]any{"interrupted": true, "run": result.Run, "cancelDelivered": cancelDelivered, "cancelError": cancelError})
}

func (s *Server) appendAndPublish(ctx context.Context, event protocol.UniversalEvent) (StoredEvent, error) {
	stored, err := s.store.Append(ctx, event)
	if err != nil {
		return StoredEvent{}, err
	}
	s.hub.Publish(stored)
	return stored, nil
}

func (s *Server) finishRunAfterTurn(ctx context.Context, started SessionRun, turnErr error, runtimeError *protocol.EventErrorPayload, timedOut bool) {
	run, ok, err := s.store.GetRun(ctx, started.SessionID, started.RunID)
	if err != nil || !ok || isTerminalRunStatus(run.Status) {
		return
	}
	status := RunStatusCompleted
	var eventError *protocol.EventErrorPayload
	reason := "completed"
	if timedOut {
		status = RunStatusTimedOut
		reason = "timeout"
		eventError = timeoutRunError(s.config.RunTimeout)
	} else if runtimeError != nil {
		status = RunStatusFailed
		reason = "runtime_error"
		eventError = runtimeError
	} else if run.Status == RunStatusCancelling {
		status = RunStatusCancelled
		reason = "cancelled"
	} else if turnErr != nil {
		status = RunStatusFailed
		reason = "turn_error"
		eventError = &protocol.EventErrorPayload{Message: turnErr.Error()}
	}
	finished, err := s.store.FinishRun(ctx, started.SessionID, started.RunID, status, eventError)
	if err != nil {
		return
	}
	_, _ = s.appendAndPublish(ctx, runLifecycleEvent(finished, runTerminalEventType(status), reason, eventError))
}

func (s *Server) writeReplay(w http.ResponseWriter, r *http.Request, sessionID string, cursor *int64) error {
	for {
		replay, err := s.store.ListEventsBySession(r.Context(), sessionID, *cursor, DefaultEventReplayLimit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return err
		}
		for _, event := range replay {
			if err := sse.WriteEventWithID(w, event.ID, event.Payload); err != nil {
				return err
			}
			*cursor = event.ID
		}
		if len(replay) < DefaultEventReplayLimit {
			return nil
		}
	}
}

func streamAfterID(r *http.Request) (int64, error) {
	if value := r.Header.Get("Last-Event-ID"); value != "" {
		return strconv.ParseInt(value, 10, 64)
	}
	return parseInt64Query(r, "after", 0)
}

func parseInt64Query(r *http.Request, key string, fallback int64) (int64, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseIntQuery(r *http.Request, key string, fallback int) (int, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func normalizeEventLimit(limit int) int {
	if limit <= 0 {
		return DefaultEventReplayLimit
	}
	if limit > MaxEventReplayLimit {
		return MaxEventReplayLimit
	}
	return limit
}

func (s *Server) handleListWorkspaceSessions(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	limit, _ := parseIntQuery(r, "limit", 20)
	offset, _ := parseIntQuery(r, "offset", 0)
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	sessions, err := s.store.ListWorkspaceSessions(r.Context(), workspaceID, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []SessionProjection{}
	}
	writeJSON(w, map[string]any{"sessions": sessions, "limit": limit, "offset": offset})
}
