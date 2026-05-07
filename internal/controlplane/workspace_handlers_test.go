package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"agenthub/internal/driver"
	"agenthub/pkg/protocol"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type workspaceListResponse struct {
	Workspaces []WorkspaceProjection `json:"workspaces"`
	Limit      int                   `json:"limit"`
	Offset     int                   `json:"offset"`
}

type workspaceEnvelope struct {
	Workspace WorkspaceProjection `json:"workspace"`
}

type deleteWorkspaceEnvelope struct {
	Deleted              bool                 `json:"deleted"`
	ReplacementWorkspace *WorkspaceProjection `json:"replacementWorkspace,omitempty"`
}

func TestListWorkspacesCreatesDefaultOnce(t *testing.T) {
	_, baseURL, client := newAuthenticatedWorkspaceTestServer(t)

	first := getWorkspaceList(t, client, baseURL)
	if len(first.Workspaces) != 1 {
		t.Fatalf("first list returned %d workspaces, want 1", len(first.Workspaces))
	}
	assertWorkspaceProjection(t, first.Workspaces[0], defaultWorkspaceName, adminUserID)
	status, body := doJSON(t, client, http.MethodGet, baseURL+"/workspaces", nil)
	assertWorkspaceResponseShape(t, status, body, "workspaces")

	second := getWorkspaceList(t, client, baseURL)
	if len(second.Workspaces) != 1 {
		t.Fatalf("second list returned %d workspaces, want 1", len(second.Workspaces))
	}
	if second.Workspaces[0].ID != first.Workspaces[0].ID {
		t.Fatalf("repeated list created a new default workspace: first=%s second=%s", first.Workspaces[0].ID, second.Workspaces[0].ID)
	}
}

func TestWorkspaceCRUDAndLastDeleteReplacement(t *testing.T) {
	_, baseURL, client := newAuthenticatedWorkspaceTestServer(t)
	initial := getWorkspaceList(t, client, baseURL)
	defaultWorkspace := initial.Workspaces[0]

	status, body := doJSON(t, client, http.MethodPost, baseURL+"/workspaces", map[string]any{"name": "Team Alpha"})
	if status != http.StatusCreated {
		t.Fatalf("create status=%d body=%s, want 201", status, body)
	}
	var created workspaceEnvelope
	decodeJSON(t, body, &created)
	assertWorkspaceProjection(t, created.Workspace, "Team Alpha", adminUserID)
	if created.Workspace.ID == defaultWorkspace.ID {
		t.Fatalf("created workspace reused default id %s", created.Workspace.ID)
	}

	status, body = doJSON(t, client, http.MethodPut, baseURL+"/workspaces/"+created.Workspace.ID, map[string]any{"name": "Renamed Team Alpha"})
	if status != http.StatusOK {
		t.Fatalf("rename status=%d body=%s, want 200", status, body)
	}
	var renamed workspaceEnvelope
	decodeJSON(t, body, &renamed)
	if renamed.Workspace.ID != created.Workspace.ID {
		t.Fatalf("rename changed id: got %s want %s", renamed.Workspace.ID, created.Workspace.ID)
	}
	assertWorkspaceProjection(t, renamed.Workspace, "Renamed Team Alpha", adminUserID)

	status, body = doJSON(t, client, http.MethodDelete, baseURL+"/workspaces/"+created.Workspace.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("delete non-last status=%d body=%s, want 200", status, body)
	}
	var deletedTeam deleteWorkspaceEnvelope
	decodeJSON(t, body, &deletedTeam)
	if !deletedTeam.Deleted || deletedTeam.ReplacementWorkspace != nil {
		t.Fatalf("delete non-last response=%+v, want deleted without replacement", deletedTeam)
	}
	remaining := getWorkspaceList(t, client, baseURL)
	if len(remaining.Workspaces) != 1 || remaining.Workspaces[0].ID != defaultWorkspace.ID {
		t.Fatalf("after deleting non-last, remaining=%+v, want original default", remaining.Workspaces)
	}

	status, body = doJSON(t, client, http.MethodDelete, baseURL+"/workspaces/"+defaultWorkspace.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("delete last status=%d body=%s, want 200", status, body)
	}
	var deletedDefault deleteWorkspaceEnvelope
	decodeJSON(t, body, &deletedDefault)
	if !deletedDefault.Deleted || deletedDefault.ReplacementWorkspace == nil {
		t.Fatalf("delete last response=%+v, want deleted with replacement", deletedDefault)
	}
	assertWorkspaceProjection(t, *deletedDefault.ReplacementWorkspace, defaultWorkspaceName, adminUserID)
	if deletedDefault.ReplacementWorkspace.ID == defaultWorkspace.ID {
		t.Fatalf("replacement reused deleted workspace id %s", defaultWorkspace.ID)
	}
	afterReplacement := getWorkspaceList(t, client, baseURL)
	if len(afterReplacement.Workspaces) != 1 || afterReplacement.Workspaces[0].ID != deletedDefault.ReplacementWorkspace.ID {
		t.Fatalf("after last delete list=%+v, want replacement only", afterReplacement.Workspaces)
	}
	writeJSONEvidence(t, "task-4-delete-last-default.json", map[string]any{
		"deletedWorkspaceId": defaultWorkspace.ID,
		"deleteResponse":     deletedDefault,
		"finalList":          afterReplacement,
	})
}

