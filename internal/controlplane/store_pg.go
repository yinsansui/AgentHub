package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
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
