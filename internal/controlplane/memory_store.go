package controlplane

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"agenthub/pkg/protocol"
)

type MemoryStore struct {
	mu             sync.RWMutex
	nextEventID    int64
	workspaces     map[string]WorkspaceProjection
	workspaceToken map[string]string
	events         map[string][]StoredEvent
	messages       map[string]map[string]MessageProjection
	order          map[string][]string
	tasks          map[string]TaskProjection
	sessions       map[string]SessionProjection
	activeRuns     map[string]string
	runs           map[string]SessionRun
	skills         map[string]SkillDefinitionWithFiles
	mcpServers     map[string]MCPServerDefinitionWithEnv
	llmConnections map[string]LLMConnection
	llmModels      map[string]LLMConnectionModel
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		workspaces:     map[string]WorkspaceProjection{},
		workspaceToken: map[string]string{},
		events:         map[string][]StoredEvent{},
		messages:       map[string]map[string]MessageProjection{},
		order:          map[string][]string{},
		tasks:          map[string]TaskProjection{},
		sessions:       map[string]SessionProjection{},
		activeRuns:     map[string]string{},
		runs:           map[string]SessionRun{},
		skills:         map[string]SkillDefinitionWithFiles{},
		mcpServers:     map[string]MCPServerDefinitionWithEnv{},
		llmConnections: map[string]LLMConnection{},
		llmModels:      map[string]LLMConnectionModel{},
	}
}

func (s *MemoryStore) Ping(ctx context.Context) error { return nil }