func TestWorkspaceCreateAndUpdateRejectInvalidBodies(t *testing.T) {
	_, baseURL, client := newAuthenticatedWorkspaceTestServer(t)
	defaultWorkspace := getWorkspaceList(t, client, baseURL).Workspaces[0]

	status, body := doJSON(t, client, http.MethodPost, baseURL+"/workspaces", map[string]any{"name": "   "})
	if status != http.StatusBadRequest {
		t.Fatalf("create whitespace status=%d body=%s, want 400", status, body)
	}
	for _, field := range []string{"id", "workspaceId", "ownerUserId", "owner_user_id", "description", "metadata", "podToken"} {
		payload := map[string]any{"name": "Bad", field: "client-controlled"}
		if field == "metadata" {
			payload[field] = map[string]any{"x": "y"}
		}
		status, body := doJSON(t, client, http.MethodPost, baseURL+"/workspaces", payload)
		if status != http.StatusBadRequest {
			t.Fatalf("create with forbidden field %q status=%d body=%s, want 400", field, status, body)
		}
	}
	assertNoWorkspaceNamed(t, getWorkspaceList(t, client, baseURL), "Bad")

	status, body = doJSON(t, client, http.MethodPut, baseURL+"/workspaces/"+defaultWorkspace.ID, map[string]any{"name": ""})
	if status != http.StatusBadRequest {
		t.Fatalf("update empty status=%d body=%s, want 400", status, body)
	}
	for _, field := range []string{"id", "workspaceId", "ownerUserId", "owner_user_id", "description", "metadata", "podToken"} {
		payload := map[string]any{"name": "Changed", field: "client-controlled"}
		if field == "metadata" {
			payload[field] = map[string]any{"x": "y"}
		}
		status, body := doJSON(t, client, http.MethodPut, baseURL+"/workspaces/"+defaultWorkspace.ID, payload)
		if status != http.StatusBadRequest {
			t.Fatalf("update with forbidden field %q status=%d body=%s, want 400", field, status, body)
		}
	}
	current := getWorkspaceList(t, client, baseURL).Workspaces[0]
	if current.ID != defaultWorkspace.ID || current.Name != defaultWorkspace.Name || current.OwnerUserID != defaultWorkspace.OwnerUserID {
		t.Fatalf("invalid updates mutated workspace: got %+v want %+v", current, defaultWorkspace)
	}
}

