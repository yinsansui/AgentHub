package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agenthub/internal/protocol"
	"agenthub/internal/sse"
)

type AgentPodClient struct {
	template string
	client   *http.Client
}

func NewAgentPodClient(template string) *AgentPodClient {
	return &AgentPodClient{template: template, client: http.DefaultClient}
}

func (c *AgentPodClient) Endpoint(workspaceID string) string {
	return strings.ReplaceAll(c.template, "{workspaceId}", safeID(workspaceID))
}

func (c *AgentPodClient) Turn(ctx context.Context, workspaceID, token string, turn protocol.TurnRequest, handle func(protocol.UniversalEvent) error) error {
	payload, err := json.Marshal(turn)
	if err != nil {
		return err
	}
	url := c.Endpoint(workspaceID) + "/turn"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("agent-pod turn returned %s", resp.Status)
	}
	return sse.ReadEvents(resp.Body, handle)
}
