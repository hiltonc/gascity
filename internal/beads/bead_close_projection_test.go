package beads

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	beadslib "github.com/steveyegge/beads"
)

// closedBdShowJSON is the shape `bd show <id> --json` emits for a CLOSED bead:
// the close timestamp, the reason recorded with it, and the two attribution
// columns. Every field here is one bd already serves and gascity used to drop
// on the floor.
const closedBdShowJSON = `[{
	"id": "gc-closed",
	"title": "Closed bead",
	"status": "closed",
	"issue_type": "task",
	"priority": 1,
	"created_at": "2026-09-17T16:00:00Z",
	"updated_at": "2026-09-17T17:50:00Z",
	"closed_at": "2026-09-17T17:47:39Z",
	"close_reason": "shipped in #118",
	"owner": "me@heyhilton.com",
	"created_by": "gc-mayor"
}]`

// TestBdIssueCarriesCloseAndAttribution pins the bd decode half of the close
// projection: the four properties survive parseIssuesTolerant -> toBead. They
// were absent from bdIssue entirely, so a closed bead read through the bd CLI
// arrived at every caller with no completion time.
func TestBdIssueCarriesCloseAndAttribution(t *testing.T) {
	issues, err := parseIssuesTolerant([]byte(closedBdShowJSON))
	if err != nil {
		t.Fatalf("parseIssuesTolerant(): %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	b := issues[0].toBead()

	wantClosedAt := time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC)
	if b.ClosedAt == nil {
		t.Fatal("ClosedAt = nil, want the closed_at bd served")
	}
	if !b.ClosedAt.Equal(wantClosedAt) {
		t.Fatalf("ClosedAt = %s, want %s", b.ClosedAt, wantClosedAt)
	}
	if b.CloseReason != "shipped in #118" {
		t.Fatalf("CloseReason = %q, want %q", b.CloseReason, "shipped in #118")
	}
	if b.Owner != "me@heyhilton.com" {
		t.Fatalf("Owner = %q, want %q", b.Owner, "me@heyhilton.com")
	}
	if b.CreatedBy != "gc-mayor" {
		t.Fatalf("CreatedBy = %q, want %q", b.CreatedBy, "gc-mayor")
	}
}

// TestBdIssueLeavesClosedAtNilForAnOpenBead pins the open case at the decode
// seam. bd omits closed_at for a bead that is still open, and a pointer keeps
// that absence distinguishable from a zero time -- which a client would read
// as 1970 rather than as "not closed".
func TestBdIssueLeavesClosedAtNilForAnOpenBead(t *testing.T) {
	const openBdShowJSON = `[{
		"id": "gc-open",
		"title": "Open bead",
		"status": "open",
		"issue_type": "task",
		"created_at": "2026-09-17T16:00:00Z",
		"created_by": "gc-mayor"
	}]`
	issues, err := parseIssuesTolerant([]byte(openBdShowJSON))
	if err != nil {
		t.Fatalf("parseIssuesTolerant(): %v", err)
	}
	b := issues[0].toBead()

	if b.ClosedAt != nil {
		t.Fatalf("ClosedAt = %s, want nil for an open bead", b.ClosedAt)
	}
	if b.CloseReason != "" {
		t.Fatalf("CloseReason = %q, want empty for an open bead", b.CloseReason)
	}
	if b.CreatedBy != "gc-mayor" {
		t.Fatalf("CreatedBy = %q, want %q", b.CreatedBy, "gc-mayor")
	}
}

// TestBeadFromNativeIssueCarriesCloseAndAttribution pins the same projection
// on the native DoltLite store, which reads beadslib.Issue values directly
// rather than bd's JSON. Both seams must carry the four or the served shape
// depends on which backend the city happens to run.
func TestBeadFromNativeIssueCarriesCloseAndAttribution(t *testing.T) {
	closedAt := time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC)
	b, err := beadFromNativeIssue(&beadslib.Issue{
		ID:          "gc-closed",
		Title:       "Closed bead",
		Status:      beadslib.StatusClosed,
		IssueType:   beadslib.IssueType("task"),
		CreatedAt:   time.Date(2026, 9, 17, 16, 0, 0, 0, time.UTC),
		ClosedAt:    &closedAt,
		CloseReason: "shipped in #118",
		Owner:       "me@heyhilton.com",
		CreatedBy:   "gc-mayor",
	})
	if err != nil {
		t.Fatalf("beadFromNativeIssue(): %v", err)
	}
	if b.ClosedAt == nil {
		t.Fatal("ClosedAt = nil, want the issue's closed_at")
	}
	if !b.ClosedAt.Equal(closedAt) {
		t.Fatalf("ClosedAt = %s, want %s", b.ClosedAt, closedAt)
	}
	if b.CloseReason != "shipped in #118" {
		t.Fatalf("CloseReason = %q, want %q", b.CloseReason, "shipped in #118")
	}
	if b.Owner != "me@heyhilton.com" {
		t.Fatalf("Owner = %q, want %q", b.Owner, "me@heyhilton.com")
	}
	if b.CreatedBy != "gc-mayor" {
		t.Fatalf("CreatedBy = %q, want %q", b.CreatedBy, "gc-mayor")
	}
}

