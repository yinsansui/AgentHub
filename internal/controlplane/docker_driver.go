package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

type DockerAgentPodDriver struct {
	client *dockerClient
	config Config
}

func NewDockerAgentPodDriver(config Config) *DockerAgentPodDriver {
	return &DockerAgentPodDriver{client: newDockerClient(config.DockerSocket), config: config}
}

func (d *DockerAgentPodDriver) Start(ctx context.Context, spec AgentPodSpec) (AgentPodInfo, error) {
	if spec.WorkspaceID == "" {
		return AgentPodInfo{}, errors.New("workspaceId is required")
	}
	if spec.Image == "" {
		spec.Image = d.config.AgentPodImage
	}
	if spec.Network == "" {
		spec.Network = d.config.DockerNetwork
	}
	if spec.WorkspacePath == "" {
		spec.WorkspacePath = filepath.Join(d.config.WorkspaceRoot, safeID(spec.WorkspaceID))
	}
	if spec.Token == "" {
		token, err := newToken()
		if err != nil {
			return AgentPodInfo{}, err
		}
		spec.Token = token
	}
	if err := os.MkdirAll(spec.WorkspacePath, 0o755); err != nil {
		return AgentPodInfo{}, err
	}
	if err := d.ensureNetwork(ctx, spec.Network); err != nil {
		return AgentPodInfo{}, err
	}
	name := podName(spec.WorkspaceID)
	if _, _, err := d.client.do(ctx, http.MethodPost, "/containers/create?name="+name, createContainerBody(spec, name), http.StatusCreated); err != nil {
		if !strings.Contains(err.Error(), "409") {
			return AgentPodInfo{}, err
		}
	}
	if _, _, err := d.client.do(ctx, http.MethodPost, "/containers/"+escapePathPart(name)+"/start", nil, http.StatusNoContent, http.StatusNotModified); err != nil {
		if !strings.Contains(err.Error(), "304") {
			return AgentPodInfo{}, err
		}
	}
	return d.Inspect(ctx, spec.WorkspaceID)
}

func (d *DockerAgentPodDriver) Stop(ctx context.Context, workspaceID string) error {
	name := podName(workspaceID)
	_, status, err := d.client.do(ctx, http.MethodPost, "/containers/"+escapePathPart(name)+"/stop?t=10", nil, http.StatusNoContent, http.StatusNotModified, http.StatusNotFound)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

func (d *DockerAgentPodDriver) Remove(ctx context.Context, workspaceID string) error {
	name := podName(workspaceID)
	_, status, err := d.client.do(ctx, http.MethodDelete, "/containers/"+escapePathPart(name)+"?force=1", nil, http.StatusNoContent, http.StatusNotFound)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

func (d *DockerAgentPodDriver) Inspect(ctx context.Context, workspaceID string) (AgentPodInfo, error) {
	name := podName(workspaceID)
	payload, status, err := d.client.do(ctx, http.MethodGet, "/containers/"+escapePathPart(name)+"/json", nil, http.StatusOK, http.StatusNotFound)
	if status == http.StatusNotFound {
		return AgentPodInfo{WorkspaceID: workspaceID, Name: name, Status: "not_found", Endpoint: d.Endpoint(workspaceID)}, nil
	}
	if err != nil {
		return AgentPodInfo{}, err
	}
	var out struct {
		Config struct {
			Image string `json:"Image"`
		} `json:"Config"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		NetworkSettings struct {
			Networks map[string]any `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return AgentPodInfo{}, err
	}
	network := d.config.DockerNetwork
	for key := range out.NetworkSettings.Networks {
		network = key
		break
	}
	return AgentPodInfo{WorkspaceID: workspaceID, Name: name, Image: out.Config.Image, Network: network, Status: out.State.Status, Endpoint: d.Endpoint(workspaceID)}, nil
}

func (d *DockerAgentPodDriver) Endpoint(workspaceID string) string {
	return fmt.Sprintf("http://%s:3001", podName(workspaceID))
}

func (d *DockerAgentPodDriver) Logs(ctx context.Context, workspaceID string, tail string) ([]byte, error) {
	if tail == "" {
		tail = "100"
	}
	payload, _, err := d.client.do(ctx, http.MethodGet, "/containers/"+escapePathPart(podName(workspaceID))+"/logs?stdout=1&stderr=1&tail="+tail, nil, http.StatusOK)
	return payload, err
}

func (d *DockerAgentPodDriver) ensureNetwork(ctx context.Context, network string) error {
	body := map[string]any{"Name": network, "CheckDuplicate": true, "Driver": "bridge"}
	_, _, err := d.client.do(ctx, http.MethodPost, "/networks/create", body, http.StatusCreated, http.StatusConflict)
	if err != nil && !strings.Contains(err.Error(), "409") {
		return err
	}
	return nil
}

func createContainerBody(spec AgentPodSpec, name string) map[string]any {
	return map[string]any{
		"Image": spec.Image,
		"Env": []string{
			"WORKSPACE_ID=" + spec.WorkspaceID,
			"RUNTIME_ID=pi-agent",
			"CONTROL_PLANE_URL=http://agenthub-control-plane:3000",
			"AGENTHUB_INTERNAL_TOKEN=" + spec.Token,
			"WORKSPACE_DIR=/workspace",
		},
		"Labels": map[string]string{
			"agenthub.workspace_id": spec.WorkspaceID,
			"agenthub.kind":         "agent-pod",
		},
		"ExposedPorts": map[string]any{"3001/tcp": map[string]any{}},
		"HostConfig": map[string]any{
			"Binds":       []string{spec.WorkspacePath + ":/workspace"},
			"NetworkMode": spec.Network,
		},
		"NetworkingConfig": map[string]any{
			"EndpointsConfig": map[string]any{
				spec.Network: map[string]any{"Aliases": []string{name}},
			},
		},
	}
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

var idPattern = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

func safeID(value string) string {
	cleaned := idPattern.ReplaceAllString(value, "-")
	cleaned = strings.Trim(cleaned, "-._")
	if cleaned == "" {
		return "workspace"
	}
	return cleaned
}

func podName(workspaceID string) string {
	return "agent-pod-" + safeID(workspaceID)
}
