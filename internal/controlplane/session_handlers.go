package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"agenthub/pkg/protocol"
	"agenthub/pkg/sse"
)

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
	token, ok := s.workspaceToken(r.Context(), workspaceID)
	if !ok && s.config.DevAgentPodToken != "" {
		token = s.config.DevAgentPodToken
		ok = true
	}
	if !ok {
		http.Error(w, "workspace has no active agent-pod token; call /workspaces/{id}/start first", http.StatusConflict)
		return
	}
	events, unsubscribe := s.hub.Subscribe(turn.SessionID)
	defer unsubscribe()

	if _, err := s.appendAndPublish(r.Context(), userMessageEvent(turn)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sse.SetHeaders(w)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		err := s.pods.Turn(context.Background(), workspaceID, token, turn, func(event protocol.UniversalEvent) error {
			_, err := s.appendAndPublish(context.Background(), event)
			return err
		})
		if err != nil {
			event := protocol.NewEvent(protocol.EventError, turn)
			event.Error = &protocol.EventErrorPayload{Message: err.Error()}
			_, _ = s.appendAndPublish(context.Background(), event)
		}
	}()

	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := sse.WriteEventWithID(w, event.ID, event.Payload); err != nil {
				return
			}
		case <-runDone:
			for {
				select {
				case event, ok := <-events:
					if !ok {
						return
					}
					if err := sse.WriteEventWithID(w, event.ID, event.Payload); err != nil {
						return
					}
				default:
					return
				}
			}
		case <-r.Context().Done():
			return
		}
	}
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

func (s *Server) appendAndPublish(ctx context.Context, event protocol.UniversalEvent) (StoredEvent, error) {
	stored, err := s.store.Append(ctx, event)
	if err != nil {
		return StoredEvent{}, err
	}
	s.hub.Publish(stored)
	return stored, nil
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
