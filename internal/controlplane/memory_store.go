package controlplane

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"agenthub/pkg/protocol"
)

type MemoryStore struct {
	mu          sync.RWMutex
	nextEventID int64
	workspace   map[string]string
	events      map[string][]StoredEvent
	messages    map[string]map[string]MessageProjection
	order       map[string][]string
	sessions    map[string]SessionProjection
	activeRuns  map[string]string
	runs        map[string]SessionRun
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		workspace:  map[string]string{},
		events:     map[string][]StoredEvent{},
		messages:   map[string]map[string]MessageProjection{},
		order:      map[string][]string{},
		sessions:   map[string]SessionProjection{},
		activeRuns: map[string]string{},
		runs:       map[string]SessionRun{},
	}
}

func (s *MemoryStore) Ping(ctx context.Context) error { return nil }

func (s *MemoryStore) SaveWorkspaceToken(ctx context.Context, workspaceID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspace[workspaceID] = token
	return nil
}

func (s *MemoryStore) WorkspaceToken(ctx context.Context, workspaceID string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.workspace[workspaceID]
	return token, ok, nil
}

func (s *MemoryStore) CreateSession(ctx context.Context, workspaceID, sessionID string, req protocol.CreateSessionRequest) (SessionProjection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	session := s.sessions[sessionID]
	if session.SessionID == "" {
		session = SessionProjection{SessionID: sessionID, WorkspaceID: workspaceID, CreatedAt: now}
	}
	session.WorkspaceID = workspaceID
	session.Title = req.Title
	session.Metadata = cloneMetadata(req.Metadata)
	session.UpdatedAt = now
	s.sessions[sessionID] = session
	return session, nil
}

func (s *MemoryStore) GetSession(ctx context.Context, sessionID string) (SessionProjection, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return SessionProjection{}, false, nil
	}
	session.Metadata = cloneMetadata(session.Metadata)
	return session, true, nil
}

func (s *MemoryStore) Append(ctx context.Context, event protocol.UniversalEvent) (StoredEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextEventID++
	stored := StoredEvent{
		ID:        s.nextEventID,
		Type:      event.Type,
		Payload:   event,
		CreatedAt: time.Now().UTC(),
	}
	s.events[event.SessionID] = append(s.events[event.SessionID], stored)
	s.projectMessage(event)
	if event.RunID != "" {
		run := s.runs[event.RunID]
		if run.RunID != "" && run.SessionID == event.SessionID {
			run.LastEventID = stored.ID
			run.UpdatedAt = time.Now().UTC()
			s.runs[event.RunID] = run
		}
	}
	return stored, nil
}

func (s *MemoryStore) ListEventsBySession(ctx context.Context, sessionID string, afterID int64, limit int) ([]StoredEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > MaxEventReplayLimit {
		limit = DefaultEventReplayLimit
	}
	out := make([]StoredEvent, 0, min(limit, len(s.events[sessionID])))
	for _, event := range s.events[sessionID] {
		if event.ID <= afterID {
			continue
		}
		out = append(out, event)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *MemoryStore) ListMessagesBySession(ctx context.Context, sessionID string) ([]MessageProjection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MessageProjection, 0, len(s.order[sessionID]))
	for _, messageID := range s.order[sessionID] {
		message := s.messages[sessionID][messageID]
		message.Blocks = append([]protocol.UniversalBlock(nil), message.Blocks...)
		out = append(out, message)
	}
	return out, nil
}

