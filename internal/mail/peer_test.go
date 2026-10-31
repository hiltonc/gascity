package mail

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePeerAddress(t *testing.T) {
	for _, c := range []struct {
		addr   string
		want   PeerAddress
		isPeer bool
	}{
		{"jane", PeerAddress{}, false},
		{"myrig/jane", PeerAddress{}, false},
		{"human", PeerAddress{}, false},
		{"jane@ansible", PeerAddress{Agent: "jane", City: "ansible"}, true},
		{" jane@ansible ", PeerAddress{Agent: "jane", City: "ansible"}, true},
		{"myrig/jane@ansible", PeerAddress{Agent: "myrig/jane", City: "ansible"}, true},
		{"a@b@ansible", PeerAddress{Agent: "a@b", City: "ansible"}, true},
		{"@ansible", PeerAddress{}, false},
		{"jane@", PeerAddress{}, false},
		{"jane@rig/ansible", PeerAddress{}, false},
	} {
		got, ok := ParsePeerAddress(c.addr)
		if ok != c.isPeer || got != c.want {
			t.Errorf("ParsePeerAddress(%q) = %+v, %v; want %+v, %v", c.addr, got, ok, c.want, c.isPeer)
		}
		if ok && got.String() != strings.TrimSpace(c.addr) {
			t.Errorf("ParsePeerAddress(%q).String() = %q", c.addr, got.String())
		}
	}
}

// peerCity is a destination city reached through a PeerSendFunc.
type peerCity struct{ store *Fake }

func (p *peerCity) send(m Message) (Message, error) {
	return p.store.SendInThread(m.From, m.To, m.Subject, m.Body, m.ThreadID, m.ReplyTo)
}

func TestSendToPeerDeliversAndKeepsSendersCopy(t *testing.T) {
	local := NewFake()
	remote := &peerCity{store: NewFake()}

	delivered, err := SendToPeer(local, remote.send, "wonderland", Message{From: "jane", To: "riker@ansible", Body: "first line\nmore"})
	if err != nil {
		t.Fatalf("SendToPeer: %v", err)
	}
	if delivered.From != "jane@wonderland" || delivered.To != "riker" || delivered.Subject != "first line" {
		t.Fatalf("delivered %+v: want from jane@wonderland to riker, subject from body", delivered)
	}
	copies, err := local.Thread(delivered.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 1 {
		t.Fatalf("local thread %s holds %d messages, want the sender's copy", delivered.ThreadID, len(copies))
	}
	kept := copies[0]
	if kept.From != "jane" || kept.To != "riker@ansible" || !kept.Read || kept.Body != delivered.Body {
		t.Fatalf("sender's copy %+v: want read, jane -> riker@ansible, same body", kept)
	}
	if unread, _ := local.Inbox("jane"); len(unread) != 0 {
		t.Fatalf("the sender's copy reached an inbox: %+v", unread)
	}
}

func TestSendToPeerReplyJoinsTheThread(t *testing.T) {
	local := NewFake()
	remote := &peerCity{store: NewFake()}

	delivered, err := SendToPeer(local, remote.send, "ansible", Message{From: "riker", To: "jane@wonderland", Subject: "Re: hi", Body: "ok", ThreadID: "thread-1", ReplyTo: "wl-7"})
	if err != nil {
		t.Fatalf("SendToPeer: %v", err)
	}
	if delivered.ThreadID != "thread-1" || delivered.ReplyTo != "wl-7" {
		t.Fatalf("delivered %+v: want thread-1, reply to wl-7", delivered)
	}
	kept, err := local.Thread("thread-1")
	if err != nil || len(kept) != 1 || kept[0].ReplyTo != "wl-7" {
		t.Fatalf("local thread-1 = %+v, %v: want the copy replying to wl-7", kept, err)
	}
}

func TestSendToPeerFailedDeliveryKeepsNoCopy(t *testing.T) {
	local := NewFake()
	refused := errors.New("refused")
	_, err := SendToPeer(local, func(Message) (Message, error) { return Message{}, refused }, "wonderland", Message{From: "jane", To: "riker@ansible", Body: "b"})
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the delivery error", err)
	}
	if all, _ := local.All("riker@ansible"); len(all) != 0 {
		t.Fatalf("a failed delivery left a local copy: %+v", all)
	}
}

func TestSendToPeerRequiresAPeerAddressAndAThreadedProvider(t *testing.T) {
	sent := false
	send := func(Message) (Message, error) { sent = true; return Message{}, nil }
	if _, err := SendToPeer(NewFake(), send, "wonderland", Message{From: "jane", To: "riker", Body: "b"}); err == nil {
		t.Fatal("a bare recipient was sent as peer mail")
	}
	if _, err := SendToPeer(unthreaded{NewFake()}, send, "wonderland", Message{From: "jane", To: "riker@ansible", Body: "b"}); err == nil {
		t.Fatal("a provider that cannot keep the sender's copy was accepted")
	}
	if sent {
		t.Fatal("delivered before refusing")
	}
}

// unthreaded hides Fake's ThreadedSender capability.
type unthreaded struct{ Provider }

func TestReplySubject(t *testing.T) {
	for _, c := range []struct{ subject, original, body, want string }{
		{"mine", "hi", "b", "mine"},
		{"", "hi", "b", "Re: hi"},
		{"", "RE: hi", "b", "RE: hi"},
		{"", "", "line one\nline two", "line one"},
		{"", "", "", "(reply)"},
	} {
		if got := ReplySubject(c.subject, c.original, c.body); got != c.want {
			t.Errorf("ReplySubject(%q, %q, %q) = %q, want %q", c.subject, c.original, c.body, got, c.want)
		}
	}
	if got := DeriveSubject("", strings.Repeat("x", 100)); len(got) != subjectFromBodyLimit || !strings.HasSuffix(got, "...") {
		t.Errorf("DeriveSubject of a long body = %q", got)
	}
}