// TestBeadFromNativeIssueCarriesUpdatedAt pins updated_at on the native seam.
// The bd and DoltLite seams already carried it; the native one dropped it, so
// the API served no updated_at for any bead read through it (gsc-yncq).
func TestBeadFromNativeIssueCarriesUpdatedAt(t *testing.T) {
	created := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	updated := created.Add(5 * time.Minute)
	bead, err := beadFromNativeIssue(&beadslib.Issue{
		ID:        "gc-updated",
		Title:     "updated bead",
		Status:    beadslib.StatusOpen,
		IssueType: beadslib.TypeTask,
		Priority:  2,
		CreatedAt: created,
		UpdatedAt: updated,
	})
	if err != nil {
		t.Fatalf("beadFromNativeIssue: %v", err)
	}
	if !bead.UpdatedAt.Equal(updated) {
		t.Fatalf("UpdatedAt = %v, want %v", bead.UpdatedAt, updated)
	}
	if bead.UpdatedAt.Equal(bead.CreatedAt) {
		t.Fatal("UpdatedAt == CreatedAt; want distinct values when the issue was updated after creation")
	}
}

// TestBeadFromNativeIssueKeepsUpdatedAtDistinctFromClosedAt guards the pair a
// future change is most tempted to collapse. Merge bookkeeping touches a bead
// after it closes -- 25.7% of closed beads carry an updated_at later than their
// closed_at -- so neither field is an alias for the other (gsc-yncq).
func TestBeadFromNativeIssueKeepsUpdatedAtDistinctFromClosedAt(t *testing.T) {
	closedAt := time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC)
	updatedAt := time.Date(2026, 9, 17, 17, 50, 0, 0, time.UTC)
	b, err := beadFromNativeIssue(&beadslib.Issue{
		ID:        "gc-closed",
		Title:     "Closed bead",
		Status:    beadslib.StatusClosed,
		IssueType: beadslib.IssueType("task"),
		CreatedAt: time.Date(2026, 9, 17, 16, 0, 0, 0, time.UTC),
		UpdatedAt: updatedAt,
		ClosedAt:  &closedAt,
	})
	if err != nil {
		t.Fatalf("beadFromNativeIssue(): %v", err)
	}
	if b.ClosedAt == nil || !b.ClosedAt.Equal(closedAt) {
		t.Fatalf("ClosedAt = %v, want %s", b.ClosedAt, closedAt)
	}
	if !b.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("UpdatedAt = %s, want %s", b.UpdatedAt, updatedAt)
	}
	if b.ClosedAt.Equal(b.UpdatedAt) {
		t.Fatal("closed_at == updated_at; want both served as the distinct values the issue recorded")
	}
}

// TestBeadFromNativeIssueDoesNotAliasTheClosedAtPointer guards against the store
// handing out a pointer into the caller's beadslib.Issue, where a later write
// through that issue would mutate a bead already returned.
func TestBeadFromNativeIssueDoesNotAliasTheClosedAtPointer(t *testing.T) {
	closedAt := time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC)
	issue := &beadslib.Issue{
		ID:        "gc-closed",
		Status:    beadslib.StatusClosed,
		CreatedAt: time.Date(2026, 9, 17, 16, 0, 0, 0, time.UTC),
		ClosedAt:  &closedAt,
	}
	b, err := beadFromNativeIssue(issue)
	if err != nil {
		t.Fatalf("beadFromNativeIssue(): %v", err)
	}
	if b.ClosedAt == issue.ClosedAt {
		t.Fatal("ClosedAt aliases the source issue's pointer; want a copy")
	}
}

// TestOpenBeadOmitsClosedAtOnTheWire pins the marshaled shape. A pointer plus
// omitempty is what keeps an open bead's closed_at ABSENT rather than
// "0001-01-01T00:00:00Z", which a reporting client reads as a real date.
//
// omitempty is load-bearing beyond the null case: the same encoding carries
// bead cache events, where a property's absence means the patch does not carry
// the field.
func TestOpenBeadOmitsClosedAtOnTheWire(t *testing.T) {
	data, err := json.Marshal(Bead{ID: "gc-open", Status: "open"})
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	for _, key := range []string{"closed_at", "close_reason", "owner", "created_by"} {
		if v, ok := got[key]; ok {
			t.Errorf("%s = %v, want omitted for an open bead", key, v)
		}
	}
}

