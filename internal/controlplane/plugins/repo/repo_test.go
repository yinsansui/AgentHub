package repo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agenthub/internal/controlplane/plugins"
)

func TestDefinitionMetadata(t *testing.T) {
	definition := Definition()
	if definition.ID != ID || definition.ScriptFilename != ScriptFilename || definition.Icon != "git-branch" {
		t.Fatalf("definition=%+v, want repo metadata", definition)
	}
	if len(definition.ConfigFields) != 1 || definition.ConfigFields[0].Name != "repositories" || !definition.ConfigFields[0].Required {
		t.Fatalf("configFields=%+v, want required repositories field", definition.ConfigFields)
	}
}

func TestValidateConfig(t *testing.T) {
	valid := rawConfig(t, []Repository{
		{RepoName: " AgentHub ", RepoURL: " https://example.com/repo.git "},
		{RepoName: "Docs", RepoURL: "https://example.com/docs.git"},
	})
	validated, err := ValidateConfig(valid)
	if err != nil {
		t.Fatalf("ValidateConfig(valid): %v", err)
	}
	config, ok := validated.Config.(Config)
	if !ok || len(config.Repositories) != 2 || config.Repositories[0].RepoName != "AgentHub" || config.Repositories[0].RepoURL != "https://example.com/repo.git" {
		t.Fatalf("validated config=%+v, want trimmed multi-repo config", validated.Config)
	}
	stored, ok := validated.StoredConfig["repositories"].([]Repository)
	if !ok || len(stored) != 2 || stored[1].RepoName != "Docs" {
		t.Fatalf("stored config=%+v, want repositories", validated.StoredConfig)
	}

	for name, raw := range map[string]json.RawMessage{
		"empty repositories":   rawConfig(t, nil),
		"bad repoName path":    rawConfig(t, []Repository{{RepoName: "../AgentHub", RepoURL: "https://example.com/repo.git"}}),
		"bad repoName space":   rawConfig(t, []Repository{{RepoName: "My Repo", RepoURL: "https://example.com/repo.git"}}),
		"bad repoName colon":   rawConfig(t, []Repository{{RepoName: "repo:main", RepoURL: "https://example.com/repo.git"}}),
		"bad repoName newline": rawConfig(t, []Repository{{RepoName: "repo\nmain", RepoURL: "https://example.com/repo.git"}}),
		"empty repoUrl":        rawConfig(t, []Repository{{RepoName: "AgentHub", RepoURL: "   "}}),
		"repoUrl option":       rawConfig(t, []Repository{{RepoName: "AgentHub", RepoURL: "--upload-pack=sh"}}),
		"repoUrl file":         rawConfig(t, []Repository{{RepoName: "AgentHub", RepoURL: "file:///tmp/repo.git"}}),
		"repoUrl credentials":  rawConfig(t, []Repository{{RepoName: "AgentHub", RepoURL: "https://token@example.com/repo.git"}}),
		"duplicate repoName":   rawConfig(t, []Repository{{RepoName: "AgentHub", RepoURL: "https://example.com/repo.git"}, {RepoName: "AgentHub", RepoURL: "https://example.com/other.git"}}),
	} {
		if _, err := ValidateConfig(raw); err == nil {
			t.Fatalf("%s ValidateConfig returned nil error", name)
		}
	}
}

func TestBuildContributions(t *testing.T) {
	validated, err := ValidateConfig(rawConfig(t, []Repository{
		{RepoName: "AgentHub", RepoURL: "https://example.com/repo.git"},
		{RepoName: "Docs", RepoURL: "https://example.com/docs.git"},
	}))
	if err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	contributions, err := Build(context.Background(), plugins.BuildContext{WorkspaceID: "workspace-1", ScriptPath: "/opt/agenthub/ts-runtime-host/dist/repo-mcp.js"}, validated)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(contributions.Skills) != 1 || contributions.Skills[0].Slug != ID || contributions.Skills[0].Files[0].Path != "SKILL.md" {
		t.Fatalf("skills=%+v, want repo skill contribution", contributions.Skills)
	}
	skillContent := contributions.Skills[0].Files[0].Content
	if !strings.Contains(skillContent, "AgentHub") || !strings.Contains(skillContent, "Docs") || !strings.Contains(skillContent, "read and search files directly") {
		t.Fatalf("skill content=%q, want repository instructions", skillContent)
	}
	if strings.Contains(skillContent, "https://example.com/repo.git") {
		t.Fatalf("skill content=%q, must not include repository URLs", skillContent)
	}
	if len(contributions.MCPServers) != 1 {
		t.Fatalf("mcp servers=%+v, want one repo MCP contribution", contributions.MCPServers)
	}
	mcp := contributions.MCPServers[0]
	if mcp.Name != ID || mcp.Command != "node" || mcp.Transport != "stdio" || len(mcp.Args) != 1 || mcp.Args[0] != "/opt/agenthub/ts-runtime-host/dist/repo-mcp.js" {
		t.Fatalf("mcp=%+v, want repo node stdio MCP", mcp)
	}
	if len(mcp.Env) != 1 || mcp.Env[0].Name != "AGENTHUB_REPOSITORIES" {
		t.Fatalf("env=%+v, want AGENTHUB_REPOSITORIES", mcp.Env)
	}
	if strings.Contains(mcp.Env[0].Value, "AGENTHUB_REPO_NAME") || strings.Contains(mcp.Env[0].Value, "AGENTHUB_REPO_URL") {
		t.Fatalf("env value=%q, must not include legacy repo env names", mcp.Env[0].Value)
	}
}

func rawConfig(t *testing.T, repositories []Repository) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(Config{Repositories: repositories})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return payload
}
