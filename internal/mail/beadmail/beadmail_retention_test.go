package beadmail

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/coordclass"
	"github.com/gastownhall/gascity/internal/mail"
)

// archiveBeforeCutoff and purgeBeforeCutoff give every recipient a one
// nanosecond window, so a sweep or purge run at cutoff+1ns acts on exactly the
// read mail created before cutoff.
var (
	archiveBeforeCutoff = config.NewMailRetentionPolicy(config.MailRetentionWindows{ArchiveReadAfter: time.Nanosecond})
	purgeBeforeCutoff   = config.NewMailRetentionPolicy(config.MailRetentionWindows{RetentionTTL: time.Nanosecond})
)

// readMailSeed builds a seed Bead for NewMemStoreFrom representing an open read
// message bead created at createdAt. opts mutate the bead (e.g. drop the "read"
// label or mark it closed) so a single helper covers every candidate variant.
func readMailSeed(id string, createdAt time.Time, opts ...func(*beads.Bead)) beads.Bead {
	b := beads.Bead{
		ID:        id,
		Type:      "message",
		Status:    "open",
		Labels:    []string{"read"},
		CreatedAt: createdAt,
	}
	for _, opt := range opts {
		opt(&b)
	}
	return b
}

// closeErrStore errors on Close for the configured IDs, exercising the
// per-bead (non-fatal) error path of the retention sweep.
type closeErrStore struct {
	*beads.MemStore
	failClose map[string]error
}

func (s closeErrStore) Close(id string) error {
	if err, ok := s.failClose[id]; ok {
		return err
	}
	return s.MemStore.Close(id)
}

// listErrStore errors on any message-typed List, exercising the fatal
// candidate-listing error path of the retention sweep.
type listErrStore struct {
	*beads.MemStore
	err error
}

func (s listErrStore) List(query beads.ListQuery) ([]beads.Bead, error) {
	if query.Type == "message" {
		return nil, s.err
	}
	return s.MemStore.List(query)
}

// deleteTrackStore records every successful Delete and can be told to fail a
// specific ID, mirroring the wisp-GC test double for the purge path.
type deleteTrackStore struct {
	*beads.MemStore
	failDelete map[string]error
	// getErrors injects a live-read failure for a specific ID so tests can
	// exercise the pre-delete re-verify's error branch.
	getErrors map[string]error
	deleted   []string
}

func (s *deleteTrackStore) Get(id string) (beads.Bead, error) {
	if err, ok := s.getErrors[id]; ok {
		return beads.Bead{}, err
	}
	return s.MemStore.Get(id)
}

func (s *deleteTrackStore) Delete(id string) error {
	if err, ok := s.failDelete[id]; ok {
		return err
	}
	if err := s.MemStore.Delete(id); err != nil {
		return err
	}
	s.deleted = append(s.deleted, id)
	return nil
}

func TestSweepReadMessages_ClosesAgedReadMailWithReason(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cutoff := now
	old := now.Add(-time.Minute)
	fresh := now.Add(time.Minute)

	seed := []beads.Bead{
		readMailSeed("old-1", old),
		readMailSeed("old-2", old),
		readMailSeed("fresh", fresh),
		readMailSeed("unread", old, func(b *beads.Bead) { b.Labels = nil }),
		readMailSeed("already-closed", old, func(b *beads.Bead) { b.Status = "closed" }),
	}
	store := beads.NewMemStoreFrom(100, seed, nil)
	mailStore := beads.MailStore{Store: store}

	const reason = "mail gc-swept: test retention reason padded to length"
	closed, closeErrs, listErr := SweepReadMessages(mailStore, archiveBeforeCutoff, cutoff.Add(time.Nanosecond), 0, reason)
	if listErr != nil {
		t.Fatalf("unexpected list error: %v", listErr)
	}
	if len(closeErrs) != 0 {
		t.Fatalf("unexpected per-bead errors: %v", closeErrs)
	}
	if len(closed) != 2 {
		t.Fatalf("closed = %d, want 2", len(closed))
	}

	for _, id := range []string{"old-1", "old-2"} {
		b, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if b.Status != "closed" {
			t.Errorf("%s status = %q, want closed", id, b.Status)
		}
		if got := b.Metadata["close_reason"]; got != reason {
			t.Errorf("%s close_reason = %q, want %q", id, got, reason)
		}
	}

	for _, id := range []string{"fresh", "unread"} {
		b, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if b.Status != "open" {
			t.Errorf("%s status = %q, want open (must not be swept)", id, b.Status)
		}
		if _, ok := b.Metadata["close_reason"]; ok {
			t.Errorf("%s unexpectedly stamped close_reason", id)
		}
	}
}

