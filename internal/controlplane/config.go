package controlplane

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Addr                    string
	DockerSocket            string
	DockerNetwork           string
	AgentPodImage           string
	WorkspaceRoot           string
	AgentPodBaseURLTemplate string
	DevAgentPodToken        string
	DatabaseURL             string
	PluginRuntimeScriptsDir string
	RunTimeout              time.Duration
}

func LoadConfig() Config {
	cwd, _ := os.Getwd()
	return Config{
		Addr:                    getenv("AGENTHUB_CONTROL_PLANE_ADDR", ":3000"),
		DockerSocket:            getenv("AGENTHUB_DOCKER_SOCKET", "/var/run/docker.sock"),
		DockerNetwork:           getenv("AGENTHUB_DOCKER_NETWORK", "agenthub"),
		AgentPodImage:           getenv("AGENTHUB_AGENT_POD_IMAGE", "agenthub-agent-pod:dev"),
		WorkspaceRoot:           getenv("AGENTHUB_WORKSPACE_ROOT", filepath.Join(cwd, ".agenthub", "workspaces")),
		AgentPodBaseURLTemplate: getenv("AGENTHUB_AGENT_POD_BASE_URL_TEMPLATE", "http://agent-pod-{workspaceId}:3001"),
		DevAgentPodToken:        getenv("AGENTHUB_DEV_AGENT_POD_TOKEN", ""),
		DatabaseURL:             getenv("AGENTHUB_DATABASE_URL", ""),
		PluginRuntimeScriptsDir: getenv("AGENTHUB_PLUGIN_RUNTIME_SCRIPTS_DIR", filepath.Join(cwd, "runtimes", "ts-runtime-host", "dist")),
		RunTimeout:              durationEnv("AGENTHUB_RUN_TIMEOUT", 30*time.Minute),
	}
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := getenv(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
