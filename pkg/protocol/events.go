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
	WorkspaceID string            `json:"workspaceId"`
	TaskID      string            `json:"taskId"`
	SessionID   string            `json:"sessionId"`
	RunID       string            `json:"runId"`
	Message     string            `json:"message"`
	Source      string            `json:"source"`
	SessionCWD  string            `json:"sessionCwd,omitempty"`
	RuntimeEnv  map[string]string `json:"runtimeEnv,omitempty"`
}

const (
	SkillSourceUser            = "user"
	SkillSourcePlugin          = "plugin"
	SkillSourceWorkspace       = "workspace"
	SkillSourcePlatformBuiltin = "platform_builtin"
)

type PrepareSessionRequest struct {
	WorkspaceID string              `json:"workspaceId"`
	TaskID      string              `json:"taskId"`
	SessionID   string              `json:"sessionId"`
	Skills      []ResolvedSkill     `json:"skills"`
	MCPServers  []ResolvedMCPServer `json:"mcpServers"`
}

type ResolvedSkill struct {
	Slug         string              `json:"slug"`
	Source       string              `json:"source"`
	DefinitionID string              `json:"definitionId"`
	Version      int64               `json:"version"`
	ContentHash  string              `json:"contentHash"`
	Files        []ResolvedSkillFile `json:"files"`
	Shadowed     []SkillShadow       `json:"shadowed,omitempty"`
}

type ResolvedSkillFile struct {
	Path        string `json:"path"`
	Content     string `json:"content"`
	ContentHash string `json:"contentHash"`
}

type SkillShadow struct {
	Source       string `json:"source"`
	DefinitionID string `json:"definitionId"`
	Version      int64  `json:"version"`
	ContentHash  string `json:"contentHash"`
}

type SkillManifest struct {
	SessionID string          `json:"sessionId"`
	CreatedAt string          `json:"createdAt"`
	Skills    []ResolvedSkill `json:"skills"`
}

type ResolvedMCPServer struct {
	Name         string            `json:"name"`
	Source       string            `json:"source"`
	DefinitionID string            `json:"definitionId"`
	Version      int64             `json:"version"`
	ContentHash  string            `json:"contentHash"`
	Command      string            `json:"command"`
	Args         []string          `json:"args,omitempty"`
	Transport    string            `json:"transport,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Shadowed     []MCPServerShadow `json:"shadowed,omitempty"`
}

type MCPServerShadow struct {
	Source       string `json:"source"`
	DefinitionID string `json:"definitionId"`
	Version      int64  `json:"version"`
	ContentHash  string `json:"contentHash"`
}

type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

type MCPServerConfig struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	Transport string            `json:"transport,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

type CreateSessionRequest struct {
	Title     string            `json:"title,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
	ModelID   string            `json:"modelId,omitempty"`
	FirstTurn *FirstTurnRequest `json:"firstTurn,omitempty"`
}

type FirstTurnRequest struct {
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
}

type CreateTurnRequest struct {
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
}

type InterruptRequest struct {
	ExpectedRunID string `json:"expectedRunId"`
	Reason        string `json:"reason,omitempty"`
}

type UniversalEvent struct {
	Type        string             `json:"type"`
	Timestamp   string             `json:"timestamp"`
	WorkspaceID string             `json:"workspaceId,omitempty"`
	TaskID      string             `json:"taskId,omitempty"`
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
		TaskID:      req.TaskID,
		SessionID:   req.SessionID,
		RunID:       req.RunID,
	}
}
