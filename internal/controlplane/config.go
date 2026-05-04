package controlplane

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Addr                    string
	DockerSocket            string
	DockerNetwork           string
	AgentPodImage           string
	WorkspaceRoot           string
	StatePath               string
	AgentPodBaseURLTemplate string
	DevAgentPodToken        string
}

func LoadConfig() Config {
	cwd, _ := os.Getwd()
	return Config{
		Addr:                    getenv("AGENTHUB_CONTROL_PLANE_ADDR", ":3000"),
		DockerSocket:            getenv("AGENTHUB_DOCKER_SOCKET", "/var/run/docker.sock"),
		DockerNetwork:           getenv("AGENTHUB_DOCKER_NETWORK", "agenthub"),
		AgentPodImage:           getenv("AGENTHUB_AGENT_POD_IMAGE", "agenthub-pi-agent-pod:dev"),
		WorkspaceRoot:           getenv("AGENTHUB_WORKSPACE_ROOT", filepath.Join(cwd, ".agenthub", "workspaces")),
		StatePath:               getenv("AGENTHUB_STATE_PATH", filepath.Join(cwd, ".agenthub", "events.ndjson")),
		AgentPodBaseURLTemplate: getenv("AGENTHUB_AGENT_POD_BASE_URL_TEMPLATE", "http://agent-pod-{workspaceId}:3001"),
		DevAgentPodToken:        getenv("AGENTHUB_DEV_AGENT_POD_TOKEN", ""),
	}
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
