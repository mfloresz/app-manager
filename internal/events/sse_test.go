package events

import (
	"fmt"
	"testing"
)

// TestBrokerHistoryRingAndClear verifies the broker retains only the last
// maxHistory log-like events per repo and that Clear discards them.
func TestBrokerHistoryRingAndClear(t *testing.T) {
	b := NewBroker()

	total := maxHistory + 50
	for i := 0; i < total; i++ {
		b.Emit(NewLog("a/b", fmt.Sprintf("line-%d", i)))
	}
	// Non-log events must not be retained.
	b.Emit(NewProgress("a/b", "download", 50))

	got := b.Snapshot("a/b")
	if len(got) != maxHistory {
		t.Fatalf("len(Snapshot) = %d, want %d", len(got), maxHistory)
	}
	wantOldest := fmt.Sprintf("line-%d", total-maxHistory)
	if got[0].Message != wantOldest {
		t.Errorf("oldest retained = %q, want %q", got[0].Message, wantOldest)
	}
	if last := got[len(got)-1]; last.Message != fmt.Sprintf("line-%d", total-1) || last.Type == EventProgress {
		t.Errorf("newest retained = %+v, want line-%d (not progress)", last, total-1)
	}

	// Snapshot returns a copy: mutating it must not affect the broker.
	got[0].Message = "mutated"
	if again := b.Snapshot("a/b"); again[0].Message == "mutated" {
		t.Error("Snapshot did not return a copy")
	}

	// A different repo keeps its own history, including app output.
	b.Emit(NewAppOutput("c/d", "out", false))
	if got := b.Snapshot("c/d"); len(got) != 1 || got[0].Type != EventAppOutput {
		t.Errorf("Snapshot(c/d) = %+v, want 1 app_output event", got)
	}

	b.Clear("a/b")
	if got := b.Snapshot("a/b"); len(got) != 0 {
		t.Errorf("len(Snapshot) after Clear = %d, want 0", len(got))
	}
	if got := b.Snapshot("c/d"); len(got) != 1 {
		t.Errorf("Clear(a/b) must not touch c/d, got %d events", len(got))
	}
}

// TestBrokerAppOutputAndManagerLogs verifies the log separation contract:
// SnapshotAppOutput keeps only service stdout/stderr, and ClearManagerLogs
// drops manager/updater logs while preserving app output.
func TestBrokerAppOutputAndManagerLogs(t *testing.T) {
	b := NewBroker()

	b.Emit(NewLog("a/b", ">>> ACTUALIZANDO a/b <<<"))
	b.Emit(NewAppOutput("a/b", "service line 1", false))
	b.Emit(NewError("a/b", "boom"))
	b.Emit(NewAppOutput("a/b", "service line 2", true))
	b.Emit(NewLog("_system", "Repositorio añadido: a/b"))

	got := b.SnapshotAppOutput("a/b")
	if len(got) != 2 {
		t.Fatalf("len(SnapshotAppOutput) = %d, want 2", len(got))
	}
	for _, evt := range got {
		if evt.Type != EventAppOutput {
			t.Errorf("SnapshotAppOutput returned type %q, want app_output", evt.Type)
		}
	}

	b.ClearManagerLogs()

	if got := b.SnapshotAppOutput("a/b"); len(got) != 2 {
		t.Errorf("ClearManagerLogs must preserve app output, got %d events", len(got))
	}
	if got := b.Snapshot("_system"); len(got) != 0 {
		t.Errorf("ClearManagerLogs must drop _system logs, got %d events", len(got))
	}
	full := b.Snapshot("a/b")
	for _, evt := range full {
		if evt.Type == EventLog || evt.Type == EventError {
			t.Errorf("ClearManagerLogs left a %q event: %+v", evt.Type, evt)
		}
	}
}
