package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gastownhall/gascity/internal/events"
)

// mailListIDs fetches GET /mail with the given query and returns the listed
// message IDs, failing the test on a non-200.
func mailListIDs(t *testing.T, h http.Handler, state *fakeState, query string) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", cityURL(state, "/mail?"+query), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /mail?%s = %d, body: %s", query, rec.Code, rec.Body.String())
	}
	var body MailListBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode /mail?%s: %v", query, err)
	}
	if body.Total != len(body.Items) {
		t.Errorf("/mail?%s total = %d, want %d (all items on one page)", query, body.Total, len(body.Items))
	}
	ids := make([]string, len(body.Items))
	for i, msg := range body.Items {
		ids[i] = msg.ID
	}
	return ids
}

// mailGetRaw fetches GET /mail/{id} and returns the status code and the body
// decoded field by field, so a test can assert which fields are present.
func mailGetRaw(t *testing.T, h http.Handler, state *fakeState, id string) (int, map[string]json.RawMessage) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", cityURL(state, "/mail/")+id, nil))
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&fields); err != nil {
		t.Fatalf("decode GET /mail/%s: %v", id, err)
	}
	return rec.Code, fields
}

func mailPost(t *testing.T, h http.Handler, state *fakeState, id, action string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newPostRequest(cityURL(state, "/mail/")+id+"/"+action, nil))
	return rec
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(got))
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		if seen[id] == 0 {
			return false
		}
		seen[id]--
	}
	return true
}

func TestMailListStatusFiltersServeClosedMail(t *testing.T) {
	state := newFakeState(t)
	mp := state.cityMailProv
	open, err := mp.Send("mayor", "worker", "Open", "stays in the inbox")
	if err != nil {
		t.Fatalf("Send open: %v", err)
	}
	closed, err := mp.Send("mayor", "worker", "Closed", "gets archived")
	if err != nil {
		t.Fatalf("Send closed: %v", err)
	}
	h := newTestCityHandler(t, state)
	if rec := mailPost(t, h, state, closed.ID, "archive"); rec.Code != http.StatusOK {
		t.Fatalf("archive = %d, body: %s", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct {
		status string
		want   []string
	}{
		{"unread", []string{open.ID}},
		{"all", []string{open.ID}},
		{"closed", []string{closed.ID}},
		{"any", []string{open.ID, closed.ID}},
	} {
		for _, scope := range []string{"", "&rig=myrig"} {
			got := mailListIDs(t, h, state, "agent=worker&status="+tc.status+scope)
			if !sameIDs(got, tc.want) {
				t.Errorf("status=%s%s listed %v, want %v", tc.status, scope, got, tc.want)
			}
		}
	}
}

func TestMailListStatusClosedPaginates(t *testing.T) {
	state := newFakeState(t)
	mp := state.cityMailProv
	var archived []string
	for i := 0; i < 3; i++ {
		msg, err := mp.Send("mayor", "worker", "Page "+strconv.Itoa(i), "archived")
		if err != nil {
			t.Fatalf("Send %d: %v", i, err)
		}
		if err := mp.Archive(msg.ID); err != nil {
			t.Fatalf("Archive %d: %v", i, err)
		}
		archived = append(archived, msg.ID)
	}
	h := newTestCityHandler(t, state)

	var seen []string
	cursor := ""
	for page := 0; page < 3; page++ {
		query := "agent=worker&status=closed&limit=2"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", cityURL(state, "/mail?"+query), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d = %d, body: %s", page, rec.Code, rec.Body.String())
		}
		var body MailListBody
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode page %d: %v", page, err)
		}
		if body.Total != len(archived) {
			t.Errorf("page %d total = %d, want %d", page, body.Total, len(archived))
		}
		for _, msg := range body.Items {
			seen = append(seen, msg.ID)
		}
		cursor = body.NextCursor
		if cursor == "" {
			break
		}
	}
	if !sameIDs(seen, archived) {
		t.Errorf("paged closed listing = %v, want %v", seen, archived)
	}
}

func TestMailMessagesCarryStatus(t *testing.T) {
	state := newFakeState(t)
	h := newTestCityHandler(t, state)
	sent, err := state.cityMailProv.Send("mayor", "worker", "Status", "check the fields")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	code, fields := mailGetRaw(t, h, state, sent.ID)
	if code != http.StatusOK {
		t.Fatalf("GET open = %d", code)
	}
	if string(fields["status"]) != `"open"` {
		t.Errorf("open message status = %s, want \"open\"", fields["status"])
	}
	if _, ok := fields["closed_at"]; ok {
		t.Errorf("open message carries closed_at = %s, want it absent", fields["closed_at"])
	}

	if rec := mailPost(t, h, state, sent.ID, "archive"); rec.Code != http.StatusOK {
		t.Fatalf("archive = %d, body: %s", rec.Code, rec.Body.String())
	}
	code, fields = mailGetRaw(t, h, state, sent.ID)
	if code != http.StatusOK {
		t.Fatalf("GET archived = %d, want 200 (archive keeps the message)", code)
	}
	if string(fields["status"]) != `"closed"` {
		t.Errorf("archived message status = %s, want \"closed\"", fields["status"])
	}
	if _, ok := fields["closed_at"]; !ok {
		t.Error("archived message has no closed_at")
	}
	var body string
	if err := json.Unmarshal(fields["body"], &body); err != nil || body != "check the fields" {
		t.Errorf("archived message body = %s, want the original body", fields["body"])
	}
}

