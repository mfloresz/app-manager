package events

import (
	"encoding/json"
	"fmt"
	"sync"
)

// maxHistory caps the retained log events per repo. It matches the
// per-card console line limit in the dashboard.
const maxHistory = 500

// Broker manages SSE client connections and event broadcasting.
// It also retains the last maxHistory log events per repo so the
// dashboard can restore a service's log after a page reload.
type Broker struct {
	mu       sync.Mutex
	channels map[string]map[chan string]struct{} // repoID -> set of channels
	history  map[string][]SSEEvent              // repoID -> retained log events
}

// NewBroker creates a new SSE broker.
func NewBroker() *Broker {
	return &Broker{
		channels: make(map[string]map[chan string]struct{}),
		history:  make(map[string][]SSEEvent),
	}
}

// Subscribe creates a new channel for a repoID (or "_global").
func (b *Broker) Subscribe(repoID string) chan string {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan string, 256)
	if b.channels[repoID] == nil {
		b.channels[repoID] = make(map[chan string]struct{})
	}
	b.channels[repoID][ch] = struct{}{}
	return ch
}

// Unsubscribe removes a channel.
func (b *Broker) Unsubscribe(repoID string, ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if chans, ok := b.channels[repoID]; ok {
		delete(chans, ch)
		if len(chans) == 0 {
			delete(b.channels, repoID)
		}
	}
}

// Emit sends a structured SSEEvent to repo-specific and global subscribers.
// Log-like events (log, error, app_output) are also retained per repo.
func (b *Broker) Emit(evt SSEEvent) {
	data, _ := json.Marshal(evt)
	msg := string(data)

	b.mu.Lock()
	defer b.mu.Unlock()

	switch evt.Type {
	case EventLog, EventError, EventAppOutput:
		h := append(b.history[evt.RepoID], evt)
		if len(h) > maxHistory {
			h = h[len(h)-maxHistory:]
		}
		b.history[evt.RepoID] = h
	}

	// Repo-specific
	if chans, ok := b.channels[evt.RepoID]; ok {
		for ch := range chans {
			select {
			case ch <- msg:
			default:
			}
		}
	}

	// Global
	if chans, ok := b.channels["_global"]; ok {
		for ch := range chans {
			select {
			case ch <- msg:
			default:
			}
		}
	}
}

// EmitLog sends a log event and also prints it.
func (b *Broker) EmitLog(repoID, msg string) {
	evt := NewLog(repoID, msg)
	fmt.Println(evt.Timestamp + " " + msg)
	b.Emit(evt)
}

// EmitError sends an error event and also prints to stderr.
func (b *Broker) EmitError(repoID, msg string) {
	evt := NewError(repoID, msg)
	fmt.Println("ERROR: " + msg)
	b.Emit(evt)
}

// Snapshot returns a copy of the log events retained for a repoID.
func (b *Broker) Snapshot(repoID string) []SSEEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	h := b.history[repoID]
	out := make([]SSEEvent, len(h))
	copy(out, h)
	return out
}

// Clear discards the log events retained for a repoID.
func (b *Broker) Clear(repoID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.history, repoID)
}
