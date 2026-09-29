package config

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// DefaultMailArchiveReadAfter is how long a read message stays in its
// recipient's inbox before the mail sweep archives it when [mail]
// archive_read_after is unset.
const DefaultMailArchiveReadAfter = time.Hour

// MailRetentionWindows are the read-mail retention windows that apply to one
// recipient. A zero window means never: zero ArchiveReadAfter leaves read mail
// in the inbox, zero RetentionTTL never purges it.
type MailRetentionWindows struct {
	ArchiveReadAfter time.Duration
	RetentionTTL     time.Duration
}

// MailRetentionPolicy answers which read-mail retention windows apply to a
// recipient address. The zero value never archives and never purges.
type MailRetentionPolicy struct {
	defaults  MailRetentionWindows
	overrides []mailRecipientOverride
}

// mailRecipientOverride is one validated [[mail.recipient]] entry. A nil
// window falls back to the policy defaults.
type mailRecipientOverride struct {
	match            string
	archiveReadAfter *time.Duration
	retentionTTL     *time.Duration
}

// NewMailRetentionPolicy returns a policy that applies windows to every
// recipient.
func NewMailRetentionPolicy(windows MailRetentionWindows) MailRetentionPolicy {
	return MailRetentionPolicy{defaults: windows}
}

// RetentionPolicy builds the per-recipient read-mail retention policy from the
// [mail] section. It rejects the same values ValidateMailRetention rejects at
// config load, except the city-wide retention_ttl: an unparseable one disables
// the city-wide purge, as it always has, and is reported by ValidateDurations as
// a load warning rather than an error.
func (m MailConfig) RetentionPolicy() (MailRetentionPolicy, error) {
	retentionTTL, err := m.RetentionTTLDuration()
	if err != nil {
		retentionTTL = 0
	}
	archiveReadAfter := DefaultMailArchiveReadAfter
	if raw := strings.TrimSpace(m.ArchiveReadAfter); raw != "" {
		archiveReadAfter, err = parseMailRetentionWindow("[mail]", "archive_read_after", raw)
		if err != nil {
			return MailRetentionPolicy{}, err
		}
	}
	policy := MailRetentionPolicy{
		defaults: MailRetentionWindows{ArchiveReadAfter: archiveReadAfter, RetentionTTL: retentionTTL},
	}
	seen := make(map[string]bool, len(m.Recipients))
	for i, entry := range m.Recipients {
		override, err := parseMailRecipientOverride(i, entry)
		if err != nil {
			return MailRetentionPolicy{}, err
		}
		if seen[override.match] {
			return MailRetentionPolicy{}, fmt.Errorf("[[mail.recipient]] #%d: duplicate match %q", i+1, override.match)
		}
		seen[override.match] = true
		policy.overrides = append(policy.overrides, override)
	}
	return policy, nil
}

func parseMailRecipientOverride(index int, entry MailRecipientRetention) (mailRecipientOverride, error) {
	match := strings.TrimSpace(entry.Match)
	context := fmt.Sprintf("[[mail.recipient]] #%d", index+1)
	if match == "" {
		return mailRecipientOverride{}, fmt.Errorf("%s: match must not be empty", context)
	}
	if _, err := path.Match(match, ""); err != nil {
		return mailRecipientOverride{}, fmt.Errorf("%s: match %q is not a valid glob: %w", context, match, err)
	}
	context = fmt.Sprintf("%s (match %q)", context, match)
	override := mailRecipientOverride{match: match}
	if raw := strings.TrimSpace(entry.ArchiveReadAfter); raw != "" {
		d, err := parseMailRetentionWindow(context, "archive_read_after", raw)
		if err != nil {
			return mailRecipientOverride{}, err
		}
		override.archiveReadAfter = &d
	}
	if raw := strings.TrimSpace(entry.RetentionTTL); raw != "" {
		d, err := parseMailRetentionWindow(context, "retention_ttl", raw)
		if err != nil {
			return mailRecipientOverride{}, err
		}
		override.retentionTTL = &d
	}
	return override, nil
}

func parseMailRetentionWindow(context, field, raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %s %q is not a valid Go duration: %w", context, field, raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("%s %s must not be negative: got %q", context, field, raw)
	}
	return d, nil
}

// ValidateMailRetention rejects [mail] archive_read_after and
// [[mail.recipient]] entries that would otherwise fail at sweep time: a bad or
// negative duration, an empty or invalid match glob, or a duplicate match.
func ValidateMailRetention(cfg *City, source string) error {
	if cfg == nil {
		return nil
	}
	if _, err := cfg.Mail.RetentionPolicy(); err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	return nil
}

// For returns the retention windows for a recipient address as the mail bead
// stores it. The first [[mail.recipient]] entry whose match equals or globs the
// address supplies its windows; a window the entry leaves unset comes from
// [mail].
func (p MailRetentionPolicy) For(recipient string) MailRetentionWindows {
	for _, override := range p.overrides {
		if override.matches(recipient) {
			return p.resolve(override)
		}
	}
	return p.defaults
}

func (o mailRecipientOverride) matches(recipient string) bool {
	if o.match == recipient {
		return true
	}
	ok, err := path.Match(o.match, recipient)
	return err == nil && ok
}

// ShortestArchiveReadAfter returns the shortest non-zero archive window any
// recipient can resolve to, or zero when no recipient's read mail is archived.
// The sweep lists candidates with this cutoff and filters each by its own
// recipient's window.
func (p MailRetentionPolicy) ShortestArchiveReadAfter() time.Duration {
	return p.shortest(func(w MailRetentionWindows) time.Duration { return w.ArchiveReadAfter })
}

// ShortestRetentionTTL returns the shortest non-zero purge window any
// recipient can resolve to, or zero when no recipient's read mail is purged.
func (p MailRetentionPolicy) ShortestRetentionTTL() time.Duration {
	return p.shortest(func(w MailRetentionWindows) time.Duration { return w.RetentionTTL })
}

func (p MailRetentionPolicy) shortest(window func(MailRetentionWindows) time.Duration) time.Duration {
	shortest := window(p.defaults)
	for _, override := range p.overrides {
		d := window(p.resolve(override))
		if d > 0 && (shortest == 0 || d < shortest) {
			shortest = d
		}
	}
	return shortest
}

// resolve returns the windows a recipient matched by override receives.
func (p MailRetentionPolicy) resolve(override mailRecipientOverride) MailRetentionWindows {
	windows := p.defaults
	if override.archiveReadAfter != nil {
		windows.ArchiveReadAfter = *override.archiveReadAfter
	}
	if override.retentionTTL != nil {
		windows.RetentionTTL = *override.retentionTTL
	}
	return windows
}

// ArchiveVariesByRecipient reports whether some recipient can resolve to an
// archive window other than the [mail] one, so a sweep may skip candidates
// its cutoff selected.
func (p MailRetentionPolicy) ArchiveVariesByRecipient() bool {
	for _, override := range p.overrides {
		if override.archiveReadAfter != nil && *override.archiveReadAfter != p.defaults.ArchiveReadAfter {
			return true
		}
	}
	return false
}
