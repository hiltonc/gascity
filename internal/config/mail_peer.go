package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MailPeer is another city whose agents exchange mail with this city's, as
// agent@city. The peer's URL comes from the gc context named after it.
type MailPeer struct {
	// City is the peer's city name, the city part of an agent@city address.
	City string `toml:"city" jsonschema:"required"`
	// TokenFile holds the secret the two cities share: this city sends it
	// with mail to the peer and requires it on mail from the peer, so a
	// caller without it cannot send as one of the peer's agents. A relative
	// path resolves against the city directory.
	TokenFile string `toml:"token_file" jsonschema:"required"`
}

// Peer returns the [[mail.peer]] entry for city.
func (m MailConfig) Peer(city string) (MailPeer, bool) {
	for _, p := range m.Peers {
		if p.City == city {
			return p, true
		}
	}
	return MailPeer{}, false
}

// Token reads the peer's shared secret from TokenFile.
func (p MailPeer) Token(cityPath string) (string, error) {
	if p.TokenFile == "" {
		return "", fmt.Errorf("[[mail.peer]] %q has no token_file", p.City)
	}
	path := p.TokenFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(cityPath, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading mail peer token for %q: %w", p.City, err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("mail peer token file %s for %q is empty", path, p.City)
	}
	return token, nil
}
