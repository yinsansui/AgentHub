package process

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"agenthub/pkg/protocol"
)

const defaultAbortWait = 5 * time.Second

type Adapter struct {
	Command string
	WorkDir string

	mu       sync.Mutex
	sessions map[string]*sessionProcess
}

type sessionProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan protocol.UniversalEvent
	done   chan error

	writeMu sync.Mutex
}

type bridgeCommand struct {
	Type        string `json:"type"`
	WorkspaceID string `json:"workspaceId,omitempty"`
	TaskID      string `json:"taskId,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	RunID       string `json:"runId,omitempty"`
	CWD         string `json:"cwd,omitempty"`
	Message     string `json:"message,omitempty"`
	Source      string `json:"source,omitempty"`
}

func NewAdapter(command, workDir string) *Adapter {
	return &Adapter{Command: command, WorkDir: workDir, sessions: map[string]*sessionProcess{}}
}

func (a *Adapter) RunTurn(ctx context.Context, req protocol.TurnRequest, emit func(protocol.UniversalEvent) error) error {
	if strings.TrimSpace(a.Command) == "" {
		return errors.New("runtime process command is required")
	}
	proc, err := a.session(req)
	if err != nil {
		return err
	}
	if err := proc.send(bridgeCommand{Type: "run", WorkspaceID: req.WorkspaceID, TaskID: req.TaskID, SessionID: req.SessionID, RunID: req.RunID, CWD: req.SessionCWD, Message: req.Message, Source: req.Source}); err != nil {
		a.drop(req.SessionID, proc)
		return err
	}
	for {
		select {
		case event := <-proc.events:
			if event.RunID != "" && event.RunID != req.RunID {
				continue
			}
			if err := emit(event); err != nil {
				return err
			}
			if event.Type == protocol.EventSessionEnded {
				return nil
			}
		case err := <-proc.done:
			a.drop(req.SessionID, proc)
			if err == nil {
				return io.EOF
			}
			return err
		case <-ctx.Done():
			_ = proc.send(bridgeCommand{Type: "abort", RunID: req.RunID})
			return a.waitAfterAbort(proc, req, emit, ctx.Err())
		}
	}
}

func (a *Adapter) waitAfterAbort(proc *sessionProcess, req protocol.TurnRequest, emit func(protocol.UniversalEvent) error, fallback error) error {
	timer := time.NewTimer(defaultAbortWait)
	defer timer.Stop()
	for {
		select {
		case event := <-proc.events:
			if event.RunID != "" && event.RunID != req.RunID {
				continue
			}
			if err := emit(event); err != nil {
				return err
			}
			if event.Type == protocol.EventSessionEnded {
				return fallback
			}
		case err := <-proc.done:
			if err != nil {
				return err
			}
			return fallback
		case <-timer.C:
			return fallback
		}
	}
}

func (a *Adapter) session(req protocol.TurnRequest) (*sessionProcess, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if proc, ok := a.sessions[req.SessionID]; ok {
		return proc, nil
	}
	proc, err := a.startProcess(req.SessionCWD)
	if err != nil {
		return nil, err
	}
	a.sessions[req.SessionID] = proc
	return proc, nil
}

func (a *Adapter) drop(sessionID string, proc *sessionProcess) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if current, ok := a.sessions[sessionID]; ok && current == proc {
		delete(a.sessions, sessionID)
	}
}

func (a *Adapter) startProcess(cwd string) (*sessionProcess, error) {
	parts := strings.Fields(a.Command)
	if len(parts) == 0 {
		return nil, errors.New("runtime process command is required")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = strings.TrimSpace(cwd)
	if cmd.Dir == "" {
		cmd.Dir = a.WorkDir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	proc := &sessionProcess{cmd: cmd, stdin: stdin, events: make(chan protocol.UniversalEvent, 256), done: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go proc.readStdout(stdout)
	go copyStderr(stderr)
	go func() {
		proc.done <- cmd.Wait()
		close(proc.done)
	}()
	return proc, nil
}

func (p *sessionProcess) send(command bridgeCommand) error {
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if _, err := p.stdin.Write(append(payload, '\n')); err != nil {
		return err
	}
	return nil
}

func (p *sessionProcess) readStdout(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event protocol.UniversalEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			fmt.Fprintf(os.Stderr, "agenthub runtime host emitted invalid json: %s\n", line)
			continue
		}
		p.events <- event
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "agenthub runtime host stdout error: %v\n", err)
	}
}

func copyStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		fmt.Fprintf(os.Stderr, "[runtime-host] %s\n", scanner.Text())
	}
}
