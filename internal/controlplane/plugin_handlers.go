package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agenthub/internal/controlplane/plugins"
	repoPlugin "agenthub/internal/controlplane/plugins/repo"
	"agenthub/pkg/protocol"
)

const bundledRepoPluginID = repoPlugin.ID

type workspacePluginResponse struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Description  string                `json:"description"`
	Icon         string                `json:"icon,omitempty"`
	ConfigFields []plugins.ConfigField `json:"configFields"`
	Installed    bool                  `json:"installed"`
	Config       map[string]any        `json:"config,omitempty"`
	UpdatedAt    string                `json:"updatedAt,omitempty"`
}

type installWorkspacePluginRequest struct {
	Config json.RawMessage `json:"config"`
}

var bundledPluginRegistry = map[string]plugins.Definition{
	repoPlugin.ID: repoPlugin.Definition(),
}

func (s *Server) handleListWorkspacePlugins(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	installs, err := s.store.ListWorkspacePluginInstalls(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	installedByID := map[string]WorkspacePluginInstall{}
	for _, install := range installs {
		installedByID[install.PluginID] = install
	}
	ids := make([]string, 0, len(bundledPluginRegistry))
	for id := range bundledPluginRegistry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	plugins := make([]workspacePluginResponse, 0, len(ids))
	for _, id := range ids {
		plugin := bundledPluginRegistry[id]
		response := workspacePluginResponse{ID: plugin.ID, Name: plugin.Name, Description: plugin.Description, Icon: plugin.Icon, ConfigFields: plugin.ConfigFields}
		if install, ok := installedByID[id]; ok {
			response.Installed = true
			response.Config = clonePluginConfig(install.Config)
			response.UpdatedAt = install.UpdatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
		}
		plugins = append(plugins, response)
	}
	writeJSON(w, map[string]any{"plugins": plugins})
}

func (s *Server) handlePutWorkspacePluginInstall(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok, err := s.existingWorkspaceIDFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	pluginID := r.PathValue("pluginId")
	plugin, ok := bundledPluginRegistry[pluginID]
	if !ok {
		http.Error(w, "plugin not found", http.StatusNotFound)
		return
	}
	var req installWorkspacePluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	validatedConfig, err := plugin.ValidateConfig(req.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.initializePlugin(r.Context(), workspaceID, plugin, validatedConfig); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	install, err := s.store.UpsertWorkspacePluginInstall(r.Context(), workspaceID, WorkspacePluginInstall{PluginID: plugin.ID, Config: validatedConfig.StoredConfig})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"plugin": workspacePluginResponse{
		ID:           plugin.ID,
		Name:         plugin.Name,
		Description:  plugin.Description,
		Icon:         plugin.Icon,
		ConfigFields: plugin.ConfigFields,
		Installed:    true,
		Config:       clonePluginConfig(install.Config),
		UpdatedAt:    install.UpdatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
	}})
}

func (s *Server) initializePlugin(ctx context.Context, workspaceID string, plugin plugins.Definition, validated plugins.ConfigValidation) error {
	scriptPath := ""
	if strings.TrimSpace(plugin.ScriptFilename) != "" {
		scriptPath = s.pluginRuntimeScriptPath(plugin.ScriptFilename)
	}
	contributions, err := plugin.Build(ctx, plugins.BuildContext{WorkspaceID: workspaceID, ScriptPath: scriptPath}, validated)
	if err != nil {
		return err
	}
	for _, contribution := range contributions.Skills {
		skill, err := buildPluginSkill(workspaceID, contribution)
		if err != nil {
			return err
		}
		if _, err := s.store.UpsertWorkspacePluginSkill(ctx, workspaceID, skill); err != nil {
			return err
		}
	}
	for _, contribution := range contributions.MCPServers {
		server, err := buildPluginMCPServer(workspaceID, contribution)
		if err != nil {
			return err
		}
		if _, err := s.store.UpsertWorkspacePluginMCPServer(ctx, workspaceID, server); err != nil {
			return err
		}
	}
	return nil
}