func TestWorkspacePathOperationsReturnNotFoundForOtherOwner(t *testing.T) {
	server, baseURL, client := newAuthenticatedWorkspaceTestServer(t)
	other, err := server.store.CreateWorkspace(context.Background(), "other-user", "Other User Workspace")
	if err != nil {
		t.Fatalf("create other user workspace: %v", err)
	}

	for _, req := range []struct {
		method string
		body   any
	}{
		{method: http.MethodGet},
		{method: http.MethodPut, body: map[string]any{"name": "Stolen"}},
		{method: http.MethodDelete},
	} {
		status, body := doJSON(t, client, req.method, baseURL+"/workspaces/"+other.ID, req.body)
		if status != http.StatusNotFound {
			t.Fatalf("%s non-owned workspace status=%d body=%s, want 404", req.method, status, body)
		}
	}
	stored, ok, err := server.store.GetWorkspace(context.Background(), "other-user", other.ID)
	if err != nil || !ok {
		t.Fatalf("other user workspace missing after non-owned attempts: ok=%v err=%v", ok, err)
	}
	if stored.Name != other.Name || stored.OwnerUserID != other.OwnerUserID {
		t.Fatalf("non-owned attempts mutated workspace: got %+v want %+v", stored, other)
	}
}

func TestMemoryStoreDeleteWorkspaceCascadesOwnedResources(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	workspace, err := store.CreateWorkspace(ctx, adminUserID, "Cascade Target")
	if err != nil {
		t.Fatalf("create cascade workspace: %v", err)
	}
	otherWorkspace, err := store.CreateWorkspace(ctx, "other-user", "Other Workspace")
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	target := seedWorkspaceCascadeResources(t, store, workspace.ID, "target")
	other := seedWorkspaceCascadeResources(t, store, otherWorkspace.ID, "other")

	deleted, err := store.DeleteWorkspace(ctx, "other-user", workspace.ID)
	if err != nil || deleted {
		t.Fatalf("non-owned delete deleted=%v err=%v, want false nil", deleted, err)
	}
	assertWorkspaceCascadePresent(t, store, adminUserID, workspace.ID, target)

	deleted, err = store.DeleteWorkspace(ctx, adminUserID, workspace.ID)
	if err != nil || !deleted {
		t.Fatalf("owned delete deleted=%v err=%v, want true nil", deleted, err)
	}
	assertWorkspaceCascadeGone(t, store, adminUserID, workspace.ID, target)
	assertWorkspaceCascadePresent(t, store, "other-user", otherWorkspace.ID, other)
}

func TestWorkspaceDeleteCascadeViaAPI(t *testing.T) {
	server, baseURL, client := newAuthenticatedWorkspaceTestServer(t)
	_ = getWorkspaceList(t, client, baseURL)
	workspace := createWorkspaceViaAPI(t, client, baseURL, "Cascade API")
	seed := seedWorkspaceCascadeResources(t, server.store, workspace.ID, "api")

	status, body := doJSON(t, client, http.MethodDelete, baseURL+"/workspaces/"+workspace.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("delete cascade workspace status=%d body=%s, want 200", status, body)
	}
	assertWorkspaceCascadeGone(t, server.store, adminUserID, workspace.ID, seed)
	statusWorkspace, bodyWorkspace := doJSON(t, client, http.MethodGet, baseURL+"/workspaces/"+workspace.ID, nil)
	statusState, bodyState := doJSON(t, client, http.MethodGet, baseURL+"/sessions/"+seed.SessionID+"/state", nil)
	statusMessages, bodyMessages := doJSON(t, client, http.MethodGet, baseURL+"/sessions/"+seed.SessionID+"/messages", nil)
	statusEvents, bodyEvents := doJSON(t, client, http.MethodGet, baseURL+"/sessions/"+seed.SessionID+"/events?after=0", nil)
	statusLLM, bodyLLM := doJSON(t, client, http.MethodGet, baseURL+"/workspaces/"+workspace.ID+"/llm-connection", nil)
	for name, got := range map[string]int{
		"workspace":       statusWorkspace,
		"sessionState":    statusState,
		"sessionMessages": statusMessages,
		"sessionEvents":   statusEvents,
		"llmConnection":   statusLLM,
	} {
		if got != http.StatusNotFound {
			t.Fatalf("%s status=%d, want 404", name, got)
		}
	}
	writeJSONEvidence(t, "task-4-cascade-api.txt", map[string]any{
		"deleteStatus":       status,
		"deletedWorkspaceId": workspace.ID,
		"deletedSessionId":   seed.SessionID,
		"apiStatusesAfterDelete": map[string]any{
			"workspace":       map[string]any{"status": statusWorkspace, "body": string(bodyWorkspace)},
			"sessionState":    map[string]any{"status": statusState, "body": string(bodyState)},
			"sessionMessages": map[string]any{"status": statusMessages, "body": string(bodyMessages)},
			"sessionEvents":   map[string]any{"status": statusEvents, "body": string(bodyEvents)},
			"llmConnection":   map[string]any{"status": statusLLM, "body": string(bodyLLM)},
		},
		"storeCleanup": map[string]bool{
			"workspaceGone": true,
			"tokenGone":     true,
			"sessionGone":   true,
			"eventsGone":    true,
			"messagesGone":  true,
			"runGone":       true,
			"llmGone":       true,
			"skillsGone":    true,
			"mcpGone":       true,
		},
		"runtimeHostDirectoriesRemoved": false,
	})
}