// TestClosedBeadCarriesTheFourOnTheWire is the positive half: once a store
// reports them, the marshaled bead serves all four under bd's own field names.
func TestClosedBeadCarriesTheFourOnTheWire(t *testing.T) {
	closedAt := time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC)
	data, err := json.Marshal(Bead{
		ID:          "gc-closed",
		Status:      "closed",
		ClosedAt:    &closedAt,
		CloseReason: "shipped in #118",
		Owner:       "me@heyhilton.com",
		CreatedBy:   "gc-mayor",
	})
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	want := map[string]any{
		"closed_at":    "2026-09-17T17:47:39Z",
		"close_reason": "shipped in #118",
		"owner":        "me@heyhilton.com",
		"created_by":   "gc-mayor",
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("%s = %v, want %v", key, got[key], wantValue)
		}
	}
}

// TestCachingStoreApplyEventMergesCloseAndAttribution pins the cache half of
// the projection. The caching store is what the API reads through on a live
// city, and its merge is a per-field whitelist -- a property the merge does not
// name is dropped on the floor no matter what the emitter sent, so a cached
// bead would keep serving no completion time however many events carried one.
func TestCachingStoreApplyEventMergesCloseAndAttribution(t *testing.T) {
	t.Parallel()

	backing := NewMemStore()
	created, err := backing.Create(Bead{Title: "event", Status: "open", Type: "task"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cache := NewCachingStoreForTest(backing, nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	// A full snapshot, which is what EncodeBeadEventPayload emits.
	cache.ApplyEvent("bead.updated", []byte(`{"id":"`+created.ID+`","title":"event",`+
		`"issue_type":"task","status":"open",`+
		`"closed_at":"2026-09-17T17:47:39Z","close_reason":"shipped in #118",`+
		`"owner":"me@heyhilton.com","created_by":"gc-mayor"}`))

	got, err := cache.Get(created.ID)
	if err != nil {
		t.Fatalf("Get after event: %v", err)
	}
	wantClosedAt := time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC)
	if got.ClosedAt == nil {
		t.Fatal("ClosedAt = nil after an event that carried one")
	}
	if !got.ClosedAt.Equal(wantClosedAt) {
		t.Fatalf("ClosedAt = %s, want %s", got.ClosedAt, wantClosedAt)
	}
	if got.CloseReason != "shipped in #118" {
		t.Fatalf("CloseReason = %q, want %q", got.CloseReason, "shipped in #118")
	}
	if got.Owner != "me@heyhilton.com" {
		t.Fatalf("Owner = %q, want %q", got.Owner, "me@heyhilton.com")
	}
	if got.CreatedBy != "gc-mayor" {
		t.Fatalf("CreatedBy = %q, want %q", got.CreatedBy, "gc-mayor")
	}
}

// TestCachingStoreApplyEventKeepsCloseDataAPatchOmits is the other half of the
// merge contract: a payload that does not carry a field must not blank the
// cached value. A partial patch -- a title edit, say -- arriving after a close
// would otherwise erase the completion time the close had just recorded.
func TestCachingStoreApplyEventKeepsCloseDataAPatchOmits(t *testing.T) {
	t.Parallel()

	closedAt := time.Date(2026, 9, 17, 17, 47, 39, 0, time.UTC)
	backing := NewMemStore()
	created, err := backing.Create(Bead{
		Title:       "event",
		Status:      "closed",
		Type:        "task",
		ClosedAt:    &closedAt,
		CloseReason: "shipped in #118",
		Owner:       "me@heyhilton.com",
		CreatedBy:   "gc-mayor",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cache := NewCachingStoreForTest(backing, nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	cache.ApplyEvent("bead.updated", []byte(`{"id":"`+created.ID+`","title":"renamed"}`))

	got, err := cache.Get(created.ID)
	if err != nil {
		t.Fatalf("Get after event: %v", err)
	}
	if got.Title != "renamed" {
		t.Fatalf("Title = %q, want the patch to have applied", got.Title)
	}
	if got.ClosedAt == nil || !got.ClosedAt.Equal(closedAt) {
		t.Fatalf("ClosedAt = %v, want %s preserved through a patch that omits it", got.ClosedAt, closedAt)
	}
	if got.CloseReason != "shipped in #118" {
		t.Fatalf("CloseReason = %q, want it preserved through a patch that omits it", got.CloseReason)
	}
	if got.Owner != "me@heyhilton.com" || got.CreatedBy != "gc-mayor" {
		t.Fatalf("attribution = %q/%q, want it preserved through a patch that omits it", got.Owner, got.CreatedBy)
	}
}
