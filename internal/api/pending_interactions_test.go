package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gastownhall/gascity/internal/pendingwatch"
	"github.com/gastownhall/gascity/internal/runtime"
)

// pokeRecordingState is a State whose controller records every
// pending-interaction poke the API sends it.
type pokeRecordingState struct {
	*fakeState
	mu    sync.Mutex
	pokes []string
}

func (s *pokeRecordingState) PokePendingInteractions(sessionID, resolvedBy string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pokes = append(s.pokes, sessionID+" "+resolvedBy)
}

func (s *pokeRecordingState) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.pokes...)
}

func TestSessionRespondReportsTheRespondAnswerPath(t *testing.T) {
	fs := newSessionFakeState(t)
	state := &pokeRecordingState{fakeState: fs}
	h := newTestCityHandlerWith(t, state, New(state))

	info := createTestSession(t, fs.cityBeadStore, fs.sp, "Interactive")
	fs.sp.SetPendingInteraction(info.SessionName, &runtime.PendingInteraction{RequestID: "req-1", Kind: "approval"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newPostRequest(cityURL(fs, "/session/")+info.ID+"/respond", strings.NewReader(`{"action":"approve"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("respond status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if got, want := state.recorded(), []string{info.ID + " " + pendingwatch.ResolvedByRespond}; !slices.Equal(got, want) {
		t.Fatalf("pokes = %q, want %q", got, want)
	}

	// Nothing pending any more: the failed respond answers nothing.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, newPostRequest(cityURL(fs, "/session/")+info.ID+"/respond", strings.NewReader(`{"action":"approve"}`)))
	if rec.Code == http.StatusAccepted {
		t.Fatalf("second respond status = %d, want a failure", rec.Code)
	}
	if got := state.recorded(); len(got) != 1 {
		t.Fatalf("pokes after a failed respond = %q, want only the first", got)
	}
}

func TestSessionMessageReportsTheMessageAnswerPath(t *testing.T) {
	fs := newSessionFakeState(t)
	state := &pokeRecordingState{fakeState: fs}
	h := newTestCityHandlerWith(t, state, New(state))

	info := createTestSession(t, fs.cityBeadStore, fs.sp, "Interactive")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newPostRequest(cityURL(fs, "/session/")+info.ID+"/messages", strings.NewReader(`{"message":"use option 2"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("message status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	accepted := decodeAsyncAccepted(t, rec.Body)
	if success, failure := waitForSessionMessageResult(t, fs.eventProv, accepted.RequestID); success == nil {
		t.Fatalf("session message failed: %s: %s", failure.ErrorCode, failure.ErrorMessage)
	}
	if got, want := state.recorded(), []string{info.ID + " " + pendingwatch.ResolvedByMessage}; !slices.Equal(got, want) {
		t.Fatalf("pokes = %q, want %q", got, want)
	}
}
