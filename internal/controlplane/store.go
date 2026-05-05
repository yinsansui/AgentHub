package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"agenthub/pkg/protocol"
)

const (
	MessageStatusStreaming = "streaming"
	MessageStatusCompleted = "completed"
	MessageStatusError     = "error"

	RunStatusQueued     = "queued"
	RunStatusRunning    = "running"
	RunStatusCancelling = "cancelling"
	RunStatusCompleted  = "completed"
	RunStatusFailed     = "failed"
	RunStatusCancelled  = "cancelled"

	DefaultEventReplayLimit = 500
	MaxEventReplayLimit     = 1000
)

type EventStore interface {
	Ping(ctx context.Context) error
	SaveWorkspaceToken(ctx context.Context, workspaceID string, token string) error
	WorkspaceToken(ctx context.Context, workspaceID string) (string, bool, error)
	CreateSession(ctx context.Context, workspaceID, taskID, sessionID string, req protocol.CreateSessionRequest) (TaskProjection, SessionProjection, error)
	GetSession(ctx context.Context, sessionID string) (SessionProjection, bool, error)
	GetWorkspaceLLMConnection(ctx context.Context, workspaceID string) (LLMConnection, bool, error)
	UpsertWorkspaceLLMConnection(ctx context.Context, workspaceID string, connection LLMConnection) (LLMConnection, error)
	ListWorkspaceLLMModels(ctx context.Context, workspaceID string) ([]LLMConnectionModel, error)
	UpsertWorkspaceLLMModel(ctx context.Context, workspaceID string, model LLMConnectionModel) (LLMConnectionModel, error)
	ListSkillCandidates(ctx context.Context, workspaceID string) ([]SkillDefinitionWithFiles, error)
	ListMCPCandidates(ctx context.Context, workspaceID string) ([]MCPServerDefinitionWithEnv, error)
	ListWorkspaceSkills(ctx context.Context, workspaceID string) ([]SkillDefinitionWithFiles, error)
	GetWorkspaceSkill(ctx context.Context, workspaceID, slug string) (SkillDefinitionWithFiles, bool, error)
	UpsertWorkspaceSkill(ctx context.Context, workspaceID string, skill SkillDefinitionWithFiles) (SkillDefinitionWithFiles, error)
	DeleteWorkspaceSkill(ctx context.Context, workspaceID, slug string) (bool, error)
	ListWorkspaceMCPServers(ctx context.Context, workspaceID string) ([]MCPServerDefinitionWithEnv, error)
	GetWorkspaceMCPServer(ctx context.Context, workspaceID, name string) (MCPServerDefinitionWithEnv, bool, error)
	UpsertWorkspaceMCPServer(ctx context.Context, workspaceID string, server MCPServerDefinitionWithEnv) (MCPServerDefinitionWithEnv, error)
	DeleteWorkspaceMCPServer(ctx context.Context, workspaceID, name string) (bool, error)
	Append(ctx context.Context, event protocol.UniversalEvent) (StoredEvent, error)
	ListEventsBySession(ctx context.Context, sessionID string, afterID int64, limit int) ([]StoredEvent, error)
	ListMessagesBySession(ctx context.Context, sessionID string) ([]MessageProjection, error)
	StartRun(ctx context.Context, turn protocol.TurnRequest) (SessionRun, error)
	FinishRun(ctx context.Context, sessionID, runID, status string, eventError *protocol.EventErrorPayload) (SessionRun, error)
	GetRun(ctx context.Context, sessionID, runID string) (SessionRun, bool, error)
	RequestRunInterrupt(ctx context.Context, sessionID, expectedRunID, reason string) (RunInterruptResult, error)
	SessionState(ctx context.Context, sessionID string) (SessionState, error)
}

type StoredEvent struct {
	ID        int64                   `json:"id"`
	Type      string                  `json:"type"`
	Payload   protocol.UniversalEvent `json:"payload"`
	CreatedAt time.Time               `json:"createdAt"`
}

type MessageProjection struct {
	SessionID   string                      `json:"sessionId"`
	MessageID   string                      `json:"messageId"`
	WorkspaceID string                      `json:"workspaceId"`
	RunID       string                      `json:"runId"`
	Role        string                      `json:"role"`
	Status      string                      `json:"status"`
	Blocks      []protocol.UniversalBlock   `json:"blocks"`
	Error       *protocol.EventErrorPayload `json:"error,omitempty"`
	CreatedAt   time.Time                   `json:"createdAt"`
	UpdatedAt   time.Time                   `json:"updatedAt"`
}

