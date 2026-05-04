package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type dockerClient struct {
	httpClient *http.Client
	host       string
}

func newDockerClient(socketPath string) *dockerClient {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &dockerClient{httpClient: &http.Client{Transport: transport}, host: "http://docker"}
}

func (c *dockerClient) do(ctx context.Context, method, path string, body any, expected ...int) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(payload)
	}
	requestURL := c.host + path
	req, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	payload, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	if len(expected) == 0 {
		expected = []int{http.StatusOK}
	}
	for _, code := range expected {
		if resp.StatusCode == code {
			return payload, resp.StatusCode, nil
		}
	}
	return payload, resp.StatusCode, fmt.Errorf("docker %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(payload)))
}

func escapePathPart(value string) string {
	return url.PathEscape(value)
}
