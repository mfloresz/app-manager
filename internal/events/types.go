// Package events defines structured event types for SSE communication.
package events

import "time"

// EventType represents a category of event.
type EventType string

const (
	EventLog       EventType = "log"
	EventVersion   EventType = "version"
	EventProgress  EventType = "progress"
	EventStatus    EventType = "status"
	EventUpdate    EventType = "update"
	EventError     EventType = "error"
	EventSystem    EventType = "system"
)

// SSEEvent is a structured event sent via SSE.
type SSEEvent struct {
	Type      EventType `json:"type"`
	RepoID    string    `json:"repo_id,omitempty"`
	Timestamp string    `json:"timestamp,omitempty"`
	Message   string    `json:"message,omitempty"`

	// Version fields
	Current string `json:"current,omitempty"`
	Latest  string `json:"latest,omitempty"`

	// Progress fields
	Step    string `json:"step,omitempty"`
	Percent int    `json:"percent,omitempty"`

	// Status fields
	Status     string `json:"status,omitempty"` // idle, checking, downloading, stopping, replacing, starting, verifying, completed, failed
	ServiceRun string `json:"service_run,omitempty"` // running, stopped

	// Error
	Error string `json:"error,omitempty"`
}

// Helper constructors
func NewLog(repoID, msg string) SSEEvent {
	return SSEEvent{
		Type:      EventLog,
		RepoID:    repoID,
		Timestamp: time.Now().Format("15:04:05"),
		Message:   msg,
	}
}

func NewVersion(repoID, current, latest string) SSEEvent {
	return SSEEvent{
		Type:      EventVersion,
		RepoID:    repoID,
		Timestamp: time.Now().Format("15:04:05"),
		Current:   current,
		Latest:    latest,
	}
}

func NewProgress(repoID, step string, percent int) SSEEvent {
	return SSEEvent{
		Type:      EventProgress,
		RepoID:    repoID,
		Timestamp: time.Now().Format("15:04:05"),
		Step:      step,
		Percent:   percent,
	}
}

func NewStatus(repoID, status string) SSEEvent {
	return SSEEvent{
		Type:      EventStatus,
		RepoID:    repoID,
		Timestamp: time.Now().Format("15:04:05"),
		Status:    status,
	}
}

func NewServiceStatus(repoID, serviceRun string) SSEEvent {
	return SSEEvent{
		Type:       EventStatus,
		RepoID:     repoID,
		Timestamp:  time.Now().Format("15:04:05"),
		ServiceRun: serviceRun,
	}
}

func NewUpdateComplete(repoID, version string) SSEEvent {
	return SSEEvent{
		Type:      EventUpdate,
		RepoID:    repoID,
		Timestamp: time.Now().Format("15:04:05"),
		Status:    "completed",
		Current:   version,
	}
}

func NewUpdateFailed(repoID, errMsg string) SSEEvent {
	return SSEEvent{
		Type:      EventUpdate,
		RepoID:    repoID,
		Timestamp: time.Now().Format("15:04:05"),
		Status:    "failed",
		Error:     errMsg,
	}
}

func NewError(repoID, msg string) SSEEvent {
	return SSEEvent{
		Type:      EventError,
		RepoID:    repoID,
		Timestamp: time.Now().Format("15:04:05"),
		Error:     msg,
		Message:   msg,
	}
}

func NewSystem(msg string) SSEEvent {
	return SSEEvent{
		Type:      EventSystem,
		RepoID:    "_system",
		Timestamp: time.Now().Format("15:04:05"),
		Message:   msg,
	}
}