func TestSweepReadMessages_LimitCapsCloses(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-time.Minute)

	seed := []beads.Bead{
		readMailSeed("old-1", old),
		readMailSeed("old-2", old),
		readMailSeed("old-3", old),
	}
	store := beads.NewMemStoreFrom(100, seed, nil)
	mailStore := beads.MailStore{Store: store}

	closed, closeErrs, listErr := SweepReadMessages(mailStore, archiveBeforeCutoff, now.Add(time.Nanosecond), 2, "reason padded to twenty plus characters")
	if listErr != nil || len(closeErrs) != 0 {
		t.Fatalf("unexpected errors: list=%v perBead=%v", listErr, closeErrs)
	}
	if len(closed) != 2 {
		t.Fatalf("closed = %d, want 2 (limit)", len(closed))
	}

	openCount := 0
	all, err := store.List(beads.ListQuery{Type: "message", Label: "read", TierMode: beads.TierBoth})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range all {
		if b.Status == "open" {
			openCount++
		}
	}
	if openCount != 1 {
		t.Fatalf("open read beads = %d, want 1 (limit left one)", openCount)
	}
}

func TestSweepReadMessages_PerBeadCloseErrorIsCollected(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-time.Minute)

	// good is older so the created_asc sweep visits it first; both are aged.
	seed := []beads.Bead{
		readMailSeed("good", old.Add(-time.Minute)),
		readMailSeed("bad", old),
	}
	base := beads.NewMemStoreFrom(100, seed, nil)
	store := closeErrStore{MemStore: base, failClose: map[string]error{"bad": errors.New("close boom")}}
	mailStore := beads.MailStore{Store: store}

	closed, closeErrs, listErr := SweepReadMessages(mailStore, archiveBeforeCutoff, now.Add(time.Nanosecond), 0, "reason padded to twenty plus characters")
	if listErr != nil {
		t.Fatalf("unexpected list error: %v", listErr)
	}
	if len(closed) != 1 {
		t.Fatalf("closed = %d, want 1 (good only)", len(closed))
	}
	if len(closeErrs) != 1 {
		t.Fatalf("closeErrs = %v, want exactly one", closeErrs)
	}
	if got := closeErrs[0].Error(); !strings.Contains(got, "bad") || !strings.Contains(got, "close boom") {
		t.Fatalf("closeErrs[0] = %q, want it to name the bead and the close failure", got)
	}
}

func TestSweepReadMessages_ListErrorIsFatal(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store := listErrStore{MemStore: beads.NewMemStore(), err: errors.New("store down")}
	mailStore := beads.MailStore{Store: store}

	closed, closeErrs, listErr := SweepReadMessages(mailStore, archiveBeforeCutoff, now.Add(time.Nanosecond), 0, "reason padded to twenty plus characters")
	if listErr == nil {
		t.Fatal("expected fatal list error")
	}
	if len(closed) != 0 || len(closeErrs) != 0 {
		t.Fatalf("closed=%d closeErrs=%v, want zero on list failure", len(closed), closeErrs)
	}
}

