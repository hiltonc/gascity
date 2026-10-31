package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/mail"
)

const peerTestToken = "shared-secret"

// newPeerCity is a fake city named name whose [[mail.peer]] entries name
// peers, every one sharing peerTestToken.
func newPeerCity(t *testing.T, name string, peers ...string) *fakeState {
	t.Helper()
	state := newFakeState(t)
	state.cityName = name
	state.cfg.Workspace.Name = name
	if err := os.WriteFile(filepath.Join(state.cityPath, "peer.token"), []byte(peerTestToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, peer := range peers {
		state.cfg.Mail.Peers = append(state.cfg.Mail.Peers, config.MailPeer{City: peer, TokenFile: "peer.token"})
	}
	return state
}

func postMail(t *testing.T, h http.Handler, state *fakeState, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, cityURL(state, "/mail"), bytes.NewBufferString(body))
	req.Header.Set("X-GC-Request", "true")
	if token != "" {
		req.Header.Set("X-GC-Mail-Peer-Token", token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMailFromAnotherCityNeedsThatPeersToken(t *testing.T) {
	for _, c := range []struct {
		name, from, token string
		want              int
	}{
		{"peer with its token", "jane@wonderland", peerTestToken, http.StatusCreated},
		{"peer with a wrong token", "jane@wonderland", "guess", http.StatusForbidden},
		{"peer without a token", "jane@wonderland", "", http.StatusForbidden},
		{"undeclared city", "jane@elsewhere", peerTestToken, http.StatusForbidden},
		{"this city's own name", "jane@ansible", peerTestToken, http.StatusUnprocessableEntity},
		{"local agent ignores the token", "worker", "", http.StatusCreated},
	} {
		t.Run(c.name, func(t *testing.T) {
			state := newPeerCity(t, "ansible", "wonderland")
			h := newTestCityHandler(t, state)
			rec := postMail(t, h, state, c.token, `{"from":"`+c.from+`","to":"worker","subject":"s","body":"b"}`)
			if rec.Code != c.want {
				t.Fatalf("status %d (%s), want %d", rec.Code, rec.Body.String(), c.want)
			}
			inbox, err := state.cityMailProv.Inbox("myrig/worker")
			if err != nil {
				t.Fatal(err)
			}
			if c.want != http.StatusCreated {
				if len(inbox) != 0 {
					t.Fatalf("refused send still delivered %+v", inbox)
				}
				return
			}
			if len(inbox) != 1 || inbox[0].From != c.from {
				t.Fatalf("inbox %+v: want one message from %s", inbox, c.from)
			}
		})
	}
}

func TestMailSendJoinsTheNamedThread(t *testing.T) {
	state := newPeerCity(t, "ansible", "wonderland")
	h := newTestCityHandler(t, state)
	rec := postMail(t, h, state, peerTestToken, `{"from":"jane@wonderland","to":"worker","subject":"Re: s","body":"b","thread_id":"thread-w1","reply_to":"wl-3"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	var got mail.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ThreadID != "thread-w1" || got.ReplyTo != "wl-3" {
		t.Fatalf("sent %+v: want thread-w1 replying to wl-3", got)
	}
}

// serveCity puts state behind a real HTTP server and returns a peer sender
// into it.
func serveCity(t *testing.T, state *fakeState) mail.PeerSendFunc {
	t.Helper()
	ts := httptest.NewServer(newTestCityHandler(t, state))
	t.Cleanup(ts.Close)
	c, err := NewRemoteCityScopedClient(ts.URL, state.CityName(), RemoteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return func(m mail.Message) (mail.Message, error) { return c.SendPeerMail(m, peerTestToken) }
}

func getThread(t *testing.T, state *fakeState, id string) []mail.Message {
	t.Helper()
	rec := httptest.NewRecorder()
	newTestCityHandler(t, state).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, cityURL(state, "/mail/thread/")+id, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("thread %s in %s: status %d (%s)", id, state.CityName(), rec.Code, rec.Body.String())
	}
	var body MailListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Items
}

// TestPeerMailThreadSpansTwoCities sends wonderland -> ansible, replies
// ansible -> wonderland, and reads the thread in each city.
func TestPeerMailThreadSpansTwoCities(t *testing.T) {
	wonderland := newPeerCity(t, "wonderland", "ansible")
	ansible := newPeerCity(t, "ansible", "wonderland")
	toWonderland := serveCity(t, wonderland)
	toAnsible := serveCity(t, ansible)

	first, err := mail.SendToPeer(wonderland.cityMailProv, toAnsible, "wonderland", mail.Message{From: "worker", To: "worker@ansible", Subject: "hi", Body: "hello"})
	if err != nil {
		t.Fatalf("send wonderland -> ansible: %v", err)
	}
	received, err := ansible.cityMailProv.Get(first.ID)
	if err != nil || received.From != "worker@wonderland" {
		t.Fatalf("ansible holds %+v, %v: want a message from worker@wonderland", received, err)
	}

	reply, err := mail.SendToPeer(ansible.cityMailProv, toWonderland, "ansible", mail.Message{
		From: "worker", To: received.From,
		Subject: mail.ReplySubject("", received.Subject, ""), Body: "back",
		ThreadID: received.ThreadID, ReplyTo: received.ID,
	})
	if err != nil {
		t.Fatalf("reply ansible -> wonderland: %v", err)
	}
	if reply.From != "worker@ansible" || reply.ThreadID != first.ThreadID || reply.ReplyTo != first.ID {
		t.Fatalf("reply %+v: want from worker@ansible in thread %s replying to %s", reply, first.ThreadID, first.ID)
	}

	for _, c := range []struct {
		state *fakeState
		id    string
		want  [][2]string
	}{
		{wonderland, reply.ID, [][2]string{{"worker", "worker@ansible"}, {"worker@ansible", "myrig/worker"}}},
		{ansible, first.ID, [][2]string{{"worker@wonderland", "myrig/worker"}, {"worker", "worker@wonderland"}}},
	} {
		thread := getThread(t, c.state, c.id)
		if len(thread) != len(c.want) {
			t.Fatalf("%s thread = %+v, want %d messages", c.state.CityName(), thread, len(c.want))
		}
		for i, w := range c.want {
			if thread[i].From != w[0] || thread[i].To != w[1] {
				t.Errorf("%s thread[%d] = %s -> %s, want %s -> %s", c.state.CityName(), i, thread[i].From, thread[i].To, w[0], w[1])
			}
		}
		if thread[1].Subject != "Re: hi" || thread[1].ReplyTo != first.ID {
			t.Errorf("%s reply = %+v: want Re: hi answering %s", c.state.CityName(), thread[1], first.ID)
		}
	}
}
