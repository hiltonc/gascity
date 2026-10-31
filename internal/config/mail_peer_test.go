package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMailPeerFromCityToml(t *testing.T) {
	cfg, err := Parse([]byte(`
[workspace]
name = "ansible"

[[mail.peer]]
city = "wonderland"
token_file = "secrets/wonderland.token"
`))
	if err != nil {
		t.Fatal(err)
	}
	peer, ok := cfg.Mail.Peer("wonderland")
	if !ok || peer.TokenFile != "secrets/wonderland.token" {
		t.Fatalf("Peer(wonderland) = %+v, %v", peer, ok)
	}
	if _, ok := cfg.Mail.Peer("elsewhere"); ok {
		t.Fatal("an undeclared city is a peer")
	}
}

func TestMailPeerToken(t *testing.T) {
	city := t.TempDir()
	if err := os.MkdirAll(filepath.Join(city, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(city, "secrets", "w.token"), []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := MailPeer{City: "w", TokenFile: "secrets/w.token"}.Token(city)
	if err != nil || got != "s3cret" {
		t.Fatalf("relative token = %q, %v; want s3cret", got, err)
	}
	abs := filepath.Join(city, "secrets", "w.token")
	if got, err := (MailPeer{City: "w", TokenFile: abs}).Token("/elsewhere"); err != nil || got != "s3cret" {
		t.Fatalf("absolute token = %q, %v; want s3cret", got, err)
	}

	if err := os.WriteFile(filepath.Join(city, "empty"), []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, peer := range map[string]MailPeer{
		"no token_file": {City: "w"},
		"missing file":  {City: "w", TokenFile: "absent"},
		"empty file":    {City: "w", TokenFile: "empty"},
	} {
		if _, err := peer.Token(city); err == nil || !strings.Contains(err.Error(), `"w"`) {
			t.Errorf("%s: err = %v, want an error naming the peer", name, err)
		}
	}
}
