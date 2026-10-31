package api

import (
	"context"
	"fmt"

	"github.com/gastownhall/gascity/internal/api/genclient"
	"github.com/gastownhall/gascity/internal/mail"
)

// SendPeerMail creates m in this client's city as a mail peer holding token,
// via POST /v0/city/{cityName}/mail. It is a [mail.PeerSendFunc] once token is
// bound:
//
//	send := func(m mail.Message) (mail.Message, error) { return c.SendPeerMail(m, token) }
func (c *Client) SendPeerMail(m mail.Message, token string) (mail.Message, error) {
	if err := c.requireCityScope(); err != nil {
		return mail.Message{}, err
	}
	body := genclient.SendMailJSONRequestBody{To: m.To, Subject: m.Subject}
	setStrPtr(&body.From, m.From)
	setStrPtr(&body.Body, m.Body)
	setStrPtr(&body.ThreadId, m.ThreadID)
	setStrPtr(&body.ReplyTo, m.ReplyTo)
	params := &genclient.SendMailParams{XGCRequest: "true"}
	setStrPtr(&params.XGCMailPeerToken, token)
	resp, err := c.cw.SendMailWithResponse(context.Background(), c.cityName, params, body)
	if err := checkMutation(resp, err); err != nil {
		return mail.Message{}, err
	}
	if resp.JSON201 == nil {
		return mail.Message{}, fmt.Errorf("API returned %d with no body", resp.StatusCode())
	}
	return mailMessageFromGen(*resp.JSON201), nil
}
