package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"agenthub/pkg/protocol"
)

func (s *Store) append(ctx context.Context, event protocol.UniversalEvent) (StoredEvent, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return StoredEvent{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return StoredEvent{}, err
	}
	defer tx.Rollback(ctx)
	var stored StoredEvent
	err = tx.QueryRow(ctx, `
INSERT INTO session_events (workspace_id, session_id, run_id, event_type, item_id, payload)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)
RETURNING id, event_type, created_at
`, event.WorkspaceID, event.SessionID, event.RunID, event.Type, nullIfEmpty(projectionItemID(event)), payload).Scan(&stored.ID, &stored.Type, &stored.CreatedAt)
	if err != nil {
		return StoredEvent{}, err
	}
	if err := projectMessage(ctx, tx, event); err != nil {
		return StoredEvent{}, err
	}
	if event.RunID != "" {
		if _, err := tx.Exec(ctx, `
UPDATE session_runs
SET last_event_id = $1, updated_at = now()
WHERE session_id = $2 AND run_id = $3
`, stored.ID, event.SessionID, event.RunID); err != nil {
			return StoredEvent{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return StoredEvent{}, err
	}
	stored.Payload = event
	return stored, nil
}

func (s *Store) listEventsBySession(ctx context.Context, sessionID string, afterID int64, limit int) ([]StoredEvent, error) {
	if limit <= 0 || limit > MaxEventReplayLimit {
		limit = DefaultEventReplayLimit
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, event_type, payload, created_at
FROM session_events
WHERE session_id = $1 AND id > $2
ORDER BY id ASC
LIMIT $3
`, sessionID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []StoredEvent
	for rows.Next() {
		var stored StoredEvent
		var payload []byte
		if err := rows.Scan(&stored.ID, &stored.Type, &payload, &stored.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &stored.Payload); err != nil {
			return nil, err
		}
		if stored.Payload.Type == "" {
			stored.Payload.Type = stored.Type
		}
		events = append(events, stored)
	}
	return events, rows.Err()
}

func (s *Store) listMessagesBySession(ctx context.Context, sessionID string) ([]MessageProjection, error) {
	rows, err := s.pool.Query(ctx, `
SELECT
  m.session_id, m.message_id, m.workspace_id, m.run_id, m.role, m.status,
  m.error_code, m.error_message, m.created_at, m.updated_at,
  b.block_index, b.type, b.text, b.name, b.input, b.output
FROM messages m
LEFT JOIN message_blocks b ON b.session_id = m.session_id AND b.message_id = m.message_id
WHERE m.session_id = $1
ORDER BY m.created_at ASC, m.message_id ASC, b.block_index ASC
`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]MessageProjection, 0)
	messageByID := map[string]int{}
	for rows.Next() {
		var message MessageProjection
		var errorCode sql.NullString
		var errorMessage sql.NullString
		var blockIndex sql.NullInt32
		var blockType, blockText, blockName, blockInput, blockOutput sql.NullString
		if err := rows.Scan(
			&message.SessionID,
			&message.MessageID,
			&message.WorkspaceID,
			&message.RunID,
			&message.Role,
			&message.Status,
			&errorCode,
			&errorMessage,
			&message.CreatedAt,
			&message.UpdatedAt,
			&blockIndex,
			&blockType,
			&blockText,
			&blockName,
			&blockInput,
			&blockOutput,
		); err != nil {
			return nil, err
		}
		idx, ok := messageByID[message.MessageID]
		if !ok {
			if errorCode.Valid || errorMessage.Valid {
				message.Error = &protocol.EventErrorPayload{Code: errorCode.String, Message: errorMessage.String}
			}
			message.Blocks = []protocol.UniversalBlock{}
			messages = append(messages, message)
			idx = len(messages) - 1
			messageByID[message.MessageID] = idx
		}
		if blockIndex.Valid {
			messages[idx].Blocks = append(messages[idx].Blocks, protocol.UniversalBlock{
				Type:   blockType.String,
				Text:   blockText.String,
				Name:   blockName.String,
				Input:  blockInput.String,
				Output: blockOutput.String,
			})
		}
	}
	return messages, rows.Err()
}

func (s *Store) createSession(ctx context.Context, workspaceID, taskID, sessionID string, req protocol.CreateSessionRequest) (TaskProjection, SessionProjection, error) {
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	payload, err := json.Marshal(req.Metadata)
	if err != nil {
		return TaskProjection{}, SessionProjection{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TaskProjection{}, SessionProjection{}, err
	}
	defer tx.Rollback(ctx)
	var task TaskProjection
	err = tx.QueryRow(ctx, `
INSERT INTO tasks (id, workspace_id, updated_at)
VALUES ($1, $2, now())
RETURNING id, workspace_id, created_at, updated_at
`, taskID, workspaceID).Scan(&task.TaskID, &task.WorkspaceID, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return TaskProjection{}, SessionProjection{}, err
	}
	var session SessionProjection
	var metadata []byte
	err = tx.QueryRow(ctx, `
INSERT INTO sessions (id, task_id, workspace_id, title, metadata, updated_at)
VALUES ($1, $2, $3, $4, $5::jsonb, now())
RETURNING id, task_id, workspace_id, COALESCE(title, ''), metadata, created_at, updated_at
`, sessionID, taskID, workspaceID, nullIfEmpty(req.Title), payload).Scan(&session.SessionID, &session.TaskID, &session.WorkspaceID, &session.Title, &metadata, &session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		return TaskProjection{}, SessionProjection{}, err
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &session.Metadata)
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskProjection{}, SessionProjection{}, err
	}
	return task, session, nil
}

func (s *Store) getSession(ctx context.Context, sessionID string) (SessionProjection, bool, error) {
	var session SessionProjection
	var metadata []byte
	err := s.pool.QueryRow(ctx, `
SELECT id, task_id, workspace_id, COALESCE(title, ''), metadata, created_at, updated_at
FROM sessions
WHERE id = $1
`, sessionID).Scan(&session.SessionID, &session.TaskID, &session.WorkspaceID, &session.Title, &metadata, &session.CreatedAt, &session.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionProjection{}, false, nil
	}
	if err != nil {
		return SessionProjection{}, false, err
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &session.Metadata)
	}
	return session, true, nil
}

func (s *Store) startRun(ctx context.Context, turn protocol.TurnRequest) (SessionRun, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SessionRun{}, err
	}
	defer tx.Rollback(ctx)

	var activeRunID sql.NullString
	var taskID string
	if err := tx.QueryRow(ctx, `SELECT task_id, active_run_id FROM sessions WHERE id = $1 FOR UPDATE`, turn.SessionID).Scan(&taskID, &activeRunID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionRun{}, ErrSessionNotFound
		}
		return SessionRun{}, err
	}
	if turn.TaskID == "" {
		turn.TaskID = taskID
	}
	if activeRunID.Valid && activeRunID.String != "" {
		active, ok, err := queryRun(ctx, tx, turn.SessionID, activeRunID.String)
		if err != nil {
			return SessionRun{}, err
		}
		if ok && !isTerminalRunStatus(active.Status) {
			return SessionRun{}, &ActiveRunConflict{ActiveRun: active}
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET active_run_id = NULL, updated_at = now() WHERE id = $1`, turn.SessionID); err != nil {
			return SessionRun{}, err
		}
	}

	run, err := scanRun(tx.QueryRow(ctx, `
INSERT INTO session_runs (run_id, task_id, session_id, workspace_id, status, started_at, updated_at)
VALUES ($1, $2, $3, $4, $5, now(), now())
RETURNING run_id, task_id, session_id, workspace_id, status, started_at, ended_at, COALESCE(last_event_id, 0), error_code, error_message, cancel_requested_at, created_at, updated_at
`, turn.RunID, turn.TaskID, turn.SessionID, turn.WorkspaceID, RunStatusRunning))
	if err != nil {
		return SessionRun{}, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE sessions
SET active_run_id = $2, active_run_version = active_run_version + 1, updated_at = now()
WHERE id = $1
`, turn.SessionID, turn.RunID); err != nil {
		return SessionRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SessionRun{}, err
	}
	return run, nil
}

func (s *Store) finishRun(ctx context.Context, sessionID, runID, status string, eventError *protocol.EventErrorPayload) (SessionRun, error) {
	var errorCode any
	var errorMessage any
	if eventError != nil {
		errorCode = nullIfEmpty(eventError.Code)
		errorMessage = eventError.Message
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SessionRun{}, err
	}
	defer tx.Rollback(ctx)

	run, err := scanRun(tx.QueryRow(ctx, `
UPDATE session_runs
SET status = $3,
    ended_at = COALESCE(ended_at, now()),
    error_code = $4,
    error_message = $5,
    updated_at = now()
WHERE session_id = $1 AND run_id = $2
RETURNING run_id, task_id, session_id, workspace_id, status, started_at, ended_at, COALESCE(last_event_id, 0), error_code, error_message, cancel_requested_at, created_at, updated_at
`, sessionID, runID, status, errorCode, errorMessage))
	if err != nil {
		return SessionRun{}, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE sessions
SET active_run_id = NULL,
    active_run_version = active_run_version + 1,
    updated_at = now()
WHERE id = $1 AND active_run_id = $2
`, sessionID, runID); err != nil {
		return SessionRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SessionRun{}, err
	}
	return run, nil
}

func (s *Store) getRun(ctx context.Context, sessionID, runID string) (SessionRun, bool, error) {
	return queryRun(ctx, s.pool, sessionID, runID)
}

func (s *Store) requestRunInterrupt(ctx context.Context, sessionID, expectedRunID, reason string) (RunInterruptResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RunInterruptResult{}, err
	}
	defer tx.Rollback(ctx)

	var activeRunID sql.NullString
	err = tx.QueryRow(ctx, `SELECT active_run_id FROM sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&activeRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunInterruptResult{Interrupted: false, Reason: "no_active_run", ExpectedRunID: expectedRunID}, nil
	}
	if err != nil {
		return RunInterruptResult{}, err
	}
	if !activeRunID.Valid || activeRunID.String == "" {
		return RunInterruptResult{Interrupted: false, Reason: "no_active_run", ExpectedRunID: expectedRunID}, nil
	}

	active, ok, err := queryRun(ctx, tx, sessionID, activeRunID.String)
	if err != nil {
		return RunInterruptResult{}, err
	}
	if !ok {
		return RunInterruptResult{Interrupted: false, Reason: "no_active_run", ExpectedRunID: expectedRunID}, nil
	}
	if active.RunID != expectedRunID {
		return RunInterruptResult{Interrupted: false, Reason: "run_mismatch", ExpectedRunID: expectedRunID, ActiveRun: &active}, nil
	}
	if isTerminalRunStatus(active.Status) {
		return RunInterruptResult{Interrupted: false, Reason: "already_terminal", ExpectedRunID: expectedRunID, Run: &active}, nil
	}
	if active.Status == RunStatusCancelling {
		return RunInterruptResult{Interrupted: true, ExpectedRunID: expectedRunID, Run: &active}, nil
	}

	run, err := scanRun(tx.QueryRow(ctx, `
UPDATE session_runs
SET status = $3,
    cancel_requested_at = now(),
    updated_at = now()
WHERE session_id = $1 AND run_id = $2
RETURNING run_id, task_id, session_id, workspace_id, status, started_at, ended_at, COALESCE(last_event_id, 0), error_code, error_message, cancel_requested_at, created_at, updated_at
`, sessionID, expectedRunID, RunStatusCancelling))
	if err != nil {
		return RunInterruptResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RunInterruptResult{}, err
	}
	return RunInterruptResult{Interrupted: true, ExpectedRunID: expectedRunID, Run: &run}, nil
}

func (s *Store) sessionState(ctx context.Context, sessionID string) (SessionState, error) {
	messages, err := s.listMessagesBySession(ctx, sessionID)
	if err != nil {
		return SessionState{}, err
	}
	state := SessionState{SessionID: sessionID, Messages: messages}
	var activeRunID sql.NullString
	err = s.pool.QueryRow(ctx, `SELECT active_run_id FROM sessions WHERE id = $1`, sessionID).Scan(&activeRunID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SessionState{}, err
	}
	if activeRunID.Valid && activeRunID.String != "" {
		run, ok, err := s.getRun(ctx, sessionID, activeRunID.String)
		if err != nil {
			return SessionState{}, err
		}
		if ok && !isTerminalRunStatus(run.Status) {
			state.ActiveRun = &run
		}
	}
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM session_events WHERE session_id = $1`, sessionID).Scan(&state.LatestEventID); err != nil {
		return SessionState{}, err
	}
	return state, nil
}

