package driver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"strings"
)

type AgentPodSpec struct {
	WorkspaceID   string `json:"workspaceId"`
	WorkspacePath string `json:"workspacePath"`
	Image         string `json:"image"`
	Network       string `json:"network"`
	Token         string `json:"-"`
}

type AgentPodInfo struct {
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	Image       string `json:"image"`
	Network     string `json:"network"`
	Status      string `json:"status"`
	Endpoint    string `json:"endpoint"`
}

type Config struct {
	DockerSocket  string
	DockerNetwork string
	AgentPodImage string
	WorkspaceRoot string
}

type Driver interface {
	Start(ctx context.Context, spec AgentPodSpec) (AgentPodInfo, error)
	Stop(ctx context.Context, workspaceID string) error
	Remove(ctx context.Context, workspaceID string) error
	Inspect(ctx context.Context, workspaceID string) (AgentPodInfo, error)
	Logs(ctx context.Context, workspaceID string, tail string) ([]byte, error)
	Endpoint(workspaceID string) string
}

func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

var idPattern = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

func SafeID(value string) string {
	cleaned := idPattern.ReplaceAllString(value, "-")
	cleaned = strings.Trim(cleaned, "-._")
	if cleaned == "" {
		return "workspace"
	}
	return cleaned
}
