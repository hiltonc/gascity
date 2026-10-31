package api

import (
	"context"
	"crypto/subtle"
	"fmt"

	"github.com/gastownhall/gascity/internal/api/apierr"
	"github.com/gastownhall/gascity/internal/mail"
)

// authorizeMailSender admits a sender of POST /mail. An agent@city sender of
// another city must be a [[mail.peer]] presenting that peer's shared token; any
// other sender is one of this city's identities.
func (s *Server) authorizeMailSender(ctx context.Context, from, peerToken string) error {
	if addr, ok := mail.ParsePeerAddress(from); ok && addr.City != s.state.CityName() {
		return s.authorizeMailPeer(addr.City, peerToken)
	}
	return s.refuseUnconfiguredMailSender(ctx, from)
}

func (s *Server) authorizeMailPeer(city, token string) error {
	peer, ok := s.state.Config().Mail.Peer(city)
	if !ok {
		return apierr.Forbidden.Msg(fmt.Sprintf("city %q is not a [[mail.peer]] of this city", city))
	}
	want, err := peer.Token(s.state.CityPath())
	if err != nil {
		return apierr.Internal.Msg(err.Error())
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		return apierr.Forbidden.Msg(fmt.Sprintf("X-GC-Mail-Peer-Token does not match the token of mail peer %q", city))
	}
	return nil
}

// sendMailInThread sends through the provider's [mail.ThreadedSender] when the
// message joins a thread, and through Send otherwise.
func sendMailInThread(mp mail.Provider, from, to, subject, body, threadID, replyTo string) (mail.Message, error) {
	if threadID == "" && replyTo == "" {
		sent, err := mp.Send(from, to, subject, body)
		if err != nil {
			return mail.Message{}, apierr.Internal.Msg(err.Error())
		}
		return sent, nil
	}
	threaded, ok := mp.(mail.ThreadedSender)
	if !ok {
		return mail.Message{}, apierr.InvalidRequest.Msg("this city's mail provider cannot place a message in an existing thread")
	}
	sent, err := threaded.SendInThread(from, to, subject, body, threadID, replyTo)
	if err != nil {
		return mail.Message{}, apierr.Internal.Msg(err.Error())
	}
	return sent, nil
}