type sqlProjector interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func projectMessage(ctx context.Context, exec sqlProjector, event protocol.UniversalEvent) error {
	if event.SessionID == "" {
		return nil
	}
	messageID := projectionItemID(event)
	switch event.Type {
	case protocol.EventItemStarted:
		if messageID == "" {
			return nil
		}
		return upsertMessage(ctx, exec, event, messageID, MessageStatusStreaming, nil)
	case protocol.EventItemDelta:
		if messageID == "" {
			return nil
		}
		return upsertMessage(ctx, exec, event, messageID, MessageStatusStreaming, nil)
	case protocol.EventItemCompleted:
		if messageID == "" {
			return nil
		}
		if err := upsertMessage(ctx, exec, event, messageID, MessageStatusCompleted, nil); err != nil {
			return err
		}
		blocks := eventBlocks(event)
		if len(blocks) == 0 {
			return nil
		}
		return replaceBlocks(ctx, exec, event.SessionID, messageID, blocks)
	case protocol.EventError:
		if event.Error == nil {
			return nil
		}
		if messageID == "" {
			messageID = errorMessageID(event)
		}
		if err := upsertMessage(ctx, exec, event, messageID, MessageStatusError, event.Error); err != nil {
			return err
		}
		count, err := blockCount(ctx, exec, event.SessionID, messageID)
		if err != nil {
			return err
		}
		if count == 0 {
			return appendBlock(ctx, exec, event.SessionID, messageID, protocol.UniversalBlock{Type: "text", Text: event.Error.Message})
		}
	}
	return nil
}