func TestWorkspaceDeleteStopsAndRemovesPodOnlyWhenTokenExists(t *testing.T) {
	server, baseURL, client := newAuthenticatedWorkspaceTestServer(t)
	_ = getWorkspaceList(t, client, baseURL)
	recorder := &recordingDriver{}
	server.driver = recorder

	withoutToken := createWorkspaceViaAPI(t, client, baseURL, "No Token")
	status, body := doJSON(t, client, http.MethodDelete, baseURL+"/workspaces/"+withoutToken.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("delete without token status=%d body=%s, want 200", status, body)
	}
	if len(recorder.stopped) != 0 || len(recorder.removed) != 0 {
		t.Fatalf("driver called without active token: stopped=%v removed=%v", recorder.stopped, recorder.removed)
	}

	withToken := createWorkspaceViaAPI(t, client, baseURL, "With Token")
	server.setToken(withToken.ID, "active-token")
	status, body = doJSON(t, client, http.MethodDelete, baseURL+"/workspaces/"+withToken.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("delete with token status=%d body=%s, want 200", status, body)
	}
	if len(recorder.stopped) != 1 || recorder.stopped[0] != withToken.ID || len(recorder.removed) != 1 || recorder.removed[0] != withToken.ID {
		t.Fatalf("driver cleanup calls stopped=%v removed=%v, want workspace %s once", recorder.stopped, recorder.removed, withToken.ID)
	}
	if _, ok := server.workspaceToken(context.Background(), withToken.ID); ok {
		t.Fatalf("server token for deleted workspace %s was not cleared", withToken.ID)
	}
}

type cascadeSeed struct {
	TaskID    string
	SessionID string
	RunID     string
	ModelID   string
	SkillSlug string
	MCPName   string
}

type recordingDriver struct {
	stopped []string
	removed []string
}

func (d *recordingDriver) Start(ctx context.Context, spec driver.AgentPodSpec) (driver.AgentPodInfo, error) {
	return driver.AgentPodInfo{WorkspaceID: spec.WorkspaceID, Status: "running", Endpoint: "http://example.invalid"}, nil
}

func (d *recordingDriver) Stop(ctx context.Context, workspaceID string) error {
	d.stopped = append(d.stopped, workspaceID)
	return nil
}

func (d *recordingDriver) Remove(ctx context.Context, workspaceID string) error {
	d.removed = append(d.removed, workspaceID)
	return nil
}

func (d *recordingDriver) Inspect(ctx context.Context, workspaceID string) (driver.AgentPodInfo, error) {
	return driver.AgentPodInfo{WorkspaceID: workspaceID, Status: "not_found", Endpoint: d.Endpoint(workspaceID)}, nil
}

func (d *recordingDriver) Logs(ctx context.Context, workspaceID string, tail string) ([]byte, error) {
	return []byte{}, nil
}

func (d *recordingDriver) Endpoint(workspaceID string) string {
	return "http://agent-pod-" + workspaceID + ":3001"
}

