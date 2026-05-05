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
	var token string
	if req.FirstTurn != nil {
		if strings.TrimSpace(req.FirstTurn.Message) == "" {
			http.Error(w, "firstTurn.message is required", http.StatusBadRequest)
			return
		}
		var ok bool
		token, ok = s.workspaceExecutionToken(r.Context(), workspaceID)
		if !ok {
			http.Error(w, "workspace has no active agent-pod token; call /workspaces/{id}/start first", http.StatusConflict)
			return
		}
		turn = turnRequestFromFirstTurn(workspaceID, taskID, sessionID, req.FirstTurn)
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
	run, err := s.store.StartRun(ctx, turn)
	if err != nil {
		return SessionRun{}, err
	}
	stored, err := s.appendAndPublish(ctx, userMessageEvent(turn))
	if err != nil {
		_, _ = s.store.FinishRun(context.Background(), turn.SessionID, turn.RunID, RunStatusFailed, &protocol.EventErrorPayload{Message: err.Error()})
		return SessionRun{}, err
	}
	run.LastEventID = stored.ID
	go func() {
		err := s.pods.Turn(context.Background(), workspaceID, token, turn, func(event protocol.UniversalEvent) error {
			_, err := s.appendAndPublish(context.Background(), event)
			return err
		})
		if err != nil {
			event := protocol.NewEvent(protocol.EventError, turn)
			event.Error = &protocol.EventErrorPayload{Message: err.Error()}
			_, _ = s.appendAndPublish(context.Background(), event)
		}
		s.finishRunAfterTurn(context.Background(), run, err)
	}()
	return run, nil
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
		WorkspaceID:      workspaceID,
		TaskID:           taskID,
		SessionID:        sessionID,
		Message:          firstTurn.Message,
		ActiveSkillSlugs: firstTurn.ActiveSkillSlugs,
		Source:           firstTurn.Source,
	}
	normalizeTurnRequest(&turn)
	return turn
}

func turnRequestFromCreateTurn(session SessionProjection, req protocol.CreateTurnRequest) protocol.TurnRequest {
	turn := protocol.TurnRequest{
		WorkspaceID:      session.WorkspaceID,
		TaskID:           session.TaskID,
		SessionID:        session.SessionID,
		Message:          req.Message,
		ActiveSkillSlugs: req.ActiveSkillSlugs,
		Source:           req.Source,
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
	event := protocol.NewEvent(protocol.EventItemCompleted, turn)
	event.ItemID = messageID
	event.Role = "user"
	event.Item = &protocol.UniversalItem{
		ID:      messageID,
		Type:    "message",
		Role:    "user",
		Content: []protocol.UniversalBlock{{Type: "text", Text: turn.Message}},
	}
	return event
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

func (s *Server) finishRunAfterTurn(ctx context.Context, started SessionRun, turnErr error) {
	run, ok, err := s.store.GetRun(ctx, started.SessionID, started.RunID)
	if err != nil || !ok || isTerminalRunStatus(run.Status) {
		return
	}
	status := RunStatusCompleted
	var eventError *protocol.EventErrorPayload
	if run.Status == RunStatusCancelling {
		status = RunStatusCancelled
	} else if turnErr != nil {
		status = RunStatusFailed
		eventError = &protocol.EventErrorPayload{Message: turnErr.Error()}
	}
	_, _ = s.store.FinishRun(ctx, started.SessionID, started.RunID, status, eventError)
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
