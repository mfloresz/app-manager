package process

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"ap-manager/internal/events"
)

// TestStartWithCaptureEmitsStoppedOnExit verifies that when a captured
// process exits on its own (crash), the manager announces service_run=stopped
// through the broker so the dashboard reacts without waiting for the poll.
func TestStartWithCaptureEmitsStoppedOnExit(t *testing.T) {
	if _, err := lookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	pm := NewManager(dir)
	broker := events.NewBroker()
	ch := broker.Subscribe("test/exit")
	defer broker.Unsubscribe("test/exit", ch)

	if _, err := pm.StartWithCapture("sh", "test/exit", filepath.Join("/bin", "sh"), broker, "-c", "echo bye; exit 0"); err != nil {
		t.Fatalf("StartWithCapture: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-ch:
			var evt events.SSEEvent
			if err := json.Unmarshal([]byte(msg), &evt); err != nil {
				t.Fatalf("bad event: %v", err)
			}
			if evt.Type == events.EventStatus && evt.ServiceRun == "stopped" {
				if pm.IsRunning("test/exit") {
					t.Error("PID file still present after exit announcement")
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for service_run=stopped after process exit")
		}
	}
}
