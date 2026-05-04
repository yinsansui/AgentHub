package sse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"agenthub/internal/protocol"
)

func WriteEvent(w http.ResponseWriter, event protocol.UniversalEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event.Type); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func SetHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

func ReadEvents(r io.Reader, handle func(protocol.UniversalEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var data bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				var event protocol.UniversalEvent
				if err := json.Unmarshal(data.Bytes(), &event); err != nil {
					return err
				}
				if err := handle(event); err != nil {
					return err
				}
				data.Reset()
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			chunk := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(chunk)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if data.Len() > 0 {
		var event protocol.UniversalEvent
		if err := json.Unmarshal(data.Bytes(), &event); err != nil {
			return err
		}
		return handle(event)
	}
	return nil
}
