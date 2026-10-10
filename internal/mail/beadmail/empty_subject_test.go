package beadmail

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

// TestSendEmptySubjectStaysEmpty pins ga-f4rl: a message sent with no subject
// reports an empty subject on every read path instead of its body's first line.
// The bead title stays non-empty, since bd create requires one.
func TestSendEmptySubjectStaysEmpty(t *testing.T) {
	store := beads.NewMemStore()
	p := New(store)

	sent, err := p.Send("human", "mayor", "", "first line\nsecond line")
	if err != nil {
		t.Fatal(err)
	}
	if sent.Subject != "" {
		t.Errorf("Send Subject = %q, want empty", sent.Subject)
	}
	if sent.Body != "first line\nsecond line" {
		t.Errorf("Send Body = %q, want the body unchanged", sent.Body)
	}

	b, err := store.Get(sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title == "" {
		t.Error("bead Title is empty; bd create rejects an empty title")
	}
	if b.Metadata[subjectEmptyMetadataKey] != "true" {
		t.Errorf("bead metadata %s = %q, want %q", subjectEmptyMetadataKey, b.Metadata[subjectEmptyMetadataKey], "true")
	}

	inbox, err := p.Inbox("mayor")
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 || inbox[0].Subject != "" {
		t.Fatalf("Inbox = %+v, want one message with an empty subject", inbox)
	}
	got, err := p.Get(sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "" {
		t.Errorf("Get Subject = %q, want empty", got.Subject)
	}
	read, err := p.Read(sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Subject != "" {
		t.Errorf("Read Subject = %q, want empty", read.Subject)
	}
}

// TestSendEmptySubjectAndBodyPassesBdTitleValidation covers a message with
// neither subject nor body: the bead still gets a title bd accepts, and the
// subject still reads empty.
func TestSendEmptySubjectAndBodyPassesBdTitleValidation(t *testing.T) {
	var createdTitle string
	runner := func(_ string, name string, args ...string) ([]byte, error) {
		if name != "bd" {
			return nil, errors.New("unexpected command: " + name)
		}
		if args[0] != "create" {
			return nil, errors.New("unexpected bd subcommand: " + args[0])
		}
		createdTitle = args[2]
		if createdTitle == "" {
			return nil, errors.New(`exit status 1: {"error":"validation failed for issue : title is required"}`)
		}
		return []byte(`{"id":"bd-1","title":"` + createdTitle + `","status":"open","issue_type":"message","created_at":"2026-10-10T00:00:00Z","metadata":{"` + subjectEmptyMetadataKey + `":"true"}}`), nil
	}
	p := New(beads.NewBdStore(t.TempDir(), runner))

	sent, err := p.Send("human", "mayor", "", "")
	if err != nil {
		t.Fatalf("Send with no subject and no body: %v", err)
	}
	if createdTitle == "" {
		t.Error("bd create got an empty title")
	}
	if sent.Subject != "" {
		t.Errorf("Send Subject = %q, want empty", sent.Subject)
	}
}

// TestSendDedupedEmptySubjectStaysEmpty covers the dedup send path, which
// shares Send's body.
func TestSendDedupedEmptySubjectStaysEmpty(t *testing.T) {
	p := New(beads.NewMemStore())

	sent, _, err := p.SendDeduped("human", "mayor", "", "advisory body", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if sent.Subject != "" {
		t.Errorf("SendDeduped Subject = %q, want empty", sent.Subject)
	}
}

// TestTitledMessageWithoutFlagReadsAsBefore keeps existing mail, which has a
// real title and no subject-empty flag, reading its title as the subject.
func TestTitledMessageWithoutFlagReadsAsBefore(t *testing.T) {
	store := beads.NewMemStore()
	p := New(store)

	b, err := store.Create(beads.Bead{
		Title:       "legacy derived title",
		Description: "legacy derived title",
		Type:        messageBeadType,
		Assignee:    "mayor",
		From:        "human",
		Labels:      []string{"thread:t1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := p.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "legacy derived title" {
		t.Errorf("Subject = %q, want the stored title", got.Subject)
	}
}

// TestReplyToSubjectlessMessageStaysSubjectless keeps a thread with no subject
// subject-free: a reply with no subject of its own does not borrow its body.
func TestReplyToSubjectlessMessageStaysSubjectless(t *testing.T) {
	store := beads.NewMemStore()
	p := New(store)

	sent, err := p.Send("alice", "bob", "", "first message")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := p.Reply(sent.ID, "bob", "", "reply body")
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if reply.Subject != "" {
		t.Errorf("Reply Subject = %q, want empty", reply.Subject)
	}
	b, err := store.Get(reply.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title == "" {
		t.Error("reply bead Title is empty; bd create rejects an empty title")
	}

	explicit, err := p.Reply(sent.ID, "bob", "Now with a subject", "reply body")
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if explicit.Subject != "Now with a subject" {
		t.Errorf("Reply Subject = %q, want the explicit subject", explicit.Subject)
	}
}