func (s *MemoryStore) ListWorkspaces(ctx context.Context, ownerUserID string, limit, offset int) ([]WorkspaceProjection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]WorkspaceProjection, 0, len(s.workspaces))
	for _, workspace := range s.workspaces {
		if workspace.OwnerUserID == ownerUserID {
			all = append(all, cloneWorkspaceProjection(workspace))
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	if offset >= len(all) {
		return []WorkspaceProjection{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (s *MemoryStore) CreateWorkspace(ctx context.Context, ownerUserID, name string) (WorkspaceProjection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for range 3 {
		workspaceID, err := newWorkspaceUUID()
		if err != nil {
			return WorkspaceProjection{}, err
		}
		if _, exists := s.workspaces[workspaceID]; exists {
			continue
		}
		workspace := WorkspaceProjection{ID: workspaceID, Name: name, OwnerUserID: ownerUserID}
		s.workspaces[workspaceID] = workspace
		return cloneWorkspaceProjection(workspace), nil
	}
	return WorkspaceProjection{}, ErrWorkspaceExists
}

func (s *MemoryStore) GetWorkspace(ctx context.Context, ownerUserID, workspaceID string) (WorkspaceProjection, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	workspace, ok := s.workspaces[workspaceID]
	if !ok || workspace.OwnerUserID != ownerUserID {
		return WorkspaceProjection{}, false, nil
	}
	return cloneWorkspaceProjection(workspace), true, nil
}

func (s *MemoryStore) UpdateWorkspace(ctx context.Context, ownerUserID, workspaceID, name string) (WorkspaceProjection, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.workspaces[workspaceID]
	if !ok || existing.OwnerUserID != ownerUserID {
		return WorkspaceProjection{}, false, nil
	}
	existing.Name = name
	s.workspaces[workspaceID] = existing
	return cloneWorkspaceProjection(existing), true, nil
}

func (s *MemoryStore) DeleteWorkspace(ctx context.Context, ownerUserID, workspaceID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspace, ok := s.workspaces[workspaceID]
	if !ok || workspace.OwnerUserID != ownerUserID {
		return false, nil
	}

	deletedSessions := map[string]bool{}
	for sessionID, session := range s.sessions {
		if session.WorkspaceID == workspaceID {
			deletedSessions[sessionID] = true
			delete(s.sessions, sessionID)
		}
	}
	for taskID, task := range s.tasks {
		if task.WorkspaceID == workspaceID {
			delete(s.tasks, taskID)
		}
	}
	for sessionID := range deletedSessions {
		delete(s.activeRuns, sessionID)
		delete(s.events, sessionID)
		delete(s.messages, sessionID)
		delete(s.order, sessionID)
	}
	for runID, run := range s.runs {
		if run.WorkspaceID == workspaceID || deletedSessions[run.SessionID] {
			delete(s.runs, runID)
		}
	}
	if connection, ok := s.llmConnections[workspaceID]; ok {
		for key, model := range s.llmModels {
			if model.ConnectionID == connection.ID {
				delete(s.llmModels, key)
			}
		}
		delete(s.llmConnections, workspaceID)
	}
	for key, skill := range s.skills {
		if skill.Definition.Source == protocol.SkillSourceWorkspace && skill.Definition.ScopeType == "workspace" && skill.Definition.ScopeID == workspaceID {
			delete(s.skills, key)
		}
	}
	for key, server := range s.mcpServers {
		if server.Definition.Source == protocol.SkillSourceWorkspace && server.Definition.ScopeType == "workspace" && server.Definition.ScopeID == workspaceID {
			delete(s.mcpServers, key)
		}
	}
	delete(s.workspaceToken, workspaceID)
	delete(s.workspaces, workspaceID)
	return true, nil
}

func (s *MemoryStore) SaveWorkspaceToken(ctx context.Context, workspaceID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaceToken[workspaceID] = token
	return nil
}

func (s *MemoryStore) WorkspaceToken(ctx context.Context, workspaceID string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.workspaceToken[workspaceID]
	return token, ok, nil
}

func (s *MemoryStore) CreateSession(ctx context.Context, workspaceID, taskID, sessionID string, req protocol.CreateSessionRequest) (TaskProjection, SessionProjection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	task := s.tasks[taskID]
	if task.TaskID == "" {
		task = TaskProjection{TaskID: taskID, WorkspaceID: workspaceID, CreatedAt: now}
	}
	task.WorkspaceID = workspaceID
	task.UpdatedAt = now
	s.tasks[taskID] = task
	session := s.sessions[sessionID]
	if session.SessionID == "" {
		session = SessionProjection{SessionID: sessionID, TaskID: taskID, WorkspaceID: workspaceID, CreatedAt: now}
	}
	session.TaskID = taskID
	session.WorkspaceID = workspaceID
	session.ModelID = req.ModelID
	session.Title = req.Title
	session.Metadata = cloneMetadata(req.Metadata)
	session.UpdatedAt = now
	s.sessions[sessionID] = session
	return task, session, nil
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

func (s *MemoryStore) ListWorkspaceSessions(ctx context.Context, workspaceID string, limit, offset int) ([]SessionProjection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []SessionProjection
	for _, session := range s.sessions {
		if session.WorkspaceID == workspaceID {
			all = append(all, session)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	if offset >= len(all) {
		return []SessionProjection{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (s *MemoryStore) DeleteSession(ctx context.Context, ownerUserID, sessionID string) (DeleteSessionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return DeleteSessionResult{Deleted: false}, nil
	}
	workspace, ok := s.workspaces[session.WorkspaceID]
	if !ok || workspace.OwnerUserID != ownerUserID {
		return DeleteSessionResult{Deleted: false}, nil
	}
	if activeRunID := s.activeRuns[sessionID]; activeRunID != "" {
		active := s.runs[activeRunID]
		if active.RunID != "" && !isTerminalRunStatus(active.Status) {
			return DeleteSessionResult{Deleted: false, ActiveRun: &active}, nil
		}
	}

	delete(s.sessions, sessionID)
	delete(s.activeRuns, sessionID)
	delete(s.events, sessionID)
	delete(s.messages, sessionID)
	delete(s.order, sessionID)
	for runID, run := range s.runs {
		if run.SessionID == sessionID {
			delete(s.runs, runID)
		}
	}
	hasRemainingSessionForTask := false
	for _, other := range s.sessions {
		if other.TaskID == session.TaskID {
			hasRemainingSessionForTask = true
			break
		}
	}
	if !hasRemainingSessionForTask {
		delete(s.tasks, session.TaskID)
	}
	return DeleteSessionResult{Deleted: true}, nil
}

func (s *MemoryStore) GetWorkspaceLLMConnection(ctx context.Context, workspaceID string) (LLMConnection, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connection, ok := s.llmConnections[workspaceID]
	return connection, ok, nil
}

func (s *MemoryStore) UpsertWorkspaceLLMConnection(ctx context.Context, workspaceID string, connection LLMConnection) (LLMConnection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := s.llmConnections[workspaceID]; ok {
		connection.ID = existing.ID
		connection.CreatedAt = existing.CreatedAt
	} else {
		connection.CreatedAt = now
	}
	connection.UserID = ""
	connection.WorkspaceID = workspaceID
	connection.UpdatedAt = now
	s.llmConnections[workspaceID] = connection
	return connection, nil
}

func (s *MemoryStore) ListWorkspaceLLMModels(ctx context.Context, workspaceID string) ([]LLMConnectionModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connection, ok := s.llmConnections[workspaceID]
	if !ok {
		return nil, nil
	}
	models := []LLMConnectionModel{}
	for _, model := range s.llmModels {
		if model.ConnectionID == connection.ID {
			models = append(models, cloneLLMConnectionModel(model))
		}
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Enabled != models[j].Enabled {
			return models[i].Enabled
		}
		return models[i].ModelID < models[j].ModelID
	})
	return models, nil
}

func (s *MemoryStore) UpsertWorkspaceLLMModel(ctx context.Context, workspaceID string, model LLMConnectionModel) (LLMConnectionModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	connection, ok := s.llmConnections[workspaceID]
	if !ok {
		return LLMConnectionModel{}, errors.New("llm connection is not configured")
	}
	now := time.Now().UTC()
	key := connection.ID + "\x00" + model.ModelID
	if existing, ok := s.llmModels[key]; ok {
		model.ID = existing.ID
		model.CreatedAt = existing.CreatedAt
	} else {
		model.CreatedAt = now
	}
	model.ConnectionID = connection.ID
	model.UpdatedAt = now
	s.llmModels[key] = cloneLLMConnectionModel(model)
	return cloneLLMConnectionModel(model), nil
}

func (s *MemoryStore) ListSkillCandidates(ctx context.Context, workspaceID string) ([]SkillDefinitionWithFiles, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SkillDefinitionWithFiles, 0, len(s.skills))
	for _, skill := range s.skills {
		def := skill.Definition
		if !isDefinitionVisible(def.Source, def.ScopeType, def.ScopeID, workspaceID) {
			continue
		}
		files := make([]SkillFile, len(skill.Files))
		copy(files, skill.Files)
		out = append(out, SkillDefinitionWithFiles{Definition: def, Files: files})
	}
	return out, nil
}

func (s *MemoryStore) ListMCPCandidates(ctx context.Context, workspaceID string) ([]MCPServerDefinitionWithEnv, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MCPServerDefinitionWithEnv, 0, len(s.mcpServers))
	for _, server := range s.mcpServers {
		def := server.Definition
		if !isDefinitionVisible(def.Source, def.ScopeType, def.ScopeID, workspaceID) {
			continue
		}
		env := make([]MCPServerEnv, len(server.Env))
		copy(env, server.Env)
		out = append(out, MCPServerDefinitionWithEnv{Definition: def, Env: env})
	}
	return out, nil
}

func (s *MemoryStore) ListWorkspaceSkills(ctx context.Context, workspaceID string) ([]SkillDefinitionWithFiles, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []SkillDefinitionWithFiles{}
	for _, skill := range s.skills {
		if skill.Definition.Source == protocol.SkillSourceWorkspace && skill.Definition.ScopeType == "workspace" && skill.Definition.ScopeID == workspaceID {
			out = append(out, cloneSkillWithFiles(skill))
		}
	}
	return out, nil
}

func (s *MemoryStore) GetWorkspaceSkill(ctx context.Context, workspaceID, slug string) (SkillDefinitionWithFiles, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, skill := range s.skills {
		if skill.Definition.Source == protocol.SkillSourceWorkspace && skill.Definition.ScopeType == "workspace" && skill.Definition.ScopeID == workspaceID && skill.Definition.Slug == slug {
			return cloneSkillWithFiles(skill), true, nil
		}
	}
	return SkillDefinitionWithFiles{}, false, nil
}

func (s *MemoryStore) UpsertWorkspaceSkill(ctx context.Context, workspaceID string, skill SkillDefinitionWithFiles) (SkillDefinitionWithFiles, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	key := skill.Definition.ID
	if existing, ok := s.skills[key]; ok {
		skill.Definition.Version = existing.Definition.Version + 1
		skill.Definition.CreatedAt = existing.Definition.CreatedAt
	} else {
		skill.Definition.Version = 1
		skill.Definition.CreatedAt = now
	}
	skill.Definition.Source = protocol.SkillSourceWorkspace
	skill.Definition.ScopeType = "workspace"
	skill.Definition.ScopeID = workspaceID
	skill.Definition.UpdatedAt = now
	for idx := range skill.Files {
		skill.Files[idx].SkillID = skill.Definition.ID
		skill.Files[idx].CreatedAt = now
		skill.Files[idx].UpdatedAt = now
	}
	s.skills[key] = cloneSkillWithFiles(skill)
	return cloneSkillWithFiles(skill), nil
}

func (s *MemoryStore) DeleteWorkspaceSkill(ctx context.Context, workspaceID, slug string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, skill := range s.skills {
		if skill.Definition.Source == protocol.SkillSourceWorkspace && skill.Definition.ScopeType == "workspace" && skill.Definition.ScopeID == workspaceID && skill.Definition.Slug == slug {
			delete(s.skills, key)
			return true, nil
		}
	}
	return false, nil
}

func (s *MemoryStore) ListWorkspaceMCPServers(ctx context.Context, workspaceID string) ([]MCPServerDefinitionWithEnv, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []MCPServerDefinitionWithEnv{}
	for _, server := range s.mcpServers {
		if server.Definition.Source == protocol.SkillSourceWorkspace && server.Definition.ScopeType == "workspace" && server.Definition.ScopeID == workspaceID {
			out = append(out, cloneMCPServerWithEnv(server))
		}
	}
	return out, nil
}

func (s *MemoryStore) GetWorkspaceMCPServer(ctx context.Context, workspaceID, name string) (MCPServerDefinitionWithEnv, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, server := range s.mcpServers {
		if server.Definition.Source == protocol.SkillSourceWorkspace && server.Definition.ScopeType == "workspace" && server.Definition.ScopeID == workspaceID && server.Definition.Name == name {
			return cloneMCPServerWithEnv(server), true, nil
		}
	}
	return MCPServerDefinitionWithEnv{}, false, nil
}

func (s *MemoryStore) UpsertWorkspaceMCPServer(ctx context.Context, workspaceID string, server MCPServerDefinitionWithEnv) (MCPServerDefinitionWithEnv, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	key := server.Definition.ID
	if existing, ok := s.mcpServers[key]; ok {
		server.Definition.Version = existing.Definition.Version + 1
		server.Definition.CreatedAt = existing.Definition.CreatedAt
	} else {
		server.Definition.Version = 1
		server.Definition.CreatedAt = now
	}
	server.Definition.Source = protocol.SkillSourceWorkspace
	server.Definition.ScopeType = "workspace"
	server.Definition.ScopeID = workspaceID
	server.Definition.UpdatedAt = now
	for idx := range server.Env {
		server.Env[idx].ServerID = server.Definition.ID
		server.Env[idx].CreatedAt = now
		server.Env[idx].UpdatedAt = now
	}
	s.mcpServers[key] = cloneMCPServerWithEnv(server)
	return cloneMCPServerWithEnv(server), nil
}

func (s *MemoryStore) DeleteWorkspaceMCPServer(ctx context.Context, workspaceID, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, server := range s.mcpServers {
		if server.Definition.Source == protocol.SkillSourceWorkspace && server.Definition.ScopeType == "workspace" && server.Definition.ScopeID == workspaceID && server.Definition.Name == name {
			delete(s.mcpServers, key)
			return true, nil
		}
	}
	return false, nil
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
	if turn.TaskID == "" {
		turn.TaskID = s.sessions[turn.SessionID].TaskID
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
		TaskID:      turn.TaskID,
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
		return RunInterruptResult{Interrupted: true, Reason: "already_cancelling", ExpectedRunID: expectedRunID, Run: &active}, nil
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
	state := SessionState{SessionID: sessionID, ModelID: s.sessions[sessionID].ModelID}
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
	messageID := projectionMessageID(event)
	switch event.Type {
	case protocol.EventMessageStarted:
		if messageID == "" {
			return
		}
		s.upsertMessage(event, messageID, MessageStatusStreaming, nil)
	case protocol.EventMessageCompleted:
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

func cloneWorkspaceProjection(workspace WorkspaceProjection) WorkspaceProjection {
	return workspace
}

func cloneSkillWithFiles(skill SkillDefinitionWithFiles) SkillDefinitionWithFiles {
	files := make([]SkillFile, len(skill.Files))
	copy(files, skill.Files)
	return SkillDefinitionWithFiles{Definition: skill.Definition, Files: files}
}

func cloneMCPServerWithEnv(server MCPServerDefinitionWithEnv) MCPServerDefinitionWithEnv {
	env := make([]MCPServerEnv, len(server.Env))
	copy(env, server.Env)
	def := server.Definition
	def.Args = append([]string(nil), def.Args...)
	return MCPServerDefinitionWithEnv{Definition: def, Env: env}
}

func cloneLLMConnectionModel(model LLMConnectionModel) LLMConnectionModel {
	model.Raw = cloneMetadata(model.Raw)
	if model.LastSeenAt != nil {
		lastSeenAt := *model.LastSeenAt
		model.LastSeenAt = &lastSeenAt
	}
	return model
}
