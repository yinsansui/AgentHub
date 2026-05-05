package controlplane

import (
	"context"
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
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		workspace: map[string]string{},
		events:    map[string][]StoredEvent{},
		messages:  map[string]map[string]MessageProjection{},
		order:     map[string][]string{},
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