type TaskProjection struct {
	TaskID      string    `json:"taskId"`
	WorkspaceID string    `json:"workspaceId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SessionProjection struct {
	SessionID   string         `json:"sessionId"`
	TaskID      string         `json:"taskId"`
	WorkspaceID string         `json:"workspaceId"`
	ModelID     string         `json:"modelId,omitempty"`
	Title       string         `json:"title,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type LLMConnection struct {
	ID          string    `json:"id"`
	UserID      string    `json:"userId"`
	WorkspaceID string    `json:"workspaceId"`
	Provider    string    `json:"provider"`
	APIProtocol string    `json:"apiProtocol"`
	BaseURL     string    `json:"baseUrl"`
	APIKey      string    `json:"apiKey,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type LLMConnectionModel struct {
	ID           string         `json:"id"`
	ConnectionID string         `json:"connectionId"`
	ModelID      string         `json:"modelId"`
	Source       string         `json:"source"`
	Enabled      bool           `json:"enabled"`
	Raw          map[string]any `json:"raw,omitempty"`
	LastSeenAt   *time.Time     `json:"lastSeenAt,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type SessionRun struct {
	RunID             string                      `json:"runId"`
	TaskID            string                      `json:"taskId"`
	SessionID         string                      `json:"sessionId"`
	WorkspaceID       string                      `json:"workspaceId"`
	Status            string                      `json:"status"`
	StartedAt         time.Time                   `json:"startedAt"`
	EndedAt           *time.Time                  `json:"endedAt,omitempty"`
	LastEventID       int64                       `json:"lastEventId"`
	Error             *protocol.EventErrorPayload `json:"error,omitempty"`
	CancelRequestedAt *time.Time                  `json:"cancelRequestedAt,omitempty"`
	CreatedAt         time.Time                   `json:"createdAt"`
	UpdatedAt         time.Time                   `json:"updatedAt"`
}

type SessionState struct {
	SessionID     string              `json:"sessionId"`
	Messages      []MessageProjection `json:"messages"`
	ActiveRun     *SessionRun         `json:"activeRun,omitempty"`
	LatestEventID int64               `json:"latestEventId"`
}

type RunInterruptResult struct {
	Interrupted   bool        `json:"interrupted"`
	Reason        string      `json:"reason,omitempty"`
	ExpectedRunID string      `json:"expectedRunId"`
	ActiveRun     *SessionRun `json:"activeRun,omitempty"`
	Run           *SessionRun `json:"run,omitempty"`
}

type SkillDefinition struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Source      string    `json:"source"`
	ScopeType   string    `json:"scopeType"`
	ScopeID     string    `json:"scopeId,omitempty"`
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	Version     int64     `json:"version"`
	ContentHash string    `json:"contentHash"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SkillFile struct {
	ID          string    `json:"id"`
	SkillID     string    `json:"skillId"`
	Path        string    `json:"path"`
	Content     string    `json:"content"`
	ContentHash string    `json:"contentHash"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SkillDefinitionWithFiles struct {
	Definition SkillDefinition `json:"definition"`
	Files      []SkillFile     `json:"files"`
}

type MCPServerDefinition struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	ScopeType   string    `json:"scopeType"`
	ScopeID     string    `json:"scopeId,omitempty"`
	Command     string    `json:"command"`
	Args        []string  `json:"args,omitempty"`
	Transport   string    `json:"transport"`
	Version     int64     `json:"version"`
	ContentHash string    `json:"contentHash"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type MCPServerEnv struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"serverId"`
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type MCPServerDefinitionWithEnv struct {
	Definition MCPServerDefinition `json:"definition"`
	Env        []MCPServerEnv      `json:"env"`
}

type ActiveRunConflict struct {
	ActiveRun SessionRun
}

var ErrSessionNotFound = errors.New("session not found")

