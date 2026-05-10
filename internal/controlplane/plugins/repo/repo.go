package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"agenthub/internal/controlplane/plugins"
)

const (
	ID             = "repo"
	ScriptFilename = "repo-mcp.js"
)

var (
	repoNamePattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)
	scpLikeURLPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._~/-]+$`)
)

type Config struct {
	Repositories []Repository `json:"repositories"`
}

type Repository struct {
	RepoName string `json:"repoName"`
	RepoURL  string `json:"repoUrl"`
}

func Definition() plugins.Definition {
	return plugins.Definition{
		ID:             ID,
		Name:           "Repository",
		Description:    "Connects a workspace to source repositories for agent file access.",
		Icon:           "git-branch",
		ScriptFilename: ScriptFilename,
		ConfigFields: []plugins.ConfigField{
			{Name: "repositories", Type: "array", Required: true, Description: "Repository entries with repoName and repoUrl."},
		},
		ValidateConfig: ValidateConfig,
		Build:          Build,
	}
}

func ValidateConfig(raw json.RawMessage) (plugins.ConfigValidation, error) {
	var config Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return plugins.ConfigValidation{}, err
		}
	}
	if len(config.Repositories) == 0 {
		return plugins.ConfigValidation{}, errors.New("repositories is required")
	}
	seenNames := map[string]bool{}
	for idx := range config.Repositories {
		repoName := strings.TrimSpace(config.Repositories[idx].RepoName)
		if err := validateSafeSegment(repoName, "repoName"); err != nil {
			return plugins.ConfigValidation{}, err
		}
		if seenNames[repoName] {
			return plugins.ConfigValidation{}, errors.New("duplicate repoName: " + repoName)
		}
		seenNames[repoName] = true
		repoURL := strings.TrimSpace(config.Repositories[idx].RepoURL)
		if err := validateRepoURL(repoURL); err != nil {
			return plugins.ConfigValidation{}, err
		}
		config.Repositories[idx] = Repository{RepoName: repoName, RepoURL: repoURL}
	}
	return plugins.ConfigValidation{Config: config, StoredConfig: map[string]any{"repositories": config.Repositories}}, nil
}

func Build(ctx context.Context, buildCtx plugins.BuildContext, validated plugins.ConfigValidation) (plugins.Contributions, error) {
	_ = ctx
	config, ok := validated.Config.(Config)
	if !ok {
		return plugins.Contributions{}, errors.New("invalid repo plugin config")
	}
	repositoriesPayload, err := json.Marshal(config.Repositories)
	if err != nil {
		return plugins.Contributions{}, err
	}
	return plugins.Contributions{
		Skills: []plugins.SkillContribution{
			{
				Slug:        ID,
				Name:        "Repository Plugin",
				Description: "Guides the agent to clone and inspect the configured repository.",
				Files: []plugins.SkillFileContribution{
					{Path: "SKILL.md", Content: SkillContent(config)},
				},
			},
		},
		MCPServers: []plugins.MCPServerContribution{
			{
				Name:      ID,
				Command:   "node",
				Args:      []string{buildCtx.ScriptPath},
				Transport: "stdio",
				Env: []plugins.MCPEnvContribution{
					{Name: "AGENTHUB_REPOSITORIES", Value: string(repositoriesPayload)},
				},
			},
		},
	}, nil
}

func SkillContent(config Config) string {
	lines := make([]string, 0, len(config.Repositories))
	for _, repo := range config.Repositories {
		lines = append(lines, fmt.Sprintf("- %s", repo.RepoName))
	}
	return fmt.Sprintf(`# Repository Plugin

This workspace has a repository plugin configured with these repositories:
%s

When the user asks about one of these repositories, use the repo MCP clone tool first with the repository name to clone or refresh that repository. After cloning, read and search files directly from the cloned working tree instead of using tools to inspect file contents indirectly.
`, strings.Join(lines, "\n"))
}

func validateSafeSegment(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New(field + " is required")
	}
	if !repoNamePattern.MatchString(value) || value == "." || value == ".." {
		return errors.New(field + " must be a safe path segment")
	}
	return nil
}

func validateRepoURL(value string) error {
	if value == "" {
		return errors.New("repoUrl is required")
	}
	if strings.HasPrefix(value, "-") || strings.ContainsFunc(value, unicode.IsControl) {
		return errors.New("repoUrl must be a safe Git URL")
	}
	if scpLikeURLPattern.MatchString(value) {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("repoUrl must be an https or ssh Git URL")
	}
	switch parsed.Scheme {
	case "https":
		if parsed.User != nil {
			return errors.New("repoUrl must not include credentials")
		}
	case "ssh":
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return errors.New("repoUrl must not include credentials")
		}
	default:
		return errors.New("repoUrl must be an https or ssh Git URL")
	}
	return nil
}
