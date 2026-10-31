package mail //nolint:revive // internal package, always imported qualified

import (
	"errors"
	"fmt"
	"strings"
)

// PeerAddress is an agent in another city, written agent@city.
type PeerAddress struct {
	Agent string
	City  string
}

// ParsePeerAddress splits addr at its last "@". It reports false for an
// address with no city part, which names a recipient in this city.
//
//	ParsePeerAddress("jane@ansible")        // {jane ansible}, true
//	ParsePeerAddress("myrig/jane@ansible")  // {myrig/jane ansible}, true
//	ParsePeerAddress("jane")                // {}, false
func ParsePeerAddress(addr string) (PeerAddress, bool) {
	addr = strings.TrimSpace(addr)
	at := strings.LastIndex(addr, "@")
	if at <= 0 || at == len(addr)-1 {
		return PeerAddress{}, false
	}
	agent, city := addr[:at], addr[at+1:]
	if strings.ContainsAny(city, "/ \t\n") {
		return PeerAddress{}, false
	}
	return PeerAddress{Agent: agent, City: city}, true
}

// String renders the address as agent@city.
func (a PeerAddress) String() string {
	return a.Agent + "@" + a.City
}

// ThreadedSender is an optional [Provider] capability: create a message in a
// given thread. Mail between cities needs it, because the thread a message
// joins was started in the other city's store. An empty threadID starts a new
// thread; replyTo is the ID of the message answered, in whichever city holds it.
type ThreadedSender interface {
	SendInThread(from, to, subject, body, threadID, replyTo string) (Message, error)
}

// PeerSendFunc creates m in the recipient's city and returns the message as
// that city stored it.
type PeerSendFunc func(m Message) (Message, error)

// SendToPeer delivers m to another city and keeps the sender's copy here. m.From
// is the sender in originCity and m.To a [PeerAddress]; ThreadID and ReplyTo
// place m in an existing thread. The delivered message carries the sender as
// agent@originCity. The copy, filed read in local under the delivered thread,
// is what lets each city's thread view show both sides of the conversation.
//
// A non-nil error with a non-empty delivered.ID means the message reached the
// peer but the local copy was not kept.
func SendToPeer(local Provider, send PeerSendFunc, originCity string, m Message) (delivered Message, err error) {
	to, ok := ParsePeerAddress(m.To)
	if !ok {
		return Message{}, fmt.Errorf("recipient %q is not an agent@city address", m.To)
	}
	threaded, ok := local.(ThreadedSender)
	if !ok {
		return Message{}, errors.New("this city's mail provider cannot keep the sender's copy of mail to another city")
	}
	out := Message{
		From:     PeerAddress{Agent: m.From, City: originCity}.String(),
		To:       to.Agent,
		Subject:  DeriveSubject(m.Subject, m.Body),
		Body:     m.Body,
		ThreadID: m.ThreadID,
		ReplyTo:  m.ReplyTo,
	}
	delivered, err = send(out)
	if err != nil {
		return Message{}, err
	}
	sent, err := threaded.SendInThread(m.From, to.String(), out.Subject, m.Body, delivered.ThreadID, m.ReplyTo)
	if err != nil {
		return delivered, fmt.Errorf("delivered %s to %s, but keeping the sender's copy failed: %w", delivered.ID, to, err)
	}
	if err := local.MarkRead(sent.ID); err != nil {
		return delivered, fmt.Errorf("delivered %s to %s, but marking the sender's copy %s read failed: %w", delivered.ID, to, sent.ID, err)
	}
	return delivered, nil
}

// subjectFromBodyLimit is the longest subject derived from a message body.
const subjectFromBodyLimit = 80

// DeriveSubject returns subject, or when it is empty the first line of body,
// shortened to fit a summary line.
func DeriveSubject(subject, body string) string {
	if subject != "" {
		return subject
	}
	line := strings.SplitN(body, "\n", 2)[0]
	if len(line) > subjectFromBodyLimit {
		line = line[:subjectFromBodyLimit-3] + "..."
	}
	return line
}

// ReplySubject returns a non-empty subject for a reply: subject when given,
// else "Re: <original>" (without stacking "Re:"), else the first line of
// body, else "(reply)".
func ReplySubject(subject, originalSubject, body string) string {
	if subject != "" {
		return subject
	}
	if originalSubject != "" {
		if strings.HasPrefix(strings.ToLower(strings.TrimLeft(originalSubject, " \t")), "re:") {
			return originalSubject
		}
		return "Re: " + originalSubject
	}
	if line := DeriveSubject("", body); line != "" {
		return line
	}
	return "(reply)"
}
