package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"agenthub/pkg/protocol"
)

type Adapter interface {
	RunTurn(ctx context.Context, req protocol.TurnRequest, emit func(protocol.UniversalEvent) error) error
}

type StubPiAdapter struct{}

func (StubPiAdapter) RunTurn(ctx context.Context, req protocol.TurnRequest, emit func(protocol.UniversalEvent) error) error {
	itemID := "msg_" + req.RunID
	started := protocol.NewEvent(protocol.EventSessionStarted, req)
	started.Metadata = map[string]any{"adapter": "stub-pi"}
	if err := emit(started); err != nil {
		return err
	}
	item := protocol.NewEvent(protocol.EventItemStarted, req)
	item.ItemID = itemID
	item.Role = "assistant"
	item.Item = &protocol.UniversalItem{ID: itemID, Type: "message", Role: "assistant"}
	if err := emit(item); err != nil {
		return err
	}
	message := "Pi Agent adapter boundary is ready. Set PI_AGENT_COMMAND to stream real pi JSON output."
	for _, part := range []string{message} {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		delta := protocol.NewEvent(protocol.EventItemDelta, req)
		delta.ItemID = itemID
		delta.Role = "assistant"
		delta.Delta = part
		if err := emit(delta); err != nil {
			return err
		}
	}
	completed := protocol.NewEvent(protocol.EventItemCompleted, req)
	completed.ItemID = itemID
	completed.Role = "assistant"
	completed.Item = &protocol.UniversalItem{
		ID:      itemID,
		Type:    "message",
		Role:    "assistant",
		Content: []protocol.UniversalBlock{{Type: "text", Text: message}},
	}
	if err := emit(completed); err != nil {
		return err
	}
	ended := protocol.NewEvent(protocol.EventSessionEnded, req)
	ended.Metadata = map[string]any{"reason": "stop"}
	return emit(ended)
}

type PiCLIAdapter struct {
	Command string
	WorkDir string
}

func (a PiCLIAdapter) RunTurn(ctx context.Context, req protocol.TurnRequest, emit func(protocol.UniversalEvent) error) error {
	if strings.TrimSpace(a.Command) == "" {
		return StubPiAdapter{}.RunTurn(ctx, req, emit)
	}
	args := strings.Fields(a.Command)
	args = append(args, "--mode", "json", req.Message)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	workDir := strings.TrimSpace(req.SessionCWD)
	if workDir == "" {
		workDir = a.WorkDir
	}
	if workDir != "" {
		cmd.Dir = workDir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	itemID := "msg_" + req.RunID
	if err := emit(protocol.NewEvent(protocol.EventSessionStarted, req)); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	var assistantText strings.Builder
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		if err := mapPiJSONLine(req, itemID, scanner.Text(), &assistantText, emit); err != nil {
			_ = cmd.Process.Kill()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	if err := cmd.Wait(); err != nil {
		return err
	}
	ended := protocol.NewEvent(protocol.EventSessionEnded, req)
	ended.Metadata = map[string]any{"reason": "stop", "adapter": "pi-cli"}
	return emit(ended)
}

func mapPiJSONLine(req protocol.TurnRequest, itemID string, line string, assistantText *strings.Builder, emit func(protocol.UniversalEvent) error) error {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return fmt.Errorf("parse pi json line: %w", err)
	}
	typeName, _ := raw["type"].(string)
	switch typeName {
	case "message_update":
		assistantEvent, _ := raw["assistantMessageEvent"].(map[string]any)
		eventType, _ := assistantEvent["type"].(string)
		switch eventType {
		case "text_start":
			e := protocol.NewEvent(protocol.EventItemStarted, req)
			e.ItemID = itemID
			e.Role = "assistant"
			e.Item = &protocol.UniversalItem{ID: itemID, Type: "message", Role: "assistant"}
			return emit(e)
		case "text_delta":
			delta, _ := assistantEvent["delta"].(string)
			assistantText.WriteString(delta)
			e := protocol.NewEvent(protocol.EventItemDelta, req)
			e.ItemID = itemID
			e.Role = "assistant"
			e.Delta = delta
			return emit(e)
		case "error":
			e := protocol.NewEvent(protocol.EventError, req)
			e.Error = &protocol.EventErrorPayload{Message: fmt.Sprint(assistantEvent["error"])}
			return emit(e)
		}
	case "message_end":
		e := protocol.NewEvent(protocol.EventItemCompleted, req)
		e.ItemID = itemID
		e.Role = "assistant"
		e.Item = &protocol.UniversalItem{
			ID:      itemID,
			Type:    "message",
			Role:    "assistant",
			Content: []protocol.UniversalBlock{{Type: "text", Text: assistantText.String()}},
		}
		e.Metadata = map[string]any{"piEvent": raw, "completedAt": time.Now().UTC().Format(time.RFC3339Nano)}
		return emit(e)
	case "error":
		e := protocol.NewEvent(protocol.EventError, req)
		e.Error = &protocol.EventErrorPayload{Message: fmt.Sprint(raw["error"])}
		return emit(e)
	}
	return nil
}
