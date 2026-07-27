package events

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Broker manages SSE client connections and event broadcasting.
type Broker struct {
	mu       sync.Mutex
	channels map[string]map[chan string]struct{} // repoID -> set of channels
}

// NewBroker creates a new SSE broker.
func NewBroker() *Broker {
	return &Broker{
		channels: make(map[string]map[chan string]struct{}),
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
func (b *Broker) Emit(evt SSEEvent) {
	data, _ := json.Marshal(evt)
	msg := string(data)

	b.mu.Lock()
	defer b.mu.Unlock()

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
