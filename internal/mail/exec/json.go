package exec //nolint:revive // internal package, always imported with alias

import (
	"encoding/json"
	"fmt"

	"github.com/gastownhall/gascity/internal/mail"
)

// sendInput is the JSON wire format sent to the script's stdin on Send.
type sendInput struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// replyInput is the JSON wire format sent to the script's stdin on Reply.
type replyInput struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// countOutput is the JSON wire format returned by the script on Count.
type countOutput struct {
	Total  int `json:"total"`
	Unread int `json:"unread"`
}

// marshalSendInput encodes the send payload as JSON.
func marshalSendInput(from, subject, body string) ([]byte, error) {
	return json.Marshal(sendInput{From: from, Subject: subject, Body: body})
}

// marshalReplyInput encodes the reply payload as JSON.
func marshalReplyInput(from, subject, body string) ([]byte, error) {
	return json.Marshal(replyInput{From: from, Subject: subject, Body: body})
}

// unmarshalMessage decodes a single Message from JSON. A message the script
// returns without a status is open.
func unmarshalMessage(data string) (mail.Message, error) {
	var m mail.Message
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return mail.Message{}, err
	}
	if err := normalizeStatus(&m, mail.StatusOpen); err != nil {
		return mail.Message{}, err
	}
	return m, nil
}

// unmarshalMessages decodes a JSON array of open-view Messages. A message the
// script returns without a status is open.
func unmarshalMessages(data string) ([]mail.Message, error) {
	return unmarshalMessagesWithStatus(data, mail.StatusOpen)
}

// unmarshalMessagesWithStatus decodes a JSON array of Messages, filling a
// missing status with defaultStatus.
func unmarshalMessagesWithStatus(data, defaultStatus string) ([]mail.Message, error) {
	var msgs []mail.Message
	if err := json.Unmarshal([]byte(data), &msgs); err != nil {
		return nil, err
	}
	for i := range msgs {
		if err := normalizeStatus(&msgs[i], defaultStatus); err != nil {
			return nil, err
		}
	}
	return msgs, nil
}

// normalizeStatus fills a missing message status with defaultStatus and
// rejects any value outside the mail status vocabulary, so a script bug
// surfaces here instead of as an invalid status on the API wire.
func normalizeStatus(m *mail.Message, defaultStatus string) error {
	switch m.Status {
	case "":
		m.Status = defaultStatus
	case mail.StatusOpen, mail.StatusClosed:
	default:
		return fmt.Errorf("message %q has status %q; want %q or %q", m.ID, m.Status, mail.StatusOpen, mail.StatusClosed)
	}
	if m.Status == mail.StatusOpen {
		m.ClosedAt = nil
	}
	return nil
}

// unmarshalCount decodes the count output JSON.
func unmarshalCount(data string) (int, int, error) {
	var c countOutput
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		return 0, 0, err
	}
	return c.Total, c.Unread, nil
}
