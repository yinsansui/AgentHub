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
`, event.WorkspaceID, event.SessionID, event.RunID, event.Type, nullIfEmpty(projectionMessageID(event)), payload).Scan(&stored.ID, &stored.Type, &stored.CreatedAt)
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

func (s *Store) listWorkspaces(ctx context.Context, ownerUserID string, limit, offset int) ([]WorkspaceProjection, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, name, owner_user_id
FROM workspaces
WHERE owner_user_id = $1
ORDER BY id ASC
LIMIT $2 OFFSET $3
`, ownerUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	workspaces := []WorkspaceProjection{}
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, rows.Err()
}

func (s *Store) createWorkspace(ctx context.Context, ownerUserID, name string) (WorkspaceProjection, error) {
	for range 3 {
		workspaceID, err := newWorkspaceUUID()
		if err != nil {
			return WorkspaceProjection{}, err
		}
		saved, err := scanWorkspace(s.pool.QueryRow(ctx, `
INSERT INTO workspaces (id, name, owner_user_id)
VALUES ($1, $2, $3)
RETURNING id, name, owner_user_id
`, workspaceID, name, ownerUserID))
		if isUniqueViolation(err) {
			continue
		}
		return saved, err
	}
	return WorkspaceProjection{}, ErrWorkspaceExists
}

