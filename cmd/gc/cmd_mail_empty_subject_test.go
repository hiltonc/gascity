package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/mail/beadmail"
)

// TestMailSendEmptySubjectStaysEmpty pins ga-f4rl for both CLI shapes that
// send no subject: `-s "" -m <body>` and `-m <body>` (args [to, "", body]),
// and the positional `<to> <body>` (args [to, body]). The send JSON reports
// `subject: ""` and the stored message reads back with an empty subject.
func TestMailSendEmptySubjectStaysEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "empty -s with -m", args: []string{"mayor", "", "status: all green"}},
		{name: "positional body", args: []string{"mayor", "status: all green"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mp := beadmail.New(beads.NewMemStore())
			recipients := map[string]bool{"human": true, "mayor": true}

			var stdout, stderr bytes.Buffer
			code := doMailSendJSON(mp, events.Discard, recipients, "human", tc.args, nil, "", true, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("doMailSendJSON = %d, want 0; stderr: %s", code, stderr.String())
			}
			var got struct {
				Message map[string]any `json:"message"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
			}
			subject, ok := got.Message["subject"]
			if !ok || subject != "" {
				t.Fatalf("send JSON message.subject = %#v (present=%v), want \"\"\n%s", subject, ok, stdout.String())
			}
			validateJSONResultSchema(t, []string{"mail", "send"}, stdout.Bytes())

			inbox, err := mp.Inbox("mayor")
			if err != nil {
				t.Fatal(err)
			}
			if len(inbox) != 1 || inbox[0].Subject != "" || inbox[0].Body != "status: all green" {
				t.Fatalf("inbox = %+v, want one message with an empty subject and the body", inbox)
			}
		})
	}
}

// TestMailInboxShowsPlaceholderOnlyInHumanOutput checks that the inbox table
// labels a subjectless message "(no subject)" while --json reports "".
func TestMailInboxShowsPlaceholderOnlyInHumanOutput(t *testing.T) {
	mp := beadmail.New(beads.NewMemStore())
	if _, err := mp.Send("human", "mayor", "", "status: all green"); err != nil {
		t.Fatal(err)
	}
	target := resolvedMailTarget{display: "mayor", recipients: []string{"mayor"}}

	var human, stderr bytes.Buffer
	if code := doMailInboxTargetWithJSON(mp, target, false, &human, &stderr); code != 0 {
		t.Fatalf("inbox = %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(human.String(), mailNoSubjectPlaceholder) {
		t.Errorf("human inbox missing %q:\n%s", mailNoSubjectPlaceholder, human.String())
	}

	var jsonOut bytes.Buffer
	if code := doMailInboxTargetWithJSON(mp, target, true, &jsonOut, &stderr); code != 0 {
		t.Fatalf("inbox --json = %d; stderr: %s", code, stderr.String())
	}
	if strings.Contains(jsonOut.String(), mailNoSubjectPlaceholder) {
		t.Errorf("inbox JSON carries the human placeholder:\n%s", jsonOut.String())
	}
	var got mailInboxJSONResult
	if err := json.Unmarshal(bytes.TrimSpace(jsonOut.Bytes()), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, jsonOut.String())
	}
	if len(got.Messages) != 1 || got.Messages[0].Subject != "" {
		t.Fatalf("inbox JSON messages = %+v, want one with subject \"\"", got.Messages)
	}
	if !strings.Contains(jsonOut.String(), `"subject":""`) {
		t.Errorf("inbox JSON omits subject:\n%s", jsonOut.String())
	}
	validateJSONResultSchema(t, []string{"mail", "inbox"}, jsonOut.Bytes())
}

// TestMailReadJSONReportsEmptySubject checks `gc mail read --json` for a
// subjectless message.
func TestMailReadJSONReportsEmptySubject(t *testing.T) {
	mp := beadmail.New(beads.NewMemStore())
	sent, err := mp.Send("human", "mayor", "", "status: all green")
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := doMailReadWithJSON(mp, events.Discard, []string{sent.ID}, true, &stdout, &stderr); code != 0 {
		t.Fatalf("read --json = %d; stderr: %s", code, stderr.String())
	}
	var got mailMessageJSONResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if got.Message.Subject != "" || got.Message.Body != "status: all green" {
		t.Fatalf("read JSON message = %+v, want empty subject and the body", got.Message)
	}
	validateJSONResultSchema(t, []string{"mail", "read"}, stdout.Bytes())
}

// TestMailThreadShowsPlaceholderForEmptySubject checks the thread table.
func TestMailThreadShowsPlaceholderForEmptySubject(t *testing.T) {
	mp := beadmail.New(beads.NewMemStore())
	sent, err := mp.Send("human", "mayor", "", "status: all green")
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := doMailThread(mp, []string{sent.ID}, &stdout, &stderr); code != 0 {
		t.Fatalf("thread = %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), mailNoSubjectPlaceholder) {
		t.Errorf("thread table missing %q:\n%s", mailNoSubjectPlaceholder, stdout.String())
	}
}