func TestCountReadMessages_CountsWithoutMutating(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-time.Minute)
	fresh := now.Add(time.Minute)

	seed := []beads.Bead{
		readMailSeed("old-1", old),
		readMailSeed("old-2", old),
		readMailSeed("fresh", fresh),
		readMailSeed("unread", old, func(b *beads.Bead) { b.Labels = nil }),
	}
	store := beads.NewMemStoreFrom(100, seed, nil)
	mailStore := beads.MailStore{Store: store}

	count, err := CountReadMessages(mailStore, archiveBeforeCutoff, now.Add(time.Nanosecond), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	// No mutation: every seeded bead is still open.
	for _, id := range []string{"old-1", "old-2", "fresh", "unread"} {
		b, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if b.Status != "open" {
			t.Errorf("%s status = %q, count must not mutate", id, b.Status)
		}
	}
}

func TestCountReadMessages_LimitCapsCount(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-time.Minute)
	seed := []beads.Bead{
		readMailSeed("old-1", old),
		readMailSeed("old-2", old),
		readMailSeed("old-3", old),
	}
	store := beads.NewMemStoreFrom(100, seed, nil)
	mailStore := beads.MailStore{Store: store}

	count, err := CountReadMessages(mailStore, archiveBeforeCutoff, now.Add(time.Nanosecond), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2 (limit)", count)
	}
}

