package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/clientcontext"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/mail"
)

// writeMailPeerContexts gives a fresh GC_HOME a contexts.toml naming cities.
func writeMailPeerContexts(t *testing.T, cities ...string) {
	t.Helper()
	t.Setenv("GC_HOME", t.TempDir())
	file := &clientcontext.File{}
	for _, city := range cities {
		file.Contexts = append(file.Contexts, clientcontext.Context{Name: city, URL: "https://" + city + ".example.test"})
	}
	if err := file.Save(DefaultPath()); err != nil {
		t.Fatal(err)
	}
}

func TestDialMailPeer(t *testing.T) {
	cityPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(cityPath, "ansible.token"), []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	peered := &config.City{Mail: config.MailConfig{Peers: []config.MailPeer{{City: "ansible", TokenFile: "ansible.token"}}}}

	writeMailPeerContexts(t, "ansible")
	peer, err := dialMailPeer(peered, cityPath, "ansible")
	if err != nil {
		t.Fatalf("dialMailPeer: %v", err)
	}
	if peer.token != "tok" || peer.client == nil {
		t.Fatalf("peer = %+v: want a client and the token file's secret", peer)
	}

	if _, err := dialMailPeer(peered, cityPath, "elsewhere"); err == nil || !strings.Contains(err.Error(), `unknown city "elsewhere"`) {
		t.Fatalf("a city with no gc context: err = %v, want unknown city", err)
	}
	if _, err := dialMailPeer(&config.City{}, cityPath, "ansible"); err == nil || !strings.Contains(err.Error(), "[[mail.peer]]") {
		t.Fatalf("a context without a [[mail.peer]] entry: err = %v, want the missing peer named", err)
	}
}

func TestMailSendToUnknownCityFails(t *testing.T) {
	writeMailPeerContexts(t)
	var stdout, stderr bytes.Buffer
	cfg := &config.City{Mail: config.MailConfig{Peers: []config.MailPeer{{City: "ansible", TokenFile: "t"}}}}
	code := cmdMailSendPeer(mail.NewFake(), cfg, t.TempDir(), "jane", mail.PeerAddress{Agent: "riker", City: "ansible"}, []string{"riker@ansible", "hi"}, false, "", false, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), `unknown city "ansible"`) {
		t.Fatalf("code %d, stderr %q: want unknown city", code, stderr.String())
	}
}

// fakePeerCity is the destination city: it stores what it is sent and the
// notices its recipients get.
type fakePeerCity struct {
	store   *mail.Fake
	notices []string
}

func (c *fakePeerCity) send(m mail.Message) (mail.Message, error) {
	return c.store.SendInThread(m.From, m.To, m.Subject, m.Body, m.ThreadID, m.ReplyTo)
}

func (c *fakePeerCity) notify(recipient, text string) error {
	c.notices = append(c.notices, recipient+": "+text)
	return nil
}

func TestDoMailPeerSendDeliversRecordsAndNotifies(t *testing.T) {
	local := mail.NewFake()
	remote := &fakePeerCity{store: mail.NewFake()}
	rec := events.NewFake()
	var stdout, stderr bytes.Buffer

	code := doMailPeer(local, rec, remote.send, remote.notify, "wonderland", mail.Message{From: "jane", To: "riker@ansible", Subject: "s", Body: "b"}, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code %d, stderr %s", code, stderr.String())
	}
	if want := "Sent message fake-1 to riker@ansible\n"; stdout.String() != want {
		t.Fatalf("stdout %q, want %q", stdout.String(), want)
	}
	if len(remote.notices) != 1 || remote.notices[0] != "riker: You have mail from jane@wonderland" {
		t.Fatalf("notices %v: want riker told of mail from jane@wonderland", remote.notices)
	}
	if len(rec.Events) != 1 || rec.Events[0].Type != events.MailSent || rec.Events[0].Message != "riker@ansible" {
		t.Fatalf("events %+v: want one mail.sent to riker@ansible", rec.Events)
	}
}

func TestDoMailPeerReplyAnswersInThePeerCity(t *testing.T) {
	remote := &fakePeerCity{store: mail.NewFake()}
	rec := events.NewFake()
	var stdout, stderr bytes.Buffer

	code := doMailPeer(mail.NewFake(), rec, remote.send, nil, "ansible", mail.Message{From: "riker", To: "jane@wonderland", Subject: "Re: s", Body: "b", ThreadID: "thread-9", ReplyTo: "wl-1"}, true, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code %d, stderr %s", code, stderr.String())
	}
	delivered, err := remote.store.Thread("thread-9")
	if err != nil || len(delivered) != 1 || delivered[0].ReplyTo != "wl-1" || delivered[0].From != "riker@ansible" {
		t.Fatalf("peer thread-9 = %+v, %v: want riker@ansible answering wl-1", delivered, err)
	}
	if len(rec.Events) != 1 || rec.Events[0].Type != events.MailReplied {
		t.Fatalf("events %+v: want one mail.replied", rec.Events)
	}
	if !strings.Contains(stdout.String(), `"command":"mail.reply"`) || len(remote.notices) != 0 {
		t.Fatalf("stdout %q, notices %v: want a mail.reply JSON line and no notice without --notify", stdout.String(), remote.notices)
	}
}

func TestDoMailPeerReportsALostSendersCopy(t *testing.T) {
	remote := &fakePeerCity{store: mail.NewFake()}
	var stdout, stderr bytes.Buffer
	code := doMailPeer(mail.NewFailFake(), events.Discard, remote.send, nil, "wonderland", mail.Message{From: "jane", To: "riker@ansible", Body: "b"}, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("delivered mail exited %d", code)
	}
	if !strings.Contains(stderr.String(), "keeping the sender's copy failed") || !strings.Contains(stdout.String(), "Sent message") {
		t.Fatalf("stdout %q, stderr %q: want the send reported and the lost copy warned", stdout.String(), stderr.String())
	}
}

func TestDoMailPeerFailedDeliveryFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	refuse := func(mail.Message) (mail.Message, error) { return mail.Message{}, errors.New("403 forbidden") }
	code := doMailPeer(mail.NewFake(), events.Discard, refuse, nil, "wonderland", mail.Message{From: "jane", To: "riker@ansible", Body: "b"}, false, &stdout, &stderr)
	if code == 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "403 forbidden") {
		t.Fatalf("code %d, stdout %q, stderr %q: want failure with the peer's error", code, stdout.String(), stderr.String())
	}
}
