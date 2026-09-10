package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/worker"
)

// A turn must carry the entry ID the before/after cursors match on. Without it
// a client can see has_older_messages but has nothing to send as `before`.
func TestEntryToTurnCarriesEntryID(t *testing.T) {
	e := &worker.TranscriptEntry{
		UUID:      "11111111-2222-3333-4444-555555555555",
		Type:      "assistant",
		Timestamp: time.Now(),
		Message:   json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"hello"}]}`),
	}
	if turn := entryToTurn(e); turn.ID != e.UUID {
		t.Fatalf("turn.ID = %q, want %q", turn.ID, e.UUID)
	}
}

// A tool label alone says a tool ran but not what it did, which leaves a
// transcript full of results to questions that are never shown.
func TestEntryToTurnShowsToolCommand(t *testing.T) {
	e := &worker.TranscriptEntry{
		UUID: "abc",
		Type: "assistant",
		Message: json.RawMessage(`{"role":"assistant","content":[` +
			`{"type":"tool_use","id":"toolu_1","name":"Bash",` +
			`"input":{"command":"gc mail read hgc-wisp-9sfnjc4","description":"read mail"}}]}`),
	}
	turn := entryToTurn(e)
	if !strings.Contains(turn.Text, "gc mail read hgc-wisp-9sfnjc4") {
		t.Fatalf("turn.Text = %q, want it to carry the command", turn.Text)
	}
	if !strings.HasPrefix(turn.Text, "[Bash]") {
		t.Fatalf("turn.Text = %q, want the [Bash] label preserved", turn.Text)
	}
}

// A tool with no readable input keeps the bare label rather than gaining a
// trailing space or an empty bracket.
func TestEntryToTurnToolWithoutReadableInput(t *testing.T) {
	e := &worker.TranscriptEntry{
		UUID:    "abc",
		Type:    "assistant",
		Message: json.RawMessage(`{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"Monitor","input":{"opaque":123}}]}`),
	}
	if got := entryToTurn(e).Text; got != "[Monitor]" {
		t.Fatalf("turn.Text = %q, want exactly %q", got, "[Monitor]")
	}
}

// A turn is one line per part, so a multi-line command must not split into what
// would read as separate turns, and a long one must stay bounded.
func TestToolInvocationSuffixFlattensAndBounds(t *testing.T) {
	got := toolInvocationSuffix(json.RawMessage(`{"command":"line one\nline two\n   line three"}`))
	if strings.Contains(got, "\n") {
		t.Fatalf("suffix %q must not contain a newline", got)
	}
	if want := " line one line two line three"; got != want {
		t.Fatalf("suffix = %q, want %q", got, want)
	}

	bounded := toolInvocationSuffix(json.RawMessage(`{"command":"` + strings.Repeat("x", 900) + `"}`))
	if len(bounded) > 520 {
		t.Fatalf("suffix length = %d, want it bounded near 500", len(bounded))
	}
	if !strings.HasSuffix(bounded, "…") {
		t.Fatalf("bounded suffix should end with an ellipsis, got %q", bounded)
	}
}

// Thinking stays redacted. The reasoning text is signature-only in the provider
// log, so there is nothing to surface even if policy changed.
func TestEntryToTurnLeavesThinkingRedacted(t *testing.T) {
	e := &worker.TranscriptEntry{
		UUID:    "abc",
		Type:    "assistant",
		Message: json.RawMessage(`{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"CAIS..."}]}`),
	}
	if got := entryToTurn(e).Text; got != "[thinking]" {
		t.Fatalf("turn.Text = %q, want %q", got, "[thinking]")
	}
}
