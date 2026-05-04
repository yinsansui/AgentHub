package protocol

import "time"

const (
	EventSessionStarted = "session.started"
	EventItemStarted    = "item.started"
	EventItemDelta      = "item.delta"
	EventItemCompleted  = "item.completed"
	EventSessionEnded   = "session.ended"
	EventError          = "error"
)

type TurnRequest struct {
	WorkspaceID      string   `json:"workspaceId"`
	SessionID        string   `json:"sessionId"`
	RunID            string   `json:"runId"`
	Message          string   `json:"message"`
	ActiveSkillSlugs []string `json:"activeSkillSlugs"`
	Source           string   `json:"source"`
}

type UniversalEvent struct {
	Type        string             `json:"type"`
	Timestamp   string             `json:"timestamp"`
	WorkspaceID string             `json:"workspaceId,omitempty"`
	SessionID   string             `json:"sessionId,omitempty"`
	RunID       string             `json:"runId,omitempty"`
	ItemID      string             `json:"itemId,omitempty"`
	Role        string             `json:"role,omitempty"`
	Item        *UniversalItem     `json:"item,omitempty"`
	Delta       string             `json:"delta,omitempty"`
	Error       *EventErrorPayload `json:"error,omitempty"`
	Metadata    map[string]any     `json:"metadata,omitempty"`
}

type UniversalItem struct {
	ID      string           `json:"id"`
	Type    string           `json:"type"`
	Role    string           `json:"role,omitempty"`
	Content []UniversalBlock `json:"content,omitempty"`
}

type UniversalBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	Name   string `json:"name,omitempty"`
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
}

type EventErrorPayload struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

func NewEvent(eventType string, req TurnRequest) UniversalEvent {
	return UniversalEvent{
		Type:        eventType,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		WorkspaceID: req.WorkspaceID,
		SessionID:   req.SessionID,
		RunID:       req.RunID,
	}
}