func (s *Store) getWorkspace(ctx context.Context, ownerUserID, workspaceID string) (WorkspaceProjection, bool, error) {
	workspace, err := scanWorkspace(s.pool.QueryRow(ctx, `
SELECT id, name, owner_user_id
FROM workspaces
WHERE owner_user_id = $1 AND id = $2
`, ownerUserID, workspaceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkspaceProjection{}, false, nil
	}
	if err != nil {
		return WorkspaceProjection{}, false, err
	}
	return workspace, true, nil
}

func (s *Store) updateWorkspace(ctx context.Context, ownerUserID, workspaceID, name string) (WorkspaceProjection, bool, error) {
	saved, err := scanWorkspace(s.pool.QueryRow(ctx, `
UPDATE workspaces
SET name = $3
WHERE owner_user_id = $1 AND id = $2
RETURNING id, name, owner_user_id
`, ownerUserID, workspaceID, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkspaceProjection{}, false, nil
	}
	if err != nil {
		return WorkspaceProjection{}, false, err
	}
	return saved, true, nil
}

func (s *Store) deleteWorkspace(ctx context.Context, ownerUserID, workspaceID string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var ownedWorkspaceID string
	err = tx.QueryRow(ctx, `
SELECT id
FROM workspaces
WHERE owner_user_id = $1 AND id = $2
FOR UPDATE
`, ownerUserID, workspaceID).Scan(&ownedWorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}

	deleteStatements := []struct {
		sql  string
		args []any
	}{
		{sql: `
DELETE FROM message_blocks b
USING messages m
WHERE b.session_id = m.session_id AND b.message_id = m.message_id AND m.workspace_id = $1
`, args: []any{ownedWorkspaceID}},
		{sql: `
DELETE FROM llm_connection_models m
USING llm_connections c
WHERE m.connection_id = c.id AND c.workspace_id = $1
`, args: []any{ownedWorkspaceID}},
		{sql: `
DELETE FROM skill_files f
USING skill_definitions d
WHERE f.skill_id = d.id AND d.source = $1 AND d.scope_type = 'workspace' AND d.scope_id = $2
`, args: []any{protocol.SkillSourceWorkspace, ownedWorkspaceID}},
		{sql: `
DELETE FROM mcp_server_env e
USING mcp_server_definitions d
WHERE e.server_id = d.id AND d.source = $1 AND d.scope_type = 'workspace' AND d.scope_id = $2
`, args: []any{protocol.SkillSourceWorkspace, ownedWorkspaceID}},
		{sql: `DELETE FROM messages WHERE workspace_id = $1`, args: []any{ownedWorkspaceID}},
		{sql: `DELETE FROM session_events WHERE workspace_id = $1`, args: []any{ownedWorkspaceID}},
		{sql: `DELETE FROM session_runs WHERE workspace_id = $1`, args: []any{ownedWorkspaceID}},
		{sql: `DELETE FROM sessions WHERE workspace_id = $1`, args: []any{ownedWorkspaceID}},
		{sql: `DELETE FROM tasks WHERE workspace_id = $1`, args: []any{ownedWorkspaceID}},
		{sql: `DELETE FROM llm_connections WHERE workspace_id = $1`, args: []any{ownedWorkspaceID}},
		{sql: `DELETE FROM skill_definitions WHERE source = $1 AND scope_type = 'workspace' AND scope_id = $2`, args: []any{protocol.SkillSourceWorkspace, ownedWorkspaceID}},
		{sql: `DELETE FROM mcp_server_definitions WHERE source = $1 AND scope_type = 'workspace' AND scope_id = $2`, args: []any{protocol.SkillSourceWorkspace, ownedWorkspaceID}},
		{sql: `DELETE FROM workspace_tokens WHERE workspace_id = $1`, args: []any{ownedWorkspaceID}},
	}
	for _, statement := range deleteStatements {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			return false, err
		}
	}

	tag, err := tx.Exec(ctx, `DELETE FROM workspaces WHERE owner_user_id = $1 AND id = $2`, ownerUserID, ownedWorkspaceID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

type workspaceScanner interface {
	Scan(dest ...any) error
}

func scanWorkspace(row workspaceScanner) (WorkspaceProjection, error) {
	var workspace WorkspaceProjection
	if err := row.Scan(
		&workspace.ID,
		&workspace.Name,
		&workspace.OwnerUserID,
	); err != nil {
		return WorkspaceProjection{}, err
	}
	return workspace, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
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
INSERT INTO sessions (id, task_id, workspace_id, model_id, title, metadata, updated_at)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, now())
RETURNING id, task_id, workspace_id, model_id, COALESCE(title, ''), metadata, created_at, updated_at
`, sessionID, taskID, workspaceID, req.ModelID, nullIfEmpty(req.Title), payload).Scan(&session.SessionID, &session.TaskID, &session.WorkspaceID, &session.ModelID, &session.Title, &metadata, &session.CreatedAt, &session.UpdatedAt)
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
SELECT id, task_id, workspace_id, model_id, COALESCE(title, ''), metadata, created_at, updated_at
FROM sessions
WHERE id = $1
`, sessionID).Scan(&session.SessionID, &session.TaskID, &session.WorkspaceID, &session.ModelID, &session.Title, &metadata, &session.CreatedAt, &session.UpdatedAt)
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

func (s *Store) listWorkspaceSessions(ctx context.Context, workspaceID string, limit, offset int) ([]SessionProjection, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, task_id, workspace_id, model_id, COALESCE(title, ''), metadata, created_at, updated_at
FROM sessions
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3
`, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []SessionProjection
	for rows.Next() {
		var session SessionProjection
		var metadata []byte
		if err := rows.Scan(&session.SessionID, &session.TaskID, &session.WorkspaceID, &session.ModelID, &session.Title, &metadata, &session.CreatedAt, &session.UpdatedAt); err != nil {
			return nil, err
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &session.Metadata)
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) getWorkspaceLLMConnection(ctx context.Context, workspaceID string) (LLMConnection, bool, error) {
	var connection LLMConnection
	err := s.pool.QueryRow(ctx, `
SELECT id, user_id, workspace_id, provider, api_protocol, base_url, api_key, created_at, updated_at
FROM llm_connections
WHERE user_id = '' AND workspace_id = $1
`, workspaceID).Scan(
		&connection.ID,
		&connection.UserID,
		&connection.WorkspaceID,
		&connection.Provider,
		&connection.APIProtocol,
		&connection.BaseURL,
		&connection.APIKey,
		&connection.CreatedAt,
		&connection.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return LLMConnection{}, false, nil
	}
	if err != nil {
		return LLMConnection{}, false, err
	}
	return connection, true, nil
}

func (s *Store) upsertWorkspaceLLMConnection(ctx context.Context, workspaceID string, connection LLMConnection) (LLMConnection, error) {
	err := s.pool.QueryRow(ctx, `
INSERT INTO llm_connections (id, user_id, workspace_id, provider, api_protocol, base_url, api_key, updated_at)
VALUES ($1, '', $2, $3, $4, $5, $6, now())
ON CONFLICT (user_id, workspace_id)
DO UPDATE SET
  provider = EXCLUDED.provider,
  api_protocol = EXCLUDED.api_protocol,
  base_url = EXCLUDED.base_url,
  api_key = EXCLUDED.api_key,
  updated_at = now()
RETURNING id, user_id, workspace_id, provider, api_protocol, base_url, api_key, created_at, updated_at
`, connection.ID, workspaceID, connection.Provider, connection.APIProtocol, connection.BaseURL, connection.APIKey).Scan(
		&connection.ID,
		&connection.UserID,
		&connection.WorkspaceID,
		&connection.Provider,
		&connection.APIProtocol,
		&connection.BaseURL,
		&connection.APIKey,
		&connection.CreatedAt,
		&connection.UpdatedAt,
	)
	if err != nil {
		return LLMConnection{}, err
	}
	return connection, nil
}

func (s *Store) listWorkspaceLLMModels(ctx context.Context, workspaceID string) ([]LLMConnectionModel, error) {
	connection, ok, err := s.getWorkspaceLLMConnection(ctx, workspaceID)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, connection_id, model_id, source, enabled, raw, last_seen_at, created_at, updated_at
FROM llm_connection_models
WHERE connection_id = $1
ORDER BY enabled DESC, model_id ASC
`, connection.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	models := []LLMConnectionModel{}
	for rows.Next() {
		model, err := scanLLMConnectionModel(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	return models, rows.Err()
}

func (s *Store) upsertWorkspaceLLMModel(ctx context.Context, workspaceID string, model LLMConnectionModel) (LLMConnectionModel, error) {
	connection, ok, err := s.getWorkspaceLLMConnection(ctx, workspaceID)
	if err != nil {
		return LLMConnectionModel{}, err
	}
	if !ok {
		return LLMConnectionModel{}, errors.New("llm connection is not configured")
	}
	rawPayload, err := json.Marshal(model.Raw)
	if err != nil {
		return LLMConnectionModel{}, err
	}
	row := s.pool.QueryRow(ctx, `
INSERT INTO llm_connection_models (id, connection_id, model_id, source, enabled, raw, last_seen_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, now())
ON CONFLICT (connection_id, model_id)
DO UPDATE SET
  source = EXCLUDED.source,
  enabled = EXCLUDED.enabled,
  raw = EXCLUDED.raw,
  last_seen_at = EXCLUDED.last_seen_at,
  updated_at = now()
RETURNING id, connection_id, model_id, source, enabled, raw, last_seen_at, created_at, updated_at
`, model.ID, connection.ID, model.ModelID, model.Source, model.Enabled, rawPayload, model.LastSeenAt)
	return scanLLMConnectionModel(row)
}

type llmModelScanner interface {
	Scan(dest ...any) error
}

func scanLLMConnectionModel(row llmModelScanner) (LLMConnectionModel, error) {
	var model LLMConnectionModel
	var raw []byte
	var lastSeenAt sql.NullTime
	if err := row.Scan(
		&model.ID,
		&model.ConnectionID,
		&model.ModelID,
		&model.Source,
		&model.Enabled,
		&raw,
		&lastSeenAt,
		&model.CreatedAt,
		&model.UpdatedAt,
	); err != nil {
		return LLMConnectionModel{}, err
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &model.Raw)
	}
	if model.Raw == nil {
		model.Raw = map[string]any{}
	}
	if lastSeenAt.Valid {
		model.LastSeenAt = &lastSeenAt.Time
	}
	return model, nil
}

func (s *Store) listSkillCandidates(ctx context.Context, workspaceID string) ([]SkillDefinitionWithFiles, error) {
	rows, err := s.pool.Query(ctx, `
SELECT
  d.id, d.slug, d.source, d.scope_type, d.scope_id, d.name, d.description,
  d.version, d.content_hash, d.created_at, d.updated_at,
  f.id, f.skill_id, f.path, f.content, f.content_hash, f.created_at, f.updated_at
FROM skill_definitions d
LEFT JOIN skill_files f ON f.skill_id = d.id
WHERE
  (d.source = $2 AND d.scope_type = 'global')
  OR (d.source = $3 AND d.scope_type = 'workspace' AND d.scope_id = $1)
  OR (d.source = $4 AND (d.scope_type = 'global' OR (d.scope_type = 'workspace' AND d.scope_id = $1)))
  OR (d.source = $5 AND d.scope_type = 'global')
ORDER BY d.slug ASC, d.source ASC, d.updated_at DESC, f.path ASC
`, workspaceID, protocol.SkillSourcePlatformBuiltin, protocol.SkillSourceWorkspace, protocol.SkillSourcePlugin, protocol.SkillSourceUser)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	definitions := []SkillDefinitionWithFiles{}
	byID := map[string]int{}
	for rows.Next() {
		var def SkillDefinition
		var file SkillFile
		var fileID sql.NullString
		var fileSkillID sql.NullString
		var filePath sql.NullString
		var fileContent sql.NullString
		var fileHash sql.NullString
		var fileCreatedAt sql.NullTime
		var fileUpdatedAt sql.NullTime
		if err := rows.Scan(
			&def.ID,
			&def.Slug,
			&def.Source,
			&def.ScopeType,
			&def.ScopeID,
			&def.Name,
			&def.Description,
			&def.Version,
			&def.ContentHash,
			&def.CreatedAt,
			&def.UpdatedAt,
			&fileID,
			&fileSkillID,
			&filePath,
			&fileContent,
			&fileHash,
			&fileCreatedAt,
			&fileUpdatedAt,
		); err != nil {
			return nil, err
		}
		idx, ok := byID[def.ID]
		if !ok {
			definitions = append(definitions, SkillDefinitionWithFiles{Definition: def})
			idx = len(definitions) - 1
			byID[def.ID] = idx
		}
		if fileID.Valid {
			file.ID = fileID.String
			file.SkillID = fileSkillID.String
			file.Path = filePath.String
			file.Content = fileContent.String
			file.ContentHash = fileHash.String
			if fileCreatedAt.Valid {
				file.CreatedAt = fileCreatedAt.Time
			}
			if fileUpdatedAt.Valid {
				file.UpdatedAt = fileUpdatedAt.Time
			}
			definitions[idx].Files = append(definitions[idx].Files, file)
		}
	}
	return definitions, rows.Err()
}

func (s *Store) listMCPCandidates(ctx context.Context, workspaceID string) ([]MCPServerDefinitionWithEnv, error) {
	rows, err := s.pool.Query(ctx, `
SELECT
  d.id, d.name, d.source, d.scope_type, d.scope_id, d.command, d.args,
  d.transport, d.version, d.content_hash, d.created_at, d.updated_at,
  e.id, e.server_id, e.name, e.value, e.created_at, e.updated_at
FROM mcp_server_definitions d
LEFT JOIN mcp_server_env e ON e.server_id = d.id
WHERE
  (d.source = $2 AND d.scope_type = 'global')
  OR (d.source = $3 AND d.scope_type = 'workspace' AND d.scope_id = $1)
  OR (d.source = $4 AND (d.scope_type = 'global' OR (d.scope_type = 'workspace' AND d.scope_id = $1)))
  OR (d.source = $5 AND d.scope_type = 'global')
ORDER BY d.name ASC, d.source ASC, d.updated_at DESC, e.name ASC
`, workspaceID, protocol.SkillSourcePlatformBuiltin, protocol.SkillSourceWorkspace, protocol.SkillSourcePlugin, protocol.SkillSourceUser)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	definitions := []MCPServerDefinitionWithEnv{}
	byID := map[string]int{}
	for rows.Next() {
		var def MCPServerDefinition
		var argsPayload []byte
		var env MCPServerEnv
		var envID sql.NullString
		var envServerID sql.NullString
		var envName sql.NullString
		var envValue sql.NullString
		var envCreatedAt sql.NullTime
		var envUpdatedAt sql.NullTime
		if err := rows.Scan(
			&def.ID,
			&def.Name,
			&def.Source,
			&def.ScopeType,
			&def.ScopeID,
			&def.Command,
			&argsPayload,
			&def.Transport,
			&def.Version,
			&def.ContentHash,
			&def.CreatedAt,
			&def.UpdatedAt,
			&envID,
			&envServerID,
			&envName,
			&envValue,
			&envCreatedAt,
			&envUpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(argsPayload) > 0 {
			if err := json.Unmarshal(argsPayload, &def.Args); err != nil {
				return nil, err
			}
		}
		idx, ok := byID[def.ID]
		if !ok {
			definitions = append(definitions, MCPServerDefinitionWithEnv{Definition: def})
			idx = len(definitions) - 1
			byID[def.ID] = idx
		}
		if envID.Valid {
			env.ID = envID.String
			env.ServerID = envServerID.String
			env.Name = envName.String
			env.Value = envValue.String
			if envCreatedAt.Valid {
				env.CreatedAt = envCreatedAt.Time
			}
			if envUpdatedAt.Valid {
				env.UpdatedAt = envUpdatedAt.Time
			}
			definitions[idx].Env = append(definitions[idx].Env, env)
		}
	}
	return definitions, rows.Err()
}

func (s *Store) listWorkspaceSkills(ctx context.Context, workspaceID string) ([]SkillDefinitionWithFiles, error) {
	return s.queryWorkspaceSkills(ctx, workspaceID, "")
}

func (s *Store) getWorkspaceSkill(ctx context.Context, workspaceID, slug string) (SkillDefinitionWithFiles, bool, error) {
	skills, err := s.queryWorkspaceSkills(ctx, workspaceID, slug)
	if err != nil || len(skills) == 0 {
		return SkillDefinitionWithFiles{}, false, err
	}
	return skills[0], true, nil
}

func (s *Store) upsertWorkspaceSkill(ctx context.Context, workspaceID string, skill SkillDefinitionWithFiles) (SkillDefinitionWithFiles, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SkillDefinitionWithFiles{}, err
	}
	defer tx.Rollback(ctx)

	def := skill.Definition
	err = tx.QueryRow(ctx, `
INSERT INTO skill_definitions (id, slug, source, scope_type, scope_id, name, description, version, content_hash, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, now())
ON CONFLICT (source, scope_type, scope_id, slug)
DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  content_hash = EXCLUDED.content_hash,
  version = skill_definitions.version + 1,
  updated_at = now()
RETURNING id, slug, source, scope_type, scope_id, name, description, version, content_hash, created_at, updated_at
`, def.ID, def.Slug, protocol.SkillSourceWorkspace, "workspace", workspaceID, def.Name, def.Description, def.ContentHash).Scan(
		&def.ID, &def.Slug, &def.Source, &def.ScopeType, &def.ScopeID, &def.Name, &def.Description, &def.Version, &def.ContentHash, &def.CreatedAt, &def.UpdatedAt,
	)
	if err != nil {
		return SkillDefinitionWithFiles{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM skill_files WHERE skill_id = $1`, def.ID); err != nil {
		return SkillDefinitionWithFiles{}, err
	}
	files := make([]SkillFile, 0, len(skill.Files))
	for _, file := range skill.Files {
		var saved SkillFile
		err := tx.QueryRow(ctx, `
INSERT INTO skill_files (id, skill_id, path, content, content_hash, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
RETURNING id, skill_id, path, content, content_hash, created_at, updated_at
`, file.ID, def.ID, file.Path, file.Content, file.ContentHash).Scan(
			&saved.ID, &saved.SkillID, &saved.Path, &saved.Content, &saved.ContentHash, &saved.CreatedAt, &saved.UpdatedAt,
		)
		if err != nil {
			return SkillDefinitionWithFiles{}, err
		}
		files = append(files, saved)
	}
	if err := tx.Commit(ctx); err != nil {
		return SkillDefinitionWithFiles{}, err
	}
	return SkillDefinitionWithFiles{Definition: def, Files: files}, nil
}

func (s *Store) deleteWorkspaceSkill(ctx context.Context, workspaceID, slug string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var skillID string
	err = tx.QueryRow(ctx, `
DELETE FROM skill_definitions
WHERE source = $1 AND scope_type = 'workspace' AND scope_id = $2 AND slug = $3
RETURNING id
`, protocol.SkillSourceWorkspace, workspaceID, slug).Scan(&skillID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM skill_files WHERE skill_id = $1`, skillID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) queryWorkspaceSkills(ctx context.Context, workspaceID, slug string) ([]SkillDefinitionWithFiles, error) {
	rows, err := s.pool.Query(ctx, `
SELECT
  d.id, d.slug, d.source, d.scope_type, d.scope_id, d.name, d.description,
  d.version, d.content_hash, d.created_at, d.updated_at,
  f.id, f.skill_id, f.path, f.content, f.content_hash, f.created_at, f.updated_at
FROM skill_definitions d
LEFT JOIN skill_files f ON f.skill_id = d.id
WHERE d.source = $1 AND d.scope_type = 'workspace' AND d.scope_id = $2 AND ($3 = '' OR d.slug = $3)
ORDER BY d.slug ASC, f.path ASC
`, protocol.SkillSourceWorkspace, workspaceID, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSkillDefinitionsWithFiles(rows)
}

func scanSkillDefinitionsWithFiles(rows pgx.Rows) ([]SkillDefinitionWithFiles, error) {
	definitions := []SkillDefinitionWithFiles{}
	byID := map[string]int{}
	for rows.Next() {
		var def SkillDefinition
		var file SkillFile
		var fileID sql.NullString
		var fileSkillID sql.NullString
		var filePath sql.NullString
		var fileContent sql.NullString
		var fileHash sql.NullString
		var fileCreatedAt sql.NullTime
		var fileUpdatedAt sql.NullTime
		if err := rows.Scan(
			&def.ID,
			&def.Slug,
			&def.Source,
			&def.ScopeType,
			&def.ScopeID,
			&def.Name,
			&def.Description,
			&def.Version,
			&def.ContentHash,
			&def.CreatedAt,
			&def.UpdatedAt,
			&fileID,
			&fileSkillID,
			&filePath,
			&fileContent,
			&fileHash,
			&fileCreatedAt,
			&fileUpdatedAt,
		); err != nil {
			return nil, err
		}
		idx, ok := byID[def.ID]
		if !ok {
			definitions = append(definitions, SkillDefinitionWithFiles{Definition: def})
			idx = len(definitions) - 1
			byID[def.ID] = idx
		}
		if fileID.Valid {
			file.ID = fileID.String
			file.SkillID = fileSkillID.String
			file.Path = filePath.String
			file.Content = fileContent.String
			file.ContentHash = fileHash.String
			if fileCreatedAt.Valid {
				file.CreatedAt = fileCreatedAt.Time
			}
			if fileUpdatedAt.Valid {
				file.UpdatedAt = fileUpdatedAt.Time
			}
			definitions[idx].Files = append(definitions[idx].Files, file)
		}
	}
	return definitions, rows.Err()
}

func (s *Store) listWorkspaceMCPServers(ctx context.Context, workspaceID string) ([]MCPServerDefinitionWithEnv, error) {
	return s.queryWorkspaceMCPServers(ctx, workspaceID, "")
}

func (s *Store) getWorkspaceMCPServer(ctx context.Context, workspaceID, name string) (MCPServerDefinitionWithEnv, bool, error) {
	servers, err := s.queryWorkspaceMCPServers(ctx, workspaceID, name)
	if err != nil || len(servers) == 0 {
		return MCPServerDefinitionWithEnv{}, false, err
	}
	return servers[0], true, nil
}

func (s *Store) upsertWorkspaceMCPServer(ctx context.Context, workspaceID string, server MCPServerDefinitionWithEnv) (MCPServerDefinitionWithEnv, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MCPServerDefinitionWithEnv{}, err
	}
	defer tx.Rollback(ctx)

	def := server.Definition
	argsPayload, err := json.Marshal(def.Args)
	if err != nil {
		return MCPServerDefinitionWithEnv{}, err
	}
	err = tx.QueryRow(ctx, `
INSERT INTO mcp_server_definitions (id, name, source, scope_type, scope_id, command, args, transport, version, content_hash, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, 1, $9, now())
ON CONFLICT (source, scope_type, scope_id, name)
DO UPDATE SET
  command = EXCLUDED.command,
  args = EXCLUDED.args,
  transport = EXCLUDED.transport,
  content_hash = EXCLUDED.content_hash,
  version = mcp_server_definitions.version + 1,
  updated_at = now()
RETURNING id, name, source, scope_type, scope_id, command, args, transport, version, content_hash, created_at, updated_at
`, def.ID, def.Name, protocol.SkillSourceWorkspace, "workspace", workspaceID, def.Command, argsPayload, def.Transport, def.ContentHash).Scan(
		&def.ID, &def.Name, &def.Source, &def.ScopeType, &def.ScopeID, &def.Command, &argsPayload, &def.Transport, &def.Version, &def.ContentHash, &def.CreatedAt, &def.UpdatedAt,
	)
	if err != nil {
		return MCPServerDefinitionWithEnv{}, err
	}
	if len(argsPayload) > 0 {
		if err := json.Unmarshal(argsPayload, &def.Args); err != nil {
			return MCPServerDefinitionWithEnv{}, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM mcp_server_env WHERE server_id = $1`, def.ID); err != nil {
		return MCPServerDefinitionWithEnv{}, err
	}
	env := make([]MCPServerEnv, 0, len(server.Env))
	for _, item := range server.Env {
		var saved MCPServerEnv
		err := tx.QueryRow(ctx, `
INSERT INTO mcp_server_env (id, server_id, name, value, updated_at)
VALUES ($1, $2, $3, $4, now())
RETURNING id, server_id, name, value, created_at, updated_at
`, item.ID, def.ID, item.Name, item.Value).Scan(
			&saved.ID, &saved.ServerID, &saved.Name, &saved.Value, &saved.CreatedAt, &saved.UpdatedAt,
		)
		if err != nil {
			return MCPServerDefinitionWithEnv{}, err
		}
		env = append(env, saved)
	}
	if err := tx.Commit(ctx); err != nil {
		return MCPServerDefinitionWithEnv{}, err
	}
	return MCPServerDefinitionWithEnv{Definition: def, Env: env}, nil
}

func (s *Store) deleteWorkspaceMCPServer(ctx context.Context, workspaceID, name string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var serverID string
	err = tx.QueryRow(ctx, `
DELETE FROM mcp_server_definitions
WHERE source = $1 AND scope_type = 'workspace' AND scope_id = $2 AND name = $3
RETURNING id
`, protocol.SkillSourceWorkspace, workspaceID, name).Scan(&serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM mcp_server_env WHERE server_id = $1`, serverID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) queryWorkspaceMCPServers(ctx context.Context, workspaceID, name string) ([]MCPServerDefinitionWithEnv, error) {
	rows, err := s.pool.Query(ctx, `
SELECT
  d.id, d.name, d.source, d.scope_type, d.scope_id, d.command, d.args,
  d.transport, d.version, d.content_hash, d.created_at, d.updated_at,
  e.id, e.server_id, e.name, e.value, e.created_at, e.updated_at
FROM mcp_server_definitions d
LEFT JOIN mcp_server_env e ON e.server_id = d.id
WHERE d.source = $1 AND d.scope_type = 'workspace' AND d.scope_id = $2 AND ($3 = '' OR d.name = $3)
ORDER BY d.name ASC, e.name ASC
`, protocol.SkillSourceWorkspace, workspaceID, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMCPServerDefinitionsWithEnv(rows)
}

func scanMCPServerDefinitionsWithEnv(rows pgx.Rows) ([]MCPServerDefinitionWithEnv, error) {
	definitions := []MCPServerDefinitionWithEnv{}
	byID := map[string]int{}
	for rows.Next() {
		var def MCPServerDefinition
		var argsPayload []byte
		var env MCPServerEnv
		var envID sql.NullString
		var envServerID sql.NullString
		var envName sql.NullString
		var envValue sql.NullString
		var envCreatedAt sql.NullTime
		var envUpdatedAt sql.NullTime
		if err := rows.Scan(
			&def.ID,
			&def.Name,
			&def.Source,
			&def.ScopeType,
			&def.ScopeID,
			&def.Command,
			&argsPayload,
			&def.Transport,
			&def.Version,
			&def.ContentHash,
			&def.CreatedAt,
			&def.UpdatedAt,
			&envID,
			&envServerID,
			&envName,
			&envValue,
			&envCreatedAt,
			&envUpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(argsPayload) > 0 {
			if err := json.Unmarshal(argsPayload, &def.Args); err != nil {
				return nil, err
			}
		}
		idx, ok := byID[def.ID]
		if !ok {
			definitions = append(definitions, MCPServerDefinitionWithEnv{Definition: def})
			idx = len(definitions) - 1
			byID[def.ID] = idx
		}
		if envID.Valid {
			env.ID = envID.String
			env.ServerID = envServerID.String
			env.Name = envName.String
			env.Value = envValue.String
			if envCreatedAt.Valid {
				env.CreatedAt = envCreatedAt.Time
			}
			if envUpdatedAt.Valid {
				env.UpdatedAt = envUpdatedAt.Time
			}
			definitions[idx].Env = append(definitions[idx].Env, env)
		}
	}
	return definitions, rows.Err()
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
		return RunInterruptResult{Interrupted: true, Reason: "already_cancelling", ExpectedRunID: expectedRunID, Run: &active}, nil
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
	messageID := projectionMessageID(event)
	switch event.Type {
	case protocol.EventMessageStarted:
		if messageID == "" {
			return nil
		}
		return upsertMessage(ctx, exec, event, messageID, MessageStatusStreaming, nil)
	case protocol.EventMessageCompleted:
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

func projectionMessageID(event protocol.UniversalEvent) string {
	return event.MessageID
}

func eventRole(event protocol.UniversalEvent) string {
	if event.Role != "" {
		return event.Role
	}
	return "assistant"
}

func eventBlocks(event protocol.UniversalEvent) []protocol.UniversalBlock {
	return event.Content
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
	return status == RunStatusCompleted || status == RunStatusFailed || status == RunStatusCancelled || status == RunStatusTimedOut
}
