package main

import (
	"context"
	"fmt"
	"io"

	"github.com/gastownhall/gascity/internal/api"
	"github.com/gastownhall/gascity/internal/clientcontext"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/mail"
	"github.com/gastownhall/gascity/internal/telemetry"
)

// mailPeer is the city an agent@city address names, reached through the gc
// context of that name and authenticated with this city's [[mail.peer]] token
// for it.
type mailPeer struct {
	client *api.Client
	token  string
}

func (p mailPeer) send(m mail.Message) (mail.Message, error) {
	return p.client.SendPeerMail(m, p.token)
}

// notifier asks a recipient in the peer city for a turn, as --notify does for
// a recipient here; nil when notify is off.
func (p mailPeer) notifier(notify bool) func(recipient, text string) error {
	if !notify {
		return nil
	}
	return p.client.SendSessionMessage
}

// dialMailPeer resolves city to a mailPeer: its URL and transport credentials
// from the gc context named city, its shared token from cfg's [[mail.peer]].
func dialMailPeer(cfg *config.City, cityPath, city string) (mailPeer, error) {
	file, err := clientcontext.Load(DefaultPath())
	if err != nil {
		return mailPeer{}, err
	}
	target, err := targetFromContext(file, city, "mail peer")
	if err != nil {
		return mailPeer{}, fmt.Errorf("unknown city %q: %w", city, err)
	}
	peer, ok := cfg.Mail.Peer(city)
	if !ok {
		return mailPeer{}, fmt.Errorf("city %q is not a mail peer of this city: declare it under [[mail.peer]] in city.toml", city)
	}
	token, err := peer.Token(cityPath)
	if err != nil {
		return mailPeer{}, err
	}
	client, err := buildRemoteWriteClient(target)
	if err != nil {
		return mailPeer{}, err
	}
	return mailPeer{client: client, token: token}, nil
}

// cmdMailSendPeer sends gc mail send's [to, ...] args to the agent@city in to.
func cmdMailSendPeer(mp mail.Provider, cfg *config.City, cityPath, sender string, to mail.PeerAddress, args []string, notify bool, dedupKey string, jsonOut bool, stdout, stderr io.Writer) int {
	if dedupKey != "" {
		fmt.Fprintf(stderr, "gc mail send: --dedup is not supported for mail to %s\n", to) //nolint:errcheck // best-effort stderr
		return 1
	}
	_, subject, body, ok := mailSendParts(args)
	if !ok {
		fmt.Fprintln(stderr, "gc mail send: usage: gc mail send <agent@city> <body>  OR  gc mail send <agent@city> -s <subject> [-m <body>]") //nolint:errcheck // best-effort stderr
		return 1
	}
	peer, err := dialMailPeer(cfg, cityPath, to.City)
	if err != nil {
		fmt.Fprintf(stderr, "gc mail send: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	msg := mail.Message{From: sender, To: to.String(), Subject: subject, Body: body}
	return doMailPeer(mp, openCityRecorderAt(cityPath, stderr), peer.send, peer.notifier(notify), loadedCityName(cfg, cityPath), msg, jsonOut, stdout, stderr)
}

// cmdMailReplyPeer replies to original, a message from the agent@city in from.
func cmdMailReplyPeer(mp mail.Provider, cfg *config.City, cityPath, sender string, original mail.Message, from mail.PeerAddress, subject, body string, notify bool, jsonOut bool, stdout, stderr io.Writer) int {
	peer, err := dialMailPeer(cfg, cityPath, from.City)
	if err != nil {
		fmt.Fprintf(stderr, "gc mail reply: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	msg := mail.Message{
		From:     sender,
		To:       from.String(),
		Subject:  mail.ReplySubject(subject, original.Subject, body),
		Body:     body,
		ThreadID: original.ThreadID,
		ReplyTo:  original.ID,
	}
	return doMailPeer(mp, openCityRecorderAt(cityPath, stderr), peer.send, peer.notifier(notify), loadedCityName(cfg, cityPath), msg, jsonOut, stdout, stderr)
}

// doMailPeer delivers msg, a send or (with ReplyTo set) a reply from a sender
// in localCity to an agent@city, keeps the sender's copy in mp, and notifies
// the recipient through notifyFn when it is non-nil.
func doMailPeer(mp mail.Provider, rec events.Recorder, send mail.PeerSendFunc, notifyFn func(recipient, text string) error, localCity string, msg mail.Message, jsonOut bool, stdout, stderr io.Writer) int {
	cmdName, op, eventType := "gc mail send", "send", events.MailSent
	if msg.ReplyTo != "" {
		cmdName, op, eventType = "gc mail reply", "reply", events.MailReplied
	}
	to, _ := mail.ParsePeerAddress(msg.To)
	delivered, err := mail.SendToPeer(mp, send, localCity, msg)
	telemetry.RecordMailOp(context.Background(), op, err)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", cmdName, err) //nolint:errcheck // best-effort stderr
		if delivered.ID == "" {
			return 1
		}
	}
	recipient := delivered.To
	delivered.To = mail.PeerAddress{Agent: recipient, City: to.City}.String()
	rec.Record(events.Event{
		Type:    eventType,
		Actor:   msg.From,
		Subject: delivered.ID,
		Message: delivered.To,
		Payload: mailEventPayload(&delivered),
	})
	if !jsonOut {
		if msg.ReplyTo != "" {
			fmt.Fprintf(stdout, "Replied to %s — sent message %s to %s\n", msg.ReplyTo, delivered.ID, delivered.To) //nolint:errcheck // best-effort stdout
		} else {
			fmt.Fprintf(stdout, "Sent message %s to %s\n", delivered.ID, delivered.To) //nolint:errcheck // best-effort stdout
		}
	}
	notified := false
	if notifyFn != nil && recipient != "human" {
		if err := notifyFn(recipient, mailNotifyText(delivered.From)); err != nil {
			fmt.Fprintf(stderr, "%s: nudge failed: %v\n", cmdName, err) //nolint:errcheck // best-effort stderr
		} else {
			notified = true
		}
	}
	if jsonOut {
		summary := summarizeMailMessage(delivered)
		return writeCLIJSONLineOrExit(stdout, stderr, cmdName, mailActionResult{SchemaVersion: "1", OK: true, Command: "mail." + op, Action: op, ID: delivered.ID, Message: &summary, Messages: []mailMessageSummary{summary}, Count: intRef(1), Notified: notified})
	}
	return 0
}
