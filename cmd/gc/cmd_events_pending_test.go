package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/api"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/pendingwatch"
)

func pendingWireEvent(t *testing.T, seq int64, typ string, payload events.Payload) cliWireEvent {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", typ, err)
	}
	return cliWireEvent{
		Actor: pendingInteractionActor, Seq: seq, Subject: "s-1", SessionID: "s-1",
		Ts: time.Unix(1700000000+seq, 0).UTC(), Type: typ, Payload: raw,
	}
}

// The --type filter that `gc events --follow` and `--watch` share picks a
// session's answered interaction out of the stream, resolved_by included.
func TestDoEventsWatchTypeFilterSelectsTheAnsweredInteraction(t *testing.T) {
	items := []cliWireEvent{
		{Actor: "human", Seq: 1, Subject: "gc-1", Ts: time.Unix(1700000000, 0).UTC(), Type: events.BeadCreated},
		pendingWireEvent(t, 2, events.SessionPending, api.SessionPendingPayload{
			SessionID: "s-1", RequestID: "req-1", Kind: "approval", Prompt: "Run rm?",
		}),
		pendingWireEvent(t, 3, events.SessionPendingCleared, api.SessionPendingClearedPayload{
			SessionID: "s-1", RequestID: "req-1", Kind: "approval",
			Reason: pendingwatch.ReasonResolved, ResolvedBy: pendingwatch.ResolvedByRespond,
		}),
	}
	server := newEventsTestServer(t, testEventRoutes{
		cityEvents: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-GC-Index", "3")
			writeJSONResponse(t, w, cityEventsListResponse(t, items))
		},
	})
	defer server.Close()

	var stdout, stderr bytes.Buffer
	code := doEventsWatch(eventsAPIScope{apiURL: server.URL, cityName: "mc-city"}, events.SessionPendingCleared,
		map[string][]string{"resolved_by": {pendingwatch.ResolvedByRespond}}, 1, "", 50*time.Millisecond, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doEventsWatch = %d, want 0; stderr=%s", code, stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d JSON lines, want 1; output=%q", len(lines), stdout.String())
	}
	var got struct {
		Seq     int64                            `json:"seq"`
		Type    string                           `json:"type"`
		Subject string                           `json:"subject"`
		Payload api.SessionPendingClearedPayload `json:"payload"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", lines[0], err)
	}
	if got.Seq != 3 || got.Type != events.SessionPendingCleared || got.Subject != "s-1" ||
		got.Payload.RequestID != "req-1" || got.Payload.ResolvedBy != pendingwatch.ResolvedByRespond {
		t.Fatalf("event = %+v, want seq 3 session.pending_cleared for s-1 req-1 resolved_by respond", got)
	}
}