func createWorkspaceViaAPI(t *testing.T, client *http.Client, baseURL, name string) WorkspaceProjection {
	t.Helper()
	status, body := doJSON(t, client, http.MethodPost, baseURL+"/workspaces", map[string]any{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("create workspace %q status=%d body=%s, want 201", name, status, body)
	}
	var response workspaceEnvelope
	decodeJSON(t, body, &response)
	return response.Workspace
}

func seedWorkspaceCascadeResources(t *testing.T, store EventStore, workspaceID, suffix string) cascadeSeed {
	t.Helper()
	ctx := context.Background()
	seed := cascadeSeed{
		TaskID:    "task-cascade-" + suffix,
		SessionID: "sess-cascade-" + suffix,
		RunID:     "run-cascade-" + suffix,
		ModelID:   "model-cascade-" + suffix,
		SkillSlug: "skill-cascade-" + suffix,
		MCPName:   "mcp-cascade-" + suffix,
	}
	if err := store.SaveWorkspaceToken(ctx, workspaceID, "token-"+suffix); err != nil {
		t.Fatalf("save workspace token: %v", err)
	}
	connection, err := store.UpsertWorkspaceLLMConnection(ctx, workspaceID, LLMConnection{ID: "llm-cascade-" + suffix, Provider: defaultLLMProvider, APIProtocol: defaultLLMAPIProtocol, BaseURL: "http://llm.invalid", APIKey: "secret"})
	if err != nil {
		t.Fatalf("upsert llm connection: %v", err)
	}
	if _, err := store.UpsertWorkspaceLLMModel(ctx, workspaceID, LLMConnectionModel{ID: "llm-model-cascade-" + suffix, ConnectionID: connection.ID, ModelID: seed.ModelID, Source: llmModelSourceManual, Enabled: true, Raw: map[string]any{"suffix": suffix}}); err != nil {
		t.Fatalf("upsert llm model: %v", err)
	}
	if _, err := store.UpsertWorkspaceSkill(ctx, workspaceID, SkillDefinitionWithFiles{Definition: SkillDefinition{ID: "skill-def-cascade-" + suffix, Slug: seed.SkillSlug, Name: "Cascade Skill"}, Files: []SkillFile{{ID: "skill-file-cascade-" + suffix, Path: "SKILL.md", Content: "# Cascade", ContentHash: "hash-" + suffix}}}); err != nil {
		t.Fatalf("upsert workspace skill: %v", err)
	}
	if _, err := store.UpsertWorkspaceMCPServer(ctx, workspaceID, MCPServerDefinitionWithEnv{Definition: MCPServerDefinition{ID: "mcp-def-cascade-" + suffix, Name: seed.MCPName, Command: "node", Args: []string{"server.js"}, Transport: "stdio", ContentHash: "hash-" + suffix}, Env: []MCPServerEnv{{ID: "mcp-env-cascade-" + suffix, Name: "TOKEN", Value: "secret"}}}); err != nil {
		t.Fatalf("upsert workspace mcp server: %v", err)
	}
	if _, _, err := store.CreateSession(ctx, workspaceID, seed.TaskID, seed.SessionID, protocol.CreateSessionRequest{Title: "Cascade Session", ModelID: seed.ModelID, Metadata: map[string]any{"suffix": suffix}}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := store.StartRun(ctx, protocol.TurnRequest{WorkspaceID: workspaceID, TaskID: seed.TaskID, SessionID: seed.SessionID, RunID: seed.RunID, Message: "hello", Source: "test"}); err != nil {
		t.Fatalf("start run: %v", err)
	}
	if _, err := store.Append(ctx, protocol.UniversalEvent{Type: protocol.EventMessageCompleted, WorkspaceID: workspaceID, TaskID: seed.TaskID, SessionID: seed.SessionID, RunID: seed.RunID, MessageID: "msg-cascade-" + suffix, Role: "assistant", Content: []protocol.UniversalBlock{{Type: "text", Text: "hello"}}}); err != nil {
		t.Fatalf("append message event: %v", err)
	}
	return seed
}

func assertWorkspaceCascadePresent(t *testing.T, store EventStore, ownerUserID, workspaceID string, seed cascadeSeed) {
	t.Helper()
	ctx := context.Background()
	if _, ok, err := store.GetWorkspace(ctx, ownerUserID, workspaceID); err != nil || !ok {
		t.Fatalf("workspace missing before owned delete: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.WorkspaceToken(ctx, workspaceID); err != nil || !ok {
		t.Fatalf("workspace token missing before owned delete: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetSession(ctx, seed.SessionID); err != nil || !ok {
		t.Fatalf("session missing before owned delete: ok=%v err=%v", ok, err)
	}
	if events, err := store.ListEventsBySession(ctx, seed.SessionID, 0, DefaultEventReplayLimit); err != nil || len(events) == 0 {
		t.Fatalf("events before owned delete len=%d err=%v, want >0", len(events), err)
	}
	if messages, err := store.ListMessagesBySession(ctx, seed.SessionID); err != nil || len(messages) == 0 || len(messages[0].Blocks) == 0 {
		t.Fatalf("messages before owned delete=%+v err=%v, want message with blocks", messages, err)
	}
	if _, ok, err := store.GetRun(ctx, seed.SessionID, seed.RunID); err != nil || !ok {
		t.Fatalf("run missing before owned delete: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetWorkspaceLLMConnection(ctx, workspaceID); err != nil || !ok {
		t.Fatalf("llm connection missing before owned delete: ok=%v err=%v", ok, err)
	}
	if models, err := store.ListWorkspaceLLMModels(ctx, workspaceID); err != nil || len(models) == 0 {
		t.Fatalf("llm models before owned delete len=%d err=%v, want >0", len(models), err)
	}
	if _, ok, err := store.GetWorkspaceSkill(ctx, workspaceID, seed.SkillSlug); err != nil || !ok {
		t.Fatalf("skill missing before owned delete: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetWorkspaceMCPServer(ctx, workspaceID, seed.MCPName); err != nil || !ok {
		t.Fatalf("mcp server missing before owned delete: ok=%v err=%v", ok, err)
	}
}

func assertWorkspaceCascadeGone(t *testing.T, store EventStore, ownerUserID, workspaceID string, seed cascadeSeed) {
	t.Helper()
	ctx := context.Background()
	if _, ok, err := store.GetWorkspace(ctx, ownerUserID, workspaceID); err != nil || ok {
		t.Fatalf("workspace after delete ok=%v err=%v, want false nil", ok, err)
	}
	if _, ok, err := store.WorkspaceToken(ctx, workspaceID); err != nil || ok {
		t.Fatalf("workspace token after delete ok=%v err=%v, want false nil", ok, err)
	}
	if _, ok, err := store.GetSession(ctx, seed.SessionID); err != nil || ok {
		t.Fatalf("session after delete ok=%v err=%v, want false nil", ok, err)
	}
	if sessions, err := store.ListWorkspaceSessions(ctx, workspaceID, 20, 0); err != nil || len(sessions) != 0 {
		t.Fatalf("workspace sessions after delete len=%d err=%v, want 0", len(sessions), err)
	}
	if events, err := store.ListEventsBySession(ctx, seed.SessionID, 0, DefaultEventReplayLimit); err != nil || len(events) != 0 {
		t.Fatalf("events after delete len=%d err=%v, want 0", len(events), err)
	}
	if messages, err := store.ListMessagesBySession(ctx, seed.SessionID); err != nil || len(messages) != 0 {
		t.Fatalf("messages after delete len=%d err=%v, want 0", len(messages), err)
	}
	state, err := store.SessionState(ctx, seed.SessionID)
	if err != nil || state.LatestEventID != 0 || len(state.Messages) != 0 || state.ActiveRun != nil {
		t.Fatalf("state after delete=%+v err=%v, want empty", state, err)
	}
	if _, ok, err := store.GetRun(ctx, seed.SessionID, seed.RunID); err != nil || ok {
		t.Fatalf("run after delete ok=%v err=%v, want false nil", ok, err)
	}
	if _, ok, err := store.GetWorkspaceLLMConnection(ctx, workspaceID); err != nil || ok {
		t.Fatalf("llm connection after delete ok=%v err=%v, want false nil", ok, err)
	}
	if models, err := store.ListWorkspaceLLMModels(ctx, workspaceID); err != nil || len(models) != 0 {
		t.Fatalf("llm models after delete len=%d err=%v, want 0", len(models), err)
	}
	if skills, err := store.ListWorkspaceSkills(ctx, workspaceID); err != nil || len(skills) != 0 {
		t.Fatalf("skills after delete len=%d err=%v, want 0", len(skills), err)
	}
	if _, ok, err := store.GetWorkspaceSkill(ctx, workspaceID, seed.SkillSlug); err != nil || ok {
		t.Fatalf("skill after delete ok=%v err=%v, want false nil", ok, err)
	}
	if servers, err := store.ListWorkspaceMCPServers(ctx, workspaceID); err != nil || len(servers) != 0 {
		t.Fatalf("mcp servers after delete len=%d err=%v, want 0", len(servers), err)
	}
	if _, ok, err := store.GetWorkspaceMCPServer(ctx, workspaceID, seed.MCPName); err != nil || ok {
		t.Fatalf("mcp server after delete ok=%v err=%v, want false nil", ok, err)
	}
}

func writeJSONEvidence(t *testing.T, name string, value any) {
	t.Helper()
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal evidence %s: %v", name, err)
	}
	path := filepath.Join("..", "..", ".sisyphus", "evidence", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create evidence dir for %s: %v", name, err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write evidence %s: %v", name, err)
	}
}

func newAuthenticatedWorkspaceTestServer(t *testing.T) (*Server, string, *http.Client) {
	t.Helper()
	server := NewServer(Config{})
	testServer := httptest.NewServer(server.Routes())
	t.Cleanup(testServer.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := testServer.Client()
	client.Jar = jar
	status, body := doJSON(t, client, http.MethodPost, testServer.URL+"/auth/login", map[string]any{"username": "admin", "password": "admin"})
	if status != http.StatusOK {
		t.Fatalf("login status=%d body=%s, want 200", status, body)
	}
	return server, testServer.URL, client
}

func getWorkspaceList(t *testing.T, client *http.Client, baseURL string) workspaceListResponse {
	t.Helper()
	status, body := doJSON(t, client, http.MethodGet, baseURL+"/workspaces", nil)
	if status != http.StatusOK {
		t.Fatalf("list status=%d body=%s, want 200", status, body)
	}
	var response workspaceListResponse
	decodeJSON(t, body, &response)
	return response
}

func doJSON(t *testing.T, client *http.Client, method, url string, payload any) (int, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal request payload: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return response.StatusCode, responseBody
}

func decodeJSON(t *testing.T, body []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

func assertWorkspaceProjection(t *testing.T, workspace WorkspaceProjection, name, ownerUserID string) {
	t.Helper()
	if !uuidPattern.MatchString(workspace.ID) {
		t.Fatalf("workspace id %q is not a v4 UUID", workspace.ID)
	}
	if workspace.ID == "ws_dev" {
		t.Fatalf("workspace id must not be ws_dev")
	}
	if workspace.Name != name {
		t.Fatalf("workspace name=%q, want %q", workspace.Name, name)
	}
	if workspace.OwnerUserID != ownerUserID {
		t.Fatalf("workspace owner=%q, want %q", workspace.OwnerUserID, ownerUserID)
	}
}

func assertNoWorkspaceNamed(t *testing.T, response workspaceListResponse, name string) {
	t.Helper()
	for _, workspace := range response.Workspaces {
		if workspace.Name == name {
			t.Fatalf("workspace named %q persisted unexpectedly in %+v", name, response.Workspaces)
		}
	}
}

func assertWorkspaceResponseShape(t *testing.T, status int, body []byte, topLevelKey string) {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("shape response status=%d body=%s, want 200", status, body)
	}
	var top map[string]json.RawMessage
	decodeJSON(t, body, &top)
	var workspaces []map[string]any
	decodeJSON(t, top[topLevelKey], &workspaces)
	if len(workspaces) == 0 {
		t.Fatalf("shape response contains no workspaces: %s", body)
	}
	for _, key := range []string{"id", "name", "ownerUserId"} {
		if _, ok := workspaces[0][key]; !ok {
			t.Fatalf("workspace object missing key %q: %#v", key, workspaces[0])
		}
	}
	for _, key := range []string{"workspaceId", "description", "metadata", "createdAt", "updatedAt", "podToken"} {
		if _, ok := workspaces[0][key]; ok {
			t.Fatalf("workspace object exposed removed key %q: %#v", key, workspaces[0])
		}
	}
	if len(workspaces[0]) != 3 {
		t.Fatalf("workspace object keys=%#v, want exactly id/name/ownerUserId", workspaces[0])
	}
}
