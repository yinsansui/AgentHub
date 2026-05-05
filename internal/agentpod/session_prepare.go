package agentpod

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agenthub/pkg/protocol"
)

func (s *Server) prepareSession(req protocol.PrepareSessionRequest) (string, error) {
	if err := validatePathSegment(req.TaskID, "taskId"); err != nil {
		return "", err
	}
	if err := validatePathSegment(req.SessionID, "sessionId"); err != nil {
		return "", err
	}
	taskDir := s.taskDir(req.TaskID)
	sessionDir := s.sessionDir(req.TaskID, req.SessionID)
	if err := os.MkdirAll(filepath.Join(taskDir, "docs"), 0o755); err != nil {
		return "", err
	}
	if err := ensureFile(filepath.Join(taskDir, "AGENTS.md"), "# AgentHub Task Instructions\n"); err != nil {
		return "", err
	}
	if err := ensureFile(filepath.Join(taskDir, "CLAUDE.md"), "# AgentHub Task Instructions\n"); err != nil {
		return "", err
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return "", err
	}
	if err := ensureSymlink(filepath.Join(sessionDir, "docs"), "../../docs"); err != nil {
		return "", err
	}
	if err := ensureSymlink(filepath.Join(sessionDir, "AGENTS.md"), "../../AGENTS.md"); err != nil {
		return "", err
	}
	if err := ensureSymlink(filepath.Join(sessionDir, "CLAUDE.md"), "../../CLAUDE.md"); err != nil {
		return "", err
	}
	if err := materializeSkills(filepath.Join(sessionDir, ".agents", "skills"), req.Skills); err != nil {
		return "", err
	}
	if err := materializeSkills(filepath.Join(sessionDir, ".claude", "skills"), req.Skills); err != nil {
		return "", err
	}
	if err := materializeMCPConfig(filepath.Join(sessionDir, ".agents", "mcp.json"), req.MCPServers); err != nil {
		return "", err
	}
	if err := writeSkillManifest(sessionDir, req.SessionID, req.Skills); err != nil {
		return "", err
	}
	if err := writeMCPManifest(sessionDir, req.SessionID, req.MCPServers); err != nil {
		return "", err
	}
	return sessionDir, nil
}

func (s *Server) taskDir(taskID string) string {
	return filepath.Join(s.workspaceDir, "tasks", taskID)
}

func (s *Server) sessionDir(taskID, sessionID string) string {
	return filepath.Join(s.taskDir(taskID), "sessions", sessionID)
}

func materializeSkills(root string, skills []protocol.ResolvedSkill) error {
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for _, skill := range skills {
		if err := validatePathSegment(skill.Slug, "skill slug"); err != nil {
			return err
		}
		skillDir := filepath.Join(root, skill.Slug)
		for _, file := range skill.Files {
			relativePath, err := cleanRelativePath(file.Path)
			if err != nil {
				return err
			}
			target := filepath.Join(skillDir, relativePath)
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(target, []byte(file.Content), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeSkillManifest(sessionDir, sessionID string, skills []protocol.ResolvedSkill) error {
	manifestDir := filepath.Join(sessionDir, ".agenthub")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(protocol.SkillManifest{
		SessionID: sessionID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Skills:    skills,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(manifestDir, "skills.manifest.json"), append(payload, '\n'), 0o644)
}

func materializeMCPConfig(path string, servers []protocol.ResolvedMCPServer) error {
	config := protocol.MCPConfig{MCPServers: map[string]protocol.MCPServerConfig{}}
	for _, server := range servers {
		if err := validatePathSegment(server.Name, "mcp server name"); err != nil {
			return err
		}
		if strings.TrimSpace(server.Command) == "" {
			return errors.New("mcp server command is required")
		}
		config.MCPServers[server.Name] = protocol.MCPServerConfig{
			Command:   server.Command,
			Args:      append([]string(nil), server.Args...),
			Transport: server.Transport,
			Env:       cloneStringMap(server.Env),
		}
	}
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o644)
}

func writeMCPManifest(sessionDir, sessionID string, servers []protocol.ResolvedMCPServer) error {
	manifestDir := filepath.Join(sessionDir, ".agenthub")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(struct {
		SessionID  string                       `json:"sessionId"`
		CreatedAt  string                       `json:"createdAt"`
		MCPServers []protocol.ResolvedMCPServer `json:"mcpServers"`
	}{
		SessionID:  sessionID,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		MCPServers: servers,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(manifestDir, "mcp.manifest.json"), append(payload, '\n'), 0o644)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func ensureFile(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func ensureSymlink(path, target string) error {
	if existing, err := os.Readlink(path); err == nil && existing == target {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return os.Symlink(target, path)
}

func validatePathSegment(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New(field + " is required")
	}
	if value == "." || value == ".." || strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return errors.New(field + " must be a safe path segment")
	}
	return nil
}

func cleanRelativePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("skill file path is required")
	}
	if filepath.IsAbs(path) {
		return "", errors.New("skill file path must be relative")
	}
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == ".." {
			return "", errors.New("skill file path must stay inside the skill directory")
		}
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		return "", errors.New("skill file path must stay inside the skill directory")
	}
	return cleaned, nil
}
