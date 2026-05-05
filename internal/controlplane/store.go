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

	DefaultEventReplayLimit = 500
	MaxEventReplayLimit     = 1000
)

type EventStore interface {
	Ping(ctx context.Context) error
	SaveWorkspaceToken(ctx context.Context, workspaceID string, token string) error
	WorkspaceToken(ctx context.Context, workspaceID string) (string, bool, error)
	Append(ctx context.Context, event protocol.UniversalEvent) (StoredEvent, error)
	ListEventsBySession(ctx context.Context, sessionID string, afterID int64, limit int) ([]StoredEvent, error)
	ListMessagesBySession(ctx context.Context, sessionID string) ([]MessageProjection, error)
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
ALTER TABLE messages ADD COLUMN IF NOT EXISTS run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'completed';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS error_code TEXT;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS error_message TEXT;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE messages ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
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
  PRIMARY KEY (session_id, message_id, block_index),
  FOREIGN KEY (session_id, message_id) REFERENCES messages (session_id, message_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_messages_session_created ON messages (session_id, created_at, message_id);
CREATE INDEX IF NOT EXISTS idx_messages_workspace_session_created ON messages (workspace_id, session_id, created_at, message_id);
CREATE INDEX IF NOT EXISTS idx_message_blocks_message_order ON message_blocks (session_id, message_id, block_index);
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

func (s *Store) Append(ctx context.Context, event protocol.UniversalEvent) (StoredEvent, error) {
	return s.append(ctx, event)
}

func (s *Store) ListEventsBySession(ctx context.Context, sessionID string, afterID int64, limit int) ([]StoredEvent, error) {
	return s.listEventsBySession(ctx, sessionID, afterID, limit)
}

func (s *Store) ListMessagesBySession(ctx context.Context, sessionID string) ([]MessageProjection, error) {
	return s.listMessagesBySession(ctx, sessionID)
}
