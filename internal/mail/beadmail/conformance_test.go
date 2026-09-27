package beadmail

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/mail"
	"github.com/gastownhall/gascity/internal/mail/mailtest"
)

func TestBeadmailConformance(t *testing.T) {
	newProvider := func(_ *testing.T) mail.Provider {
		return New(beads.NewMemStore())
	}
	mailtest.RunProviderTests(t, newProvider)
	mailtest.RunArchiveRetentionTests(t, newProvider)
}