func buildPluginSkill(workspaceID string, contribution plugins.SkillContribution) (SkillDefinitionWithFiles, error) {
	slug := strings.TrimSpace(contribution.Slug)
	if err := validateSafeSegment(slug, "skill slug"); err != nil {
		return SkillDefinitionWithFiles{}, err
	}
	if len(contribution.Files) == 0 {
		return SkillDefinitionWithFiles{}, errors.New("plugin skill must include at least one file")
	}
	definitionID := stableDefinitionID("skill", protocol.SkillSourcePlugin, "workspace", workspaceID, slug)
	files := make([]SkillFile, 0, len(contribution.Files))
	for _, contributedFile := range contribution.Files {
		path, err := cleanDefinitionFilePath(contributedFile.Path)
		if err != nil {
			return SkillDefinitionWithFiles{}, err
		}
		files = append(files, SkillFile{
			ID:          stableDefinitionID("skill_file", protocol.SkillSourcePlugin, "workspace", workspaceID, slug, path),
			SkillID:     definitionID,
			Path:        path,
			Content:     contributedFile.Content,
			ContentHash: hashStrings(contributedFile.Content),
		})
	}
	name := strings.TrimSpace(contribution.Name)
	description := strings.TrimSpace(contribution.Description)
	return SkillDefinitionWithFiles{
		Definition: SkillDefinition{
			ID:          definitionID,
			Slug:        slug,
			Source:      protocol.SkillSourcePlugin,
			ScopeType:   "workspace",
			ScopeID:     workspaceID,
			Name:        name,
			Description: description,
			ContentHash: hashSkillDefinition(name, description, files),
		},
		Files: files,
	}, nil
}

func buildPluginMCPServer(workspaceID string, contribution plugins.MCPServerContribution) (MCPServerDefinitionWithEnv, error) {
	name := strings.TrimSpace(contribution.Name)
	if err := validateSafeSegment(name, "mcp server name"); err != nil {
		return MCPServerDefinitionWithEnv{}, err
	}
	command := strings.TrimSpace(contribution.Command)
	if command == "" {
		return MCPServerDefinitionWithEnv{}, errors.New("mcp server command is required")
	}
	transport := strings.TrimSpace(contribution.Transport)
	if transport == "" {
		transport = "stdio"
	}
	serverID := stableDefinitionID("mcp", protocol.SkillSourcePlugin, "workspace", workspaceID, name)
	env := make([]MCPServerEnv, 0, len(contribution.Env))
	for _, item := range contribution.Env {
		envName := strings.TrimSpace(item.Name)
		if envName == "" {
			return MCPServerDefinitionWithEnv{}, errors.New("mcp server env name is required")
		}
		env = append(env, MCPServerEnv{ID: stableDefinitionID("mcp_env", serverID, envName), ServerID: serverID, Name: envName, Value: item.Value})
	}
	args := append([]string(nil), contribution.Args...)
	return MCPServerDefinitionWithEnv{
		Definition: MCPServerDefinition{
			ID:          serverID,
			Name:        name,
			Source:      protocol.SkillSourcePlugin,
			ScopeType:   "workspace",
			ScopeID:     workspaceID,
			Command:     command,
			Args:        args,
			Transport:   transport,
			ContentHash: hashMCPDefinition(command, args, transport, env),
		},
		Env: env,
	}, nil
}

func (s *Server) pluginRuntimeScriptPath(filename string) string {
	dir := strings.TrimSpace(s.config.PluginRuntimeScriptsDir)
	if dir == "" {
		cwd, _ := os.Getwd()
		dir = filepath.Join(cwd, "runtimes", "ts-runtime-host", "dist")
	}
	return filepath.Join(dir, filename)
}

func clonePluginConfig(config map[string]any) map[string]any {
	if len(config) == 0 {
		return nil
	}
	payload, err := json.Marshal(config)
	if err != nil {
		out := make(map[string]any, len(config))
		for key, value := range config {
			out[key] = value
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil
	}
	return out
}