func TestMailArchiveUnarchiveDeleteLifecycle(t *testing.T) {
	state := newFakeState(t)
	h := newTestCityHandler(t, state)
	sent, err := state.cityMailProv.Send("mayor", "worker", "Lifecycle", "archive, unarchive, delete")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if rec := mailPost(t, h, state, sent.ID, "archive"); rec.Code != http.StatusOK {
		t.Fatalf("archive = %d, body: %s", rec.Code, rec.Body.String())
	}
	if rec := mailPost(t, h, state, sent.ID, "archive"); rec.Code != http.StatusOK {
		t.Fatalf("repeat archive = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	rec := mailPost(t, h, state, sent.ID, "unarchive")
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive = %d, body: %s", rec.Code, rec.Body.String())
	}
	var ok OKResponse
	if err := json.NewDecoder(rec.Body).Decode(&ok.Body); err != nil || ok.Body.Status != "unarchived" {
		t.Errorf("unarchive body status = %q (err %v), want \"unarchived\"", ok.Body.Status, err)
	}
	_, fields := mailGetRaw(t, h, state, sent.ID)
	if string(fields["status"]) != `"open"` {
		t.Errorf("status after unarchive = %s, want \"open\"", fields["status"])
	}
	if got := mailListIDs(t, h, state, "agent=worker"); !sameIDs(got, []string{sent.ID}) {
		t.Errorf("inbox after unarchive = %v, want [%s]", got, sent.ID)
	}
	if rec := mailPost(t, h, state, sent.ID, "unarchive"); rec.Code != http.StatusOK {
		t.Errorf("unarchive of an open message = %d, want 200", rec.Code)
	}

	del := httptest.NewRequest("DELETE", cityURL(state, "/mail/")+sent.ID, nil)
	del.Header.Set("X-GC-Request", "true")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, del)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, body: %s", rec.Code, rec.Body.String())
	}
	if code, _ := mailGetRaw(t, h, state, sent.ID); code != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", code)
	}
	if got := mailListIDs(t, h, state, "agent=worker&status=any"); len(got) != 0 {
		t.Errorf("status=any after delete = %v, want none", got)
	}
	if rec := mailPost(t, h, state, sent.ID, "unarchive"); rec.Code != http.StatusNotFound {
		t.Errorf("unarchive after delete = %d, want 404", rec.Code)
	}
}

func TestMailUnarchiveUnknownIs404(t *testing.T) {
	state := newFakeState(t)
	h := newTestCityHandler(t, state)
	if rec := mailPost(t, h, state, "nonexistent", "unarchive"); rec.Code != http.StatusNotFound {
		t.Errorf("unarchive nonexistent = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
}

func TestMailLifecycleChangesAdvanceIndexAndEmitEvents(t *testing.T) {
	state := newFakeState(t)
	h := newTestCityHandler(t, state)
	ep := state.eventProv.(*events.Fake)
	sent, err := state.cityMailProv.Send("mayor", "worker", "Events", "announce every change")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	indexOf := func() uint64 {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", cityURL(state, "/mail?agent=worker&status=any"), nil))
		index, err := strconv.ParseUint(rec.Header().Get("X-GC-Index"), 10, 64)
		if err != nil {
			t.Fatalf("X-GC-Index %q: %v", rec.Header().Get("X-GC-Index"), err)
		}
		return index
	}

	steps := []struct {
		name      string
		do        func() int
		wantEvent string
	}{
		{"archive", func() int { return mailPost(t, h, state, sent.ID, "archive").Code }, events.MailArchived},
		{"unarchive", func() int { return mailPost(t, h, state, sent.ID, "unarchive").Code }, events.MailUnarchived},
		{"delete", func() int {
			req := httptest.NewRequest("DELETE", cityURL(state, "/mail/")+sent.ID, nil)
			req.Header.Set("X-GC-Request", "true")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return rec.Code
		}, events.MailDeleted},
	}
	for _, step := range steps {
		before := indexOf()
		if code := step.do(); code != http.StatusOK {
			t.Fatalf("%s = %d", step.name, code)
		}
		if after := indexOf(); after <= before {
			t.Errorf("%s left the index at %d, want it past %d", step.name, after, before)
		}
		last := ep.Events[len(ep.Events)-1]
		if last.Type != step.wantEvent || last.Subject != sent.ID {
			t.Errorf("%s recorded {%q %q}, want {%q %q}", step.name, last.Type, last.Subject, step.wantEvent, sent.ID)
		}
	}
}

// TestMailUnarchiveOpenMessageEmitsNothing pins that reopening a message that
// is already open is a no-op on the event log, so a client does not re-sync a
// message that did not change.
func TestMailUnarchiveOpenMessageEmitsNothing(t *testing.T) {
	state := newFakeState(t)
	h := newTestCityHandler(t, state)
	ep := state.eventProv.(*events.Fake)
	sent, err := state.cityMailProv.Send("mayor", "worker", "Open", "never archived")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	before := len(ep.Events)
	if rec := mailPost(t, h, state, sent.ID, "unarchive"); rec.Code != http.StatusOK {
		t.Fatalf("unarchive = %d", rec.Code)
	}
	for _, e := range ep.Events[before:] {
		if e.Type == events.MailUnarchived {
			t.Errorf("unarchive of an open message recorded %q", e.Type)
		}
	}
}
