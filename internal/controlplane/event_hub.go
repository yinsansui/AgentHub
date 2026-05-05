package controlplane

import "sync"

const sessionEventBufferSize = 256

type EventHub struct {
	mu       sync.RWMutex
	sessions map[string]map[chan StoredEvent]struct{}
}

func NewEventHub() *EventHub {
	return &EventHub{sessions: map[string]map[chan StoredEvent]struct{}{}}
}

func (h *EventHub) Subscribe(sessionID string) (<-chan StoredEvent, func()) {
	ch := make(chan StoredEvent, sessionEventBufferSize)
	h.mu.Lock()
	if h.sessions[sessionID] == nil {
		h.sessions[sessionID] = map[chan StoredEvent]struct{}{}
	}
	h.sessions[sessionID][ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.sessions[sessionID], ch)
		if len(h.sessions[sessionID]) == 0 {
			delete(h.sessions, sessionID)
		}
		close(ch)
	}
	return ch, unsubscribe
}

func (h *EventHub) Publish(event StoredEvent) {
	if event.Payload.SessionID == "" {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.sessions[event.Payload.SessionID] {
		select {
		case ch <- event:
		default:
		}
	}
}