func upsertMessage(ctx context.Context, exec sqlProjector, event protocol.UniversalEvent, messageID, status string, eventError *protocol.EventErrorPayload) error {
	var errorCode any
	var errorMessage any
	if eventError != nil {
		errorCode = nullIfEmpty(eventError.Code)
		errorMessage = eventError.Message
	}
	_, err := exec.Exec(ctx, `
INSERT INTO messages (session_id, message_id, workspace_id, run_id, role, status, error_code, error_message, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
ON CONFLICT (session_id, message_id) DO UPDATE SET
  workspace_id = EXCLUDED.workspace_id,
  run_id = EXCLUDED.run_id,
  role = EXCLUDED.role,
  status = EXCLUDED.status,
  error_code = EXCLUDED.error_code,
  error_message = EXCLUDED.error_message,
  updated_at = now()
`, event.SessionID, messageID, event.WorkspaceID, event.RunID, eventRole(event), status, errorCode, errorMessage)
	return err
}

func appendBlock(ctx context.Context, exec sqlProjector, sessionID, messageID string, block protocol.UniversalBlock) error {
	if block.Type == "" {
		block.Type = "text"
	}
	_, err := exec.Exec(ctx, `
INSERT INTO message_blocks (session_id, message_id, block_index, type, text, name, input, output)
VALUES (
  $1,
  $2,
  COALESCE((SELECT max(block_index) + 1 FROM message_blocks WHERE session_id = $1 AND message_id = $2), 0),
  $3,
  $4,
  $5,
  $6,
  $7
)
`, sessionID, messageID, block.Type, nullIfEmpty(block.Text), nullIfEmpty(block.Name), nullIfEmpty(block.Input), nullIfEmpty(block.Output))
	return err
}

