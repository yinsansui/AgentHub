package plugins

import (
	"context"
	"encoding/json"
)

type ConfigField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type ConfigValidation struct {
	Config       any
	StoredConfig map[string]any
}

type BuildContext struct {
	WorkspaceID string
	ScriptPath  string
}

type SkillFileContribution struct {
	Path    string
	Content string
}

type SkillContribution struct {
	Slug        string
	Name        string
	Description string
	Files       []SkillFileContribution
}

type MCPEnvContribution struct {
	Name  string
	Value string
}

type MCPServerContribution struct {
	Name      string
	Command   string
	Args      []string
	Transport string
	Env       []MCPEnvContribution
}

type Contributions struct {
	Skills     []SkillContribution
	MCPServers []MCPServerContribution
}

type Definition struct {
	ID             string
	Name           string
	Description    string
	Icon           string
	ScriptFilename string
	ConfigFields   []ConfigField
	ValidateConfig func(json.RawMessage) (ConfigValidation, error)
	Build          func(context.Context, BuildContext, ConfigValidation) (Contributions, error)
}
