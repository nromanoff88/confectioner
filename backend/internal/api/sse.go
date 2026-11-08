package api

import (
	"encoding/json"
	"sync"
)

type SSEEvent struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type SSEHub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[chan []byte]struct{}),
	}
}

func (h *SSEHub) Subscribe() chan []byte {
	ch := make(chan []byte, 32)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *SSEHub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

func (h *SSEHub) Broadcast(eventType string, payload any) {
	data, err := json.Marshal(SSEEvent{
		Type:    eventType,
		Payload: payload,
	})
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