func replaceBlocks(ctx context.Context, exec sqlProjector, sessionID, messageID string, blocks []protocol.UniversalBlock) error {
	if _, err := exec.Exec(ctx, `DELETE FROM message_blocks WHERE session_id = $1 AND message_id = $2`, sessionID, messageID); err != nil {
		return err
	}
	for index, block := range blocks {
		if block.Type == "" {
			block.Type = "text"
		}
		if _, err := exec.Exec(ctx, `
INSERT INTO message_blocks (session_id, message_id, block_index, type, text, name, input, output)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, sessionID, messageID, index, block.Type, nullIfEmpty(block.Text), nullIfEmpty(block.Name), nullIfEmpty(block.Input), nullIfEmpty(block.Output)); err != nil {
			return err
		}
	}
	return nil
}

func blockCount(ctx context.Context, exec sqlProjector, sessionID, messageID string) (int, error) {
	var count int
	err := exec.QueryRow(ctx, `
SELECT count(*)
FROM message_blocks
WHERE session_id = $1 AND message_id = $2
`, sessionID, messageID).Scan(&count)
	return count, err
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func projectionItemID(event protocol.UniversalEvent) string {
	if event.ItemID != "" {
		return event.ItemID
	}
	if event.Item != nil {
		return event.Item.ID
	}
	return ""
}

func eventRole(event protocol.UniversalEvent) string {
	if event.Role != "" {
		return event.Role
	}
	if event.Item != nil && event.Item.Role != "" {
		return event.Item.Role
	}
	return "assistant"
}

func eventBlocks(event protocol.UniversalEvent) []protocol.UniversalBlock {
	if event.Item == nil {
		return nil
	}
	return event.Item.Content
}

func errorMessageID(event protocol.UniversalEvent) string {
	if event.RunID != "" {
		return "err_" + event.RunID
	}
	if event.Timestamp != "" {
		return "err_" + strings.NewReplacer(":", "", ".", "", "-", "").Replace(event.Timestamp)
	}
	return "err_" + time.Now().UTC().Format("20060102150405.000000000")
}

type runQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func queryRun(ctx context.Context, q runQuerier, sessionID, runID string) (SessionRun, bool, error) {
	run, err := scanRun(q.QueryRow(ctx, `
SELECT run_id, task_id, session_id, workspace_id, status, started_at, ended_at, COALESCE(last_event_id, 0), error_code, error_message, cancel_requested_at, created_at, updated_at
FROM session_runs
WHERE session_id = $1 AND run_id = $2
`, sessionID, runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRun{}, false, nil
	}
	if err != nil {
		return SessionRun{}, false, err
	}
	return run, true, nil
}

func scanRun(row pgx.Row) (SessionRun, error) {
	var run SessionRun
	var endedAt sql.NullTime
	var errorCode sql.NullString
	var errorMessage sql.NullString
	var cancelRequestedAt sql.NullTime
	err := row.Scan(
		&run.RunID,
		&run.TaskID,
		&run.SessionID,
		&run.WorkspaceID,
		&run.Status,
		&run.StartedAt,
		&endedAt,
		&run.LastEventID,
		&errorCode,
		&errorMessage,
		&cancelRequestedAt,
		&run.CreatedAt,
		&run.UpdatedAt,
	)
	if err != nil {
		return SessionRun{}, err
	}
	if endedAt.Valid {
		run.EndedAt = &endedAt.Time
	}
	if errorCode.Valid || errorMessage.Valid {
		run.Error = &protocol.EventErrorPayload{Code: errorCode.String, Message: errorMessage.String}
	}
	if cancelRequestedAt.Valid {
		run.CancelRequestedAt = &cancelRequestedAt.Time
	}
	return run, nil
}

func isTerminalRunStatus(status string) bool {
	return status == RunStatusCompleted || status == RunStatusFailed || status == RunStatusCancelled
}