func (s *MemoryStore) StartRun(ctx context.Context, turn protocol.TurnRequest) (SessionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[turn.SessionID].SessionID == "" {
		return SessionRun{}, ErrSessionNotFound
	}
	if activeRunID := s.activeRuns[turn.SessionID]; activeRunID != "" {
		active := s.runs[activeRunID]
		if active.RunID != "" && !isTerminalRunStatus(active.Status) {
			return SessionRun{}, &ActiveRunConflict{ActiveRun: active}
		}
		delete(s.activeRuns, turn.SessionID)
	}
	now := time.Now().UTC()
	run := SessionRun{
		RunID:       turn.RunID,
		SessionID:   turn.SessionID,
		WorkspaceID: turn.WorkspaceID,
		Status:      RunStatusRunning,
		StartedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.runs[turn.RunID] = run
	s.activeRuns[turn.SessionID] = turn.RunID
	return run, nil
}

func (s *MemoryStore) FinishRun(ctx context.Context, sessionID, runID, status string, eventError *protocol.EventErrorPayload) (SessionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok || run.SessionID != sessionID {
		return SessionRun{}, errors.New("run not found")
	}
	now := time.Now().UTC()
	run.Status = status
	run.EndedAt = &now
	run.Error = eventError
	run.UpdatedAt = now
	s.runs[runID] = run
	if s.activeRuns[sessionID] == runID {
		delete(s.activeRuns, sessionID)
	}
	return run, nil
}

func (s *MemoryStore) GetRun(ctx context.Context, sessionID, runID string) (SessionRun, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[runID]
	if !ok || run.SessionID != sessionID {
		return SessionRun{}, false, nil
	}
	return run, true, nil
}

func (s *MemoryStore) RequestRunInterrupt(ctx context.Context, sessionID, expectedRunID, reason string) (RunInterruptResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	activeRunID := s.activeRuns[sessionID]
	if activeRunID == "" {
		return RunInterruptResult{Interrupted: false, Reason: "no_active_run", ExpectedRunID: expectedRunID}, nil
	}
	active := s.runs[activeRunID]
	if active.RunID != expectedRunID {
		return RunInterruptResult{Interrupted: false, Reason: "run_mismatch", ExpectedRunID: expectedRunID, ActiveRun: &active}, nil
	}
	if isTerminalRunStatus(active.Status) {
		return RunInterruptResult{Interrupted: false, Reason: "already_terminal", ExpectedRunID: expectedRunID, Run: &active}, nil
	}
	if active.Status == RunStatusCancelling {
		return RunInterruptResult{Interrupted: true, ExpectedRunID: expectedRunID, Run: &active}, nil
	}
	now := time.Now().UTC()
	active.Status = RunStatusCancelling
	active.CancelRequestedAt = &now
	active.UpdatedAt = now
	s.runs[expectedRunID] = active
	return RunInterruptResult{Interrupted: true, ExpectedRunID: expectedRunID, Run: &active}, nil
}

func (s *MemoryStore) SessionState(ctx context.Context, sessionID string) (SessionState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := SessionState{SessionID: sessionID}
	state.Messages = make([]MessageProjection, 0, len(s.order[sessionID]))
	for _, messageID := range s.order[sessionID] {
		message := s.messages[sessionID][messageID]
		message.Blocks = append([]protocol.UniversalBlock(nil), message.Blocks...)
		state.Messages = append(state.Messages, message)
	}
	if events := s.events[sessionID]; len(events) > 0 {
		state.LatestEventID = events[len(events)-1].ID
	}
	if activeRunID := s.activeRuns[sessionID]; activeRunID != "" {
		run := s.runs[activeRunID]
		if run.RunID != "" && !isTerminalRunStatus(run.Status) {
			state.ActiveRun = &run
		}
	}
	return state, nil
}

func (s *MemoryStore) projectMessage(event protocol.UniversalEvent) {
	if event.SessionID == "" {
		return
	}
	messageID := projectionItemID(event)
	switch event.Type {
	case protocol.EventItemStarted:
		if messageID == "" {
			return
		}
		s.upsertMessage(event, messageID, MessageStatusStreaming, nil)
	case protocol.EventItemDelta:
		if messageID == "" {
			return
		}
		s.upsertMessage(event, messageID, MessageStatusStreaming, nil)
	case protocol.EventItemCompleted:
		if messageID == "" {
			return
		}
		message := s.upsertMessage(event, messageID, MessageStatusCompleted, nil)
		if blocks := eventBlocks(event); len(blocks) > 0 {
			message.Blocks = append([]protocol.UniversalBlock(nil), blocks...)
		}
		s.messages[event.SessionID][messageID] = message
	case protocol.EventError:
		if event.Error == nil {
			return
		}
		if messageID == "" {
			messageID = errorMessageID(event)
		}
		message := s.upsertMessage(event, messageID, MessageStatusError, event.Error)
		if len(message.Blocks) == 0 && strings.TrimSpace(event.Error.Message) != "" {
			message.Blocks = append(message.Blocks, protocol.UniversalBlock{Type: "text", Text: event.Error.Message})
		}
		s.messages[event.SessionID][messageID] = message
	}
}

func (s *MemoryStore) upsertMessage(event protocol.UniversalEvent, messageID, status string, eventError *protocol.EventErrorPayload) MessageProjection {
	sessionMessages := s.messages[event.SessionID]
	if sessionMessages == nil {
		sessionMessages = map[string]MessageProjection{}
		s.messages[event.SessionID] = sessionMessages
	}
	message, exists := sessionMessages[messageID]
	if !exists {
		now := time.Now().UTC()
		message = MessageProjection{SessionID: event.SessionID, MessageID: messageID, CreatedAt: now, UpdatedAt: now, Blocks: []protocol.UniversalBlock{}}
		s.order[event.SessionID] = append(s.order[event.SessionID], messageID)
	}
	message.WorkspaceID = event.WorkspaceID
	message.RunID = event.RunID
	message.Role = eventRole(event)
	message.Status = status
	message.Error = eventError
	message.UpdatedAt = time.Now().UTC()
	sessionMessages[messageID] = message
	return message
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}