func TestPurgeReadMessageWisps_DeletesAgedReadWisps(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-time.Hour)
	aged := now.Add(-2 * time.Hour)
	recent := now.Add(-30 * time.Minute)

	wisp := func(id string, createdAt time.Time, meta map[string]string) beads.Bead {
		return beads.Bead{ID: id, Type: "message", Status: "open", CreatedAt: createdAt, Metadata: meta, Ephemeral: true}
	}
	seed := []beads.Bead{
		wisp("read-old", aged, map[string]string{mail.ReadMetadataKey: "true"}),
		wisp("unread-old", aged, map[string]string{mail.ReadMetadataKey: "false"}),
		wisp("unset-old", aged, nil),
		wisp("read-recent", recent, map[string]string{mail.ReadMetadataKey: "true"}),
		// Main-tier read message: excluded by the TierWisps query.
		{ID: "read-main", Type: "message", Status: "open", CreatedAt: aged, Metadata: map[string]string{mail.ReadMetadataKey: "true"}},
		// Wisp-tier but not a message bead: excluded by Type=message.
		{ID: "read-task-wisp", Type: "task", Status: "open", CreatedAt: aged, Metadata: map[string]string{mail.ReadMetadataKey: "true"}, Ephemeral: true},
	}
	store := &deleteTrackStore{MemStore: beads.NewMemStoreFrom(100, seed, nil), failDelete: map[string]error{}}
	mailStore := beads.MailStore{Store: store}

	purged, err := PurgeReadMessageWisps(mailStore, purgeBeforeCutoff, cutoff.Add(time.Nanosecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(purged) != 1 {
		t.Fatalf("purged = %d, want 1", len(purged))
	}
	if len(store.deleted) != 1 || store.deleted[0] != "read-old" {
		t.Fatalf("deleted = %v, want [read-old]", store.deleted)
	}
	for _, id := range []string{"unread-old", "unset-old", "read-recent", "read-main", "read-task-wisp"} {
		if _, err := store.Get(id); err != nil {
			t.Errorf("%s should be preserved: %v", id, err)
		}
	}
}

// staleListStore returns a fixed, caller-supplied snapshot from List
// regardless of the underlying store's current state, modeling a
// CachingStore whose enumeration answers from a stale cached view while
// Get/Delete (via the embedded store) stay live. This lets tests put List and
// Get/Delete out of sync the way a real cache-window race would.
type staleListStore struct {
	*deleteTrackStore
	snapshot []beads.Bead
}

func (s *staleListStore) List(beads.ListQuery) ([]beads.Bead, error) {
	return s.snapshot, nil
}

// TestPurgeReadMessageWisps_SkipsMessageUnreadAfterSnapshot is the regression
// test for ra-nxppyo: the candidate List() can answer from the CachingStore's
// stale view. A message the user un-read inside the cache window — after the
// read:true snapshot was taken but before the purge sweep reaches it — must
// not be destructively deleted on the strength of that stale snapshot.
func TestPurgeReadMessageWisps_SkipsMessageUnreadAfterSnapshot(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-time.Hour)
	aged := now.Add(-2 * time.Hour)

	seed := []beads.Bead{
		{ID: "unread-after-snapshot", Type: "message", Status: "open", CreatedAt: aged, Labels: []string{"read"}, Metadata: map[string]string{mail.ReadMetadataKey: "true"}, Ephemeral: true},
	}
	underlying := &deleteTrackStore{MemStore: beads.NewMemStoreFrom(100, seed, nil), failDelete: map[string]error{}}
	store := &staleListStore{deleteTrackStore: underlying, snapshot: seed}

	// Simulate the user un-reading the message inside the cache window: the
	// live store now disagrees with the stale read:true snapshot List returns.
	if err := underlying.Update("unread-after-snapshot", beads.UpdateOpts{
		RemoveLabels: []string{"read"},
		Metadata:     map[string]string{mail.ReadMetadataKey: "false"},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	mailStore := beads.MailStore{Store: store}
	purged, err := PurgeReadMessageWisps(mailStore, purgeBeforeCutoff, cutoff.Add(time.Nanosecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(purged) != 0 {
		t.Fatalf("purged = %d, want 0 (message was un-read after the snapshot)", len(purged))
	}
	if len(underlying.deleted) != 0 {
		t.Fatalf("deleted = %v, want none", underlying.deleted)
	}
	if _, err := underlying.Get("unread-after-snapshot"); err != nil {
		t.Fatalf("message should be preserved: %v", err)
	}
}

// TestPurgeReadMessageWisps_SkipsMessageGoneAfterSnapshot asserts a candidate
// that no longer exists live (deleted by a concurrent path between the
// snapshot and this sweep) is skipped rather than erroring the whole sweep.
func TestPurgeReadMessageWisps_SkipsMessageGoneAfterSnapshot(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-time.Hour)
	aged := now.Add(-2 * time.Hour)

	seed := []beads.Bead{
		{ID: "gone-after-snapshot", Type: "message", Status: "open", CreatedAt: aged, Labels: []string{"read"}, Metadata: map[string]string{mail.ReadMetadataKey: "true"}, Ephemeral: true},
	}
	underlying := &deleteTrackStore{MemStore: beads.NewMemStoreFrom(100, seed, nil), failDelete: map[string]error{}}
	store := &staleListStore{deleteTrackStore: underlying, snapshot: seed}

	// Simulate a concurrent delete inside the cache window: the live store no
	// longer has the bead the stale snapshot still lists.
	if err := underlying.MemStore.Delete("gone-after-snapshot"); err != nil {
		t.Fatalf("seed delete: %v", err)
	}

	mailStore := beads.MailStore{Store: store}
	purged, err := PurgeReadMessageWisps(mailStore, purgeBeforeCutoff, cutoff.Add(time.Nanosecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(purged) != 0 {
		t.Fatalf("purged = %d, want 0 (message already gone)", len(purged))
	}
	if len(underlying.deleted) != 0 {
		t.Fatalf("deleted = %v, want none", underlying.deleted)
	}
}

// TestPurgeReadMessageWisps_SurfacesLiveRecheckError asserts that a transient
// live-read failure during the pre-delete re-verify is reported rather than
// silently swallowed as "already gone" — and still never deletes.
func TestPurgeReadMessageWisps_SurfacesLiveRecheckError(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-time.Hour)
	aged := now.Add(-2 * time.Hour)

	seed := []beads.Bead{
		{ID: "boom", Type: "message", Status: "open", CreatedAt: aged, Labels: []string{"read"}, Metadata: map[string]string{mail.ReadMetadataKey: "true"}, Ephemeral: true},
	}
	underlying := &deleteTrackStore{
		MemStore:   beads.NewMemStoreFrom(100, seed, nil),
		failDelete: map[string]error{},
		getErrors:  map[string]error{"boom": errors.New("backend down")},
	}
	store := &staleListStore{deleteTrackStore: underlying, snapshot: seed}

	mailStore := beads.MailStore{Store: store}
	purged, err := PurgeReadMessageWisps(mailStore, purgeBeforeCutoff, cutoff.Add(time.Nanosecond))
	if err == nil {
		t.Fatal("PurgeReadMessageWisps: want error, got nil (a live-read failure must not be swallowed)")
	}
	if len(purged) != 0 {
		t.Fatalf("purged = %d, want 0 (live re-verify failed)", len(purged))
	}
	if len(underlying.deleted) != 0 {
		t.Fatalf("deleted = %v, want none", underlying.deleted)
	}
}

func TestPurgeReadMessageWisps_DeleteErrorSurfacedAndContinues(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-time.Hour)
	aged := now.Add(-2 * time.Hour)

	wisp := func(id string) beads.Bead {
		return beads.Bead{ID: id, Type: "message", Status: "open", CreatedAt: aged, Metadata: map[string]string{mail.ReadMetadataKey: "true"}, Ephemeral: true}
	}
	store := &deleteTrackStore{
		MemStore:   beads.NewMemStoreFrom(100, []beads.Bead{wisp("bad"), wisp("good")}, nil),
		failDelete: map[string]error{"bad": errors.New("delete boom")},
	}
	mailStore := beads.MailStore{Store: store}

	purged, err := PurgeReadMessageWisps(mailStore, purgeBeforeCutoff, cutoff.Add(time.Nanosecond))
	if err == nil {
		t.Fatal("expected delete error to be surfaced")
	}
	if len(purged) != 1 {
		t.Fatalf("purged = %d, want 1 (good deleted)", len(purged))
	}
	if !contains(store.deleted, "good") {
		t.Fatalf("deleted = %v, want to include good", store.deleted)
	}
}

func TestPurgeReadMessageWisps_ListErrorSurfaced(t *testing.T) {
	store := listErrStore{MemStore: beads.NewMemStore(), err: errors.New("store down")}
	mailStore := beads.MailStore{Store: store}
	purged, err := PurgeReadMessageWisps(mailStore, purgeBeforeCutoff, time.Now().Add(time.Nanosecond))
	if err == nil {
		t.Fatal("expected list error to be surfaced")
	}
	if len(purged) != 0 {
		t.Fatalf("purged = %d, want 0", len(purged))
	}
}

func TestIsMessageBead(t *testing.T) {
	if !IsMessageBead(beads.Bead{Type: "message"}) {
		t.Error("Type=message must be a message bead")
	}
	if IsMessageBead(beads.Bead{Type: "task"}) {
		t.Error("Type=task must not be a message bead")
	}
	if IsMessageBead(beads.Bead{}) {
		t.Error("empty-type bead must not be a message bead")
	}

	// A message bead that also carries wisp metadata is still a message bead:
	// IsMessageBead is a bare Type check, deliberately NOT coordclass.Classify
	// (which would route the wisp-marked bead to ClassGraph). This preserves the
	// historical inline `b.Type == "message"` behavior at the order single-flight
	// gate.
	wispMsg := beads.Bead{Type: "message", Metadata: map[string]string{beadmeta.KindMetadataKey: beadmeta.KindWisp}}
	if !IsMessageBead(wispMsg) {
		t.Error("wisp-marked message bead must still report true")
	}
	if coordclass.Classify(wispMsg) != coordclass.ClassGraph {
		t.Fatalf("precondition: expected wisp-marked message to Classify as ClassGraph, got %v", coordclass.Classify(wispMsg))
	}
}

// TestRetentionSweptReadMailStaysAddressableUntilPurge ties the retention sweep
// to the Provider surface. The always-on nudge-mail watchdog closes read mail
// past its TTL (stamping RetentionSweepCloseReason) and PurgeReadMessageWisps
// deletes it later; during that closed-but-not-purged window the message is
// archived: addressable by direct ID, listed by Archived, and absent from the
// inbox views. Every closed message bead reads the same way, whatever closed
// it.
func TestRetentionSweptReadMailStaysAddressableUntilPurge(t *testing.T) {
	store := beads.NewMemStore()
	p := New(store)

	sent, err := p.Send("alice", "bob", "aged", "read long ago")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Mark it read so the retention sweep treats it as a candidate.
	if _, err := p.Read(sent.ID); err != nil {
		t.Fatalf("Read before sweep: %v", err)
	}

	// The retention sweep closes the aged read mail with the canonical reason,
	// exactly as the production nudge-mail watchdog does.
	closed, closeErrs, listErr := SweepReadMessages(beads.MailStore{Store: store}, archiveBeforeCutoff, time.Now().Add(time.Hour+time.Nanosecond), 0, RetentionSweepCloseReason)
	if listErr != nil {
		t.Fatalf("sweep list error: %v", listErr)
	}
	if len(closeErrs) != 0 {
		t.Fatalf("sweep per-bead errors: %v", closeErrs)
	}
	if len(closed) != 1 || closed[0] != sent.ID {
		t.Fatalf("swept IDs = %v, want [%s]", closed, sent.ID)
	}

	// Precondition: the bead is closed and carries the retention marker.
	raw, err := store.Get(sent.ID)
	if err != nil {
		t.Fatalf("store.Get after sweep: %v", err)
	}
	if raw.Status != "closed" || raw.Metadata["close_reason"] != RetentionSweepCloseReason {
		t.Fatalf("swept bead status=%q close_reason=%q, want closed / %q",
			raw.Status, raw.Metadata["close_reason"], RetentionSweepCloseReason)
	}

	// Retention-swept mail stays addressable by direct ID until purge.
	if _, err := p.Get(sent.ID); err != nil {
		t.Errorf("Get(retention-swept) = %v, want addressable", err)
	}
	if _, err := p.Read(sent.ID); err != nil {
		t.Errorf("Read(retention-swept) = %v, want addressable", err)
	}
	reply, err := p.Reply(sent.ID, "bob", "RE: aged", "still replying after retention")
	if err != nil {
		t.Fatalf("Reply(retention-swept) = %v, want addressable", err)
	}
	if reply.ID == "" {
		t.Error("Reply(retention-swept) returned an empty message")
	}

	// But it is retired from the active list views, which already gate on open
	// status — the same asymmetry as before this PR.
	inbox, err := p.Inbox("bob")
	if err != nil {
		t.Fatalf("Inbox after sweep: %v", err)
	}
	for _, m := range inbox {
		if m.ID == sent.ID {
			t.Errorf("Inbox surfaced retention-swept message %q", sent.ID)
		}
	}

	archived, err := p.Archived("bob")
	if err != nil {
		t.Fatalf("Archived after sweep: %v", err)
	}
	if len(archived) != 1 || archived[0].ID != sent.ID || archived[0].Status != mail.StatusClosed {
		t.Errorf("Archived after sweep = %+v, want the swept message, closed", archived)
	}

	// A message closed for any other reason is archived the same way.
	other, err := p.Send("alice", "bob", "closed elsewhere", "closed by a non-retention path")
	if err != nil {
		t.Fatalf("Send other: %v", err)
	}
	if err := store.SetMetadata(other.ID, "close_reason", "manual close through bd, not the mail API"); err != nil {
		t.Fatalf("SetMetadata other: %v", err)
	}
	if err := store.Close(other.ID); err != nil {
		t.Fatalf("Close other: %v", err)
	}
	got, err := p.Get(other.ID)
	if err != nil {
		t.Fatalf("Get(non-retention closed) = %v, want addressable", err)
	}
	if got.Status != mail.StatusClosed {
		t.Errorf("Get(non-retention closed).Status = %q, want %q", got.Status, mail.StatusClosed)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// recipientPolicy archives and purges read mail after an hour, except for
// "human", whose read mail is never archived or purged, and "*/witness",
// whose read mail is archived after ten minutes and purged after two hours.
func recipientPolicy(t *testing.T) config.MailRetentionPolicy {
	t.Helper()
	policy, err := config.MailConfig{
		ArchiveReadAfter: "1h",
		RetentionTTL:     "1h",
		Recipients: []config.MailRecipientRetention{
			{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
			{Match: "*/witness", ArchiveReadAfter: "10m", RetentionTTL: "2h"},
		},
	}.RetentionPolicy()
	if err != nil {
		t.Fatalf("RetentionPolicy: %v", err)
	}
	return policy
}

func addressedTo(recipient string) func(*beads.Bead) {
	return func(b *beads.Bead) { b.Assignee = recipient }
}

func TestSweepReadMessages_PerRecipientWindows(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seed := []beads.Bead{
		readMailSeed("human-old", now.Add(-72*time.Hour), addressedTo("human")),
		readMailSeed("worker-old", now.Add(-2*time.Hour), addressedTo("gascity/worker")),
		readMailSeed("worker-recent", now.Add(-30*time.Minute), addressedTo("gascity/worker")),
		readMailSeed("witness-recent", now.Add(-30*time.Minute), addressedTo("gascity/witness")),
		readMailSeed("witness-fresh", now.Add(-5*time.Minute), addressedTo("gascity/witness")),
	}
	store := beads.NewMemStoreFrom(100, seed, nil)

	closed, closeErrs, listErr := SweepReadMessages(beads.MailStore{Store: store}, recipientPolicy(t), now, 0, RetentionSweepCloseReason)
	if listErr != nil || len(closeErrs) != 0 {
		t.Fatalf("sweep errors: list=%v close=%v", listErr, closeErrs)
	}
	if got, want := strings.Join(closed, ","), "worker-old,witness-recent"; got != want {
		t.Fatalf("closed = %s, want %s", got, want)
	}
	for id, wantStatus := range map[string]string{
		"human-old":      "open",
		"worker-old":     "closed",
		"worker-recent":  "open",
		"witness-recent": "closed",
		"witness-fresh":  "open",
	} {
		b, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if b.Status != wantStatus {
			t.Errorf("%s status = %q, want %q", id, b.Status, wantStatus)
		}
	}
}

// TestSweepReadMessages_SkippedMessagesConsumeNoBudget pins that a recipient
// whose window is zero neither spends the close budget nor starves the
// recipients behind it: its old read mail is the oldest candidate set, so a
// candidate listing bounded by the budget would return only messages the policy
// skips and close nothing.
func TestSweepReadMessages_SkippedMessagesConsumeNoBudget(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seed := []beads.Bead{
		readMailSeed("human-1", now.Add(-96*time.Hour), addressedTo("human")),
		readMailSeed("human-2", now.Add(-95*time.Hour), addressedTo("human")),
		readMailSeed("human-3", now.Add(-94*time.Hour), addressedTo("human")),
		readMailSeed("worker-1", now.Add(-3*time.Hour), addressedTo("gascity/worker")),
		readMailSeed("worker-2", now.Add(-2*time.Hour), addressedTo("gascity/worker")),
	}
	store := beads.NewMemStoreFrom(100, seed, nil)
	mailStore := beads.MailStore{Store: store}
	policy := recipientPolicy(t)

	count, err := CountReadMessages(mailStore, policy, now, 1)
	if err != nil {
		t.Fatalf("CountReadMessages: %v", err)
	}
	closed, closeErrs, listErr := SweepReadMessages(mailStore, policy, now, 1, RetentionSweepCloseReason)
	if listErr != nil || len(closeErrs) != 0 {
		t.Fatalf("sweep errors: list=%v close=%v", listErr, closeErrs)
	}
	if len(closed) != 1 || closed[0] != "worker-1" {
		t.Fatalf("closed = %v, want [worker-1]", closed)
	}
	if count != len(closed) {
		t.Fatalf("dry-run count = %d, sweep closed %d; they must agree", count, len(closed))
	}
}

func TestCountReadMessages_AgreesWithSweepPerRecipient(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seed := []beads.Bead{
		readMailSeed("human-old", now.Add(-72*time.Hour), addressedTo("human")),
		readMailSeed("worker-old", now.Add(-2*time.Hour), addressedTo("gascity/worker")),
		readMailSeed("worker-closed", now.Add(-2*time.Hour), addressedTo("gascity/worker"), func(b *beads.Bead) { b.Status = "closed" }),
		readMailSeed("witness-recent", now.Add(-30*time.Minute), addressedTo("gascity/witness")),
		readMailSeed("witness-fresh", now.Add(-5*time.Minute), addressedTo("gascity/witness")),
	}
	for _, limit := range []int{0, 1, 2, 5} {
		store := beads.NewMemStoreFrom(100, seed, nil)
		mailStore := beads.MailStore{Store: store}
		policy := recipientPolicy(t)
		count, err := CountReadMessages(mailStore, policy, now, limit)
		if err != nil {
			t.Fatalf("limit %d: CountReadMessages: %v", limit, err)
		}
		closed, _, listErr := SweepReadMessages(mailStore, policy, now, limit, RetentionSweepCloseReason)
		if listErr != nil {
			t.Fatalf("limit %d: sweep: %v", limit, listErr)
		}
		if count != len(closed) {
			t.Errorf("limit %d: count = %d, sweep closed %d", limit, count, len(closed))
		}
	}
}

func TestSweepReadMessages_NoArchiveWindowListsNothing(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store := &listCountStore{MemStore: beads.NewMemStoreFrom(100, []beads.Bead{
		readMailSeed("old", now.Add(-72*time.Hour), addressedTo("gascity/worker")),
	}, nil)}
	policy := config.NewMailRetentionPolicy(config.MailRetentionWindows{RetentionTTL: time.Hour})

	closed, _, listErr := SweepReadMessages(beads.MailStore{Store: store}, policy, now, 0, RetentionSweepCloseReason)
	if listErr != nil {
		t.Fatalf("sweep: %v", listErr)
	}
	if len(closed) != 0 || store.lists != 0 {
		t.Fatalf("closed = %v after %d listings, want nothing closed and nothing listed", closed, store.lists)
	}
}

// listCountStore counts List calls.
type listCountStore struct {
	*beads.MemStore
	lists int
}

func (s *listCountStore) List(q beads.ListQuery) ([]beads.Bead, error) {
	s.lists++
	return s.MemStore.List(q)
}

func TestPurgeReadMessageWisps_PerRecipientWindows(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	readWisp := func(id, recipient string, createdAt time.Time) beads.Bead {
		return beads.Bead{
			ID: id, Type: "message", Status: "closed", Assignee: recipient, CreatedAt: createdAt,
			Metadata: map[string]string{mail.ReadMetadataKey: "true"}, Ephemeral: true,
		}
	}
	seed := []beads.Bead{
		readWisp("human-ancient", "human", now.Add(-720*time.Hour)),
		readWisp("worker-old", "gascity/worker", now.Add(-90*time.Minute)),
		readWisp("worker-recent", "gascity/worker", now.Add(-30*time.Minute)),
		readWisp("witness-mid", "gascity/witness", now.Add(-90*time.Minute)),
		readWisp("witness-old", "gascity/witness", now.Add(-3*time.Hour)),
	}
	store := &deleteTrackStore{MemStore: beads.NewMemStoreFrom(100, seed, nil), failDelete: map[string]error{}}

	purged, err := PurgeReadMessageWisps(beads.MailStore{Store: store}, recipientPolicy(t), now)
	if err != nil {
		t.Fatalf("PurgeReadMessageWisps: %v", err)
	}
	got := map[string]bool{}
	for _, id := range purged {
		got[id] = true
	}
	want := map[string]bool{"worker-old": true, "witness-old": true}
	if len(got) != len(want) {
		t.Fatalf("purged = %v, want worker-old and witness-old", purged)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("purged = %v, missing %s", purged, id)
		}
	}
}

func TestPurgeReadMessageWisps_NoRetentionWindowListsNothing(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store := &listCountStore{MemStore: beads.NewMemStore()}
	purged, err := PurgeReadMessageWisps(beads.MailStore{Store: store}, config.MailRetentionPolicy{}, now)
	if err != nil || len(purged) != 0 || store.lists != 0 {
		t.Fatalf("purged=%v err=%v lists=%d, want nothing purged and nothing listed", purged, err, store.lists)
	}
}