func (e *ActiveRunConflict) Error() string {
	return "session already has an active run"
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	if databaseURL == "" {
		return nil, errors.New("AGENTHUB_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	store := &Store{pool: pool}
	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS workspaces (
  id TEXT PRIMARY KEY,
  pod_token TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS session_events (
  id BIGSERIAL PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  item_id TEXT,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_session_events_session_id_id ON session_events (session_id, id);
CREATE INDEX IF NOT EXISTS idx_session_events_workspace_id_id ON session_events (workspace_id, id);
CREATE INDEX IF NOT EXISTS idx_session_events_workspace_session_id ON session_events (workspace_id, session_id, id);
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  model_id TEXT NOT NULL DEFAULT '',
  title TEXT,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  active_run_id TEXT,
  active_run_version BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS session_runs (
  run_id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ended_at TIMESTAMPTZ,
  last_event_id BIGINT,
  error_code TEXT,
  error_message TEXT,
  cancel_requested_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tasks_workspace_id_id ON tasks (workspace_id, id);
CREATE INDEX IF NOT EXISTS idx_sessions_task_id_id ON sessions (task_id, id);
CREATE INDEX IF NOT EXISTS idx_sessions_active_run_id ON sessions (active_run_id);
CREATE INDEX IF NOT EXISTS idx_session_runs_session_status ON session_runs (session_id, status, started_at);
CREATE INDEX IF NOT EXISTS idx_session_runs_workspace_session ON session_runs (workspace_id, session_id, started_at);
CREATE INDEX IF NOT EXISTS idx_session_runs_task_session ON session_runs (task_id, session_id, started_at);
CREATE TABLE IF NOT EXISTS llm_connections (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL DEFAULT '',
  workspace_id TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT 'anthropic',
  api_protocol TEXT NOT NULL,
  base_url TEXT NOT NULL,
  api_key TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_connections_user_workspace ON llm_connections (user_id, workspace_id);
CREATE INDEX IF NOT EXISTS idx_llm_connections_workspace ON llm_connections (workspace_id, user_id);
CREATE TABLE IF NOT EXISTS llm_connection_models (
  id TEXT PRIMARY KEY,
  connection_id TEXT NOT NULL,
  model_id TEXT NOT NULL,
  source TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT true,
  raw JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_seen_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_connection_models_connection_model ON llm_connection_models (connection_id, model_id);
CREATE INDEX IF NOT EXISTS idx_llm_connection_models_connection_enabled ON llm_connection_models (connection_id, enabled, model_id);
CREATE TABLE IF NOT EXISTS messages (
  session_id TEXT NOT NULL,
  message_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  role TEXT NOT NULL,
  status TEXT NOT NULL,
  error_code TEXT,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (session_id, message_id)
);
CREATE TABLE IF NOT EXISTS message_blocks (
  session_id TEXT NOT NULL,
  message_id TEXT NOT NULL,
  block_index INTEGER NOT NULL,
  type TEXT NOT NULL,
  text TEXT,
  name TEXT,
  input TEXT,
  output TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (session_id, message_id, block_index)
);
CREATE INDEX IF NOT EXISTS idx_messages_session_created ON messages (session_id, created_at, message_id);
CREATE INDEX IF NOT EXISTS idx_messages_workspace_session_created ON messages (workspace_id, session_id, created_at, message_id);
CREATE INDEX IF NOT EXISTS idx_message_blocks_message_order ON message_blocks (session_id, message_id, block_index);
CREATE TABLE IF NOT EXISTS skill_definitions (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL,
  source TEXT NOT NULL,
  scope_type TEXT NOT NULL DEFAULT 'global',
  scope_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  version BIGINT NOT NULL DEFAULT 1,
  content_hash TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS skill_files (
  id TEXT PRIMARY KEY,
  skill_id TEXT NOT NULL,
  path TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_skill_definitions_scope_slug ON skill_definitions (source, scope_type, scope_id, slug);
CREATE INDEX IF NOT EXISTS idx_skill_definitions_slug_source ON skill_definitions (slug, source, updated_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_skill_files_skill_path ON skill_files (skill_id, path);
CREATE TABLE IF NOT EXISTS mcp_server_definitions (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  source TEXT NOT NULL,
  scope_type TEXT NOT NULL DEFAULT 'global',
  scope_id TEXT NOT NULL DEFAULT '',
  command TEXT NOT NULL,
  args JSONB NOT NULL DEFAULT '[]'::jsonb,
  transport TEXT NOT NULL DEFAULT 'stdio',
  version BIGINT NOT NULL DEFAULT 1,
  content_hash TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS mcp_server_env (
  id TEXT PRIMARY KEY,
  server_id TEXT NOT NULL,
  name TEXT NOT NULL,
  value TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_server_definitions_scope_name ON mcp_server_definitions (source, scope_type, scope_id, name);
CREATE INDEX IF NOT EXISTS idx_mcp_server_definitions_name_source ON mcp_server_definitions (name, source, updated_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_server_env_server_name ON mcp_server_env (server_id, name);
`)
	return err
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}
	return nil
}

func (s *Store) SaveWorkspaceToken(ctx context.Context, workspaceID string, token string) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO workspaces (id, pod_token, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (id) DO UPDATE SET pod_token = EXCLUDED.pod_token, updated_at = now()
`, workspaceID, token)
	return err
}

func (s *Store) WorkspaceToken(ctx context.Context, workspaceID string) (string, bool, error) {
	var token string
	err := s.pool.QueryRow(ctx, `SELECT pod_token FROM workspaces WHERE id = $1`, workspaceID).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

func (s *Store) CreateSession(ctx context.Context, workspaceID, taskID, sessionID string, req protocol.CreateSessionRequest) (TaskProjection, SessionProjection, error) {
	return s.createSession(ctx, workspaceID, taskID, sessionID, req)
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (SessionProjection, bool, error) {
	return s.getSession(ctx, sessionID)
}

func (s *Store) GetWorkspaceLLMConnection(ctx context.Context, workspaceID string) (LLMConnection, bool, error) {
	return s.getWorkspaceLLMConnection(ctx, workspaceID)
}

func (s *Store) UpsertWorkspaceLLMConnection(ctx context.Context, workspaceID string, connection LLMConnection) (LLMConnection, error) {
	return s.upsertWorkspaceLLMConnection(ctx, workspaceID, connection)
}

func (s *Store) ListWorkspaceLLMModels(ctx context.Context, workspaceID string) ([]LLMConnectionModel, error) {
	return s.listWorkspaceLLMModels(ctx, workspaceID)
}

func (s *Store) UpsertWorkspaceLLMModel(ctx context.Context, workspaceID string, model LLMConnectionModel) (LLMConnectionModel, error) {
	return s.upsertWorkspaceLLMModel(ctx, workspaceID, model)
}

func (s *Store) ListSkillCandidates(ctx context.Context, workspaceID string) ([]SkillDefinitionWithFiles, error) {
	return s.listSkillCandidates(ctx, workspaceID)
}

func (s *Store) ListMCPCandidates(ctx context.Context, workspaceID string) ([]MCPServerDefinitionWithEnv, error) {
	return s.listMCPCandidates(ctx, workspaceID)
}

func (s *Store) ListWorkspaceSkills(ctx context.Context, workspaceID string) ([]SkillDefinitionWithFiles, error) {
	return s.listWorkspaceSkills(ctx, workspaceID)
}

func (s *Store) GetWorkspaceSkill(ctx context.Context, workspaceID, slug string) (SkillDefinitionWithFiles, bool, error) {
	return s.getWorkspaceSkill(ctx, workspaceID, slug)
}

func (s *Store) UpsertWorkspaceSkill(ctx context.Context, workspaceID string, skill SkillDefinitionWithFiles) (SkillDefinitionWithFiles, error) {
	return s.upsertWorkspaceSkill(ctx, workspaceID, skill)
}

func (s *Store) DeleteWorkspaceSkill(ctx context.Context, workspaceID, slug string) (bool, error) {
	return s.deleteWorkspaceSkill(ctx, workspaceID, slug)
}

func (s *Store) ListWorkspaceMCPServers(ctx context.Context, workspaceID string) ([]MCPServerDefinitionWithEnv, error) {
	return s.listWorkspaceMCPServers(ctx, workspaceID)
}

func (s *Store) GetWorkspaceMCPServer(ctx context.Context, workspaceID, name string) (MCPServerDefinitionWithEnv, bool, error) {
	return s.getWorkspaceMCPServer(ctx, workspaceID, name)
}

func (s *Store) UpsertWorkspaceMCPServer(ctx context.Context, workspaceID string, server MCPServerDefinitionWithEnv) (MCPServerDefinitionWithEnv, error) {
	return s.upsertWorkspaceMCPServer(ctx, workspaceID, server)
}

func (s *Store) DeleteWorkspaceMCPServer(ctx context.Context, workspaceID, name string) (bool, error) {
	return s.deleteWorkspaceMCPServer(ctx, workspaceID, name)
}

func (s *Store) Append(ctx context.Context, event protocol.UniversalEvent) (StoredEvent, error) {
	return s.append(ctx, event)
}

func (s *Store) ListEventsBySession(ctx context.Context, sessionID string, afterID int64, limit int) ([]StoredEvent, error) {
	return s.listEventsBySession(ctx, sessionID, afterID, limit)
}

func (s *Store) ListMessagesBySession(ctx context.Context, sessionID string) ([]MessageProjection, error) {
	return s.listMessagesBySession(ctx, sessionID)
}

func (s *Store) StartRun(ctx context.Context, turn protocol.TurnRequest) (SessionRun, error) {
	return s.startRun(ctx, turn)
}

func (s *Store) FinishRun(ctx context.Context, sessionID, runID, status string, eventError *protocol.EventErrorPayload) (SessionRun, error) {
	return s.finishRun(ctx, sessionID, runID, status, eventError)
}

func (s *Store) GetRun(ctx context.Context, sessionID, runID string) (SessionRun, bool, error) {
	return s.getRun(ctx, sessionID, runID)
}

func (s *Store) RequestRunInterrupt(ctx context.Context, sessionID, expectedRunID, reason string) (RunInterruptResult, error) {
	return s.requestRunInterrupt(ctx, sessionID, expectedRunID, reason)
}

func (s *Store) SessionState(ctx context.Context, sessionID string) (SessionState, error) {
	return s.sessionState(ctx, sessionID)
}
