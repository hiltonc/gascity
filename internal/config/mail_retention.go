package config

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// DefaultMailArchiveReadAfter is how long a read message stays in its
// recipient's inbox before the mail sweep archives it when [mail]
// archive_read_after and retention_ttl are both unset.
const DefaultMailArchiveReadAfter = time.Hour

// MailRetentionWindows are the read-mail retention windows that apply to one
// message. A zero window means never: zero ArchiveReadAfter leaves read mail
// in the inbox, zero RetentionTTL never purges it.
type MailRetentionWindows struct {
	ArchiveReadAfter time.Duration
	RetentionTTL     time.Duration
}

// MailRetentionPolicy answers which read-mail retention windows apply to a
// message from its sender and recipient addresses. The zero value never
// archives and never purges.
type MailRetentionPolicy struct {
	defaults   MailRetentionWindows
	recipients []mailRetentionOverride
	senders    []mailRetentionOverride
}

// mailRetentionRule is the shape [[mail.recipient]] and [[mail.sender]]
// entries share, so one parser validates both.
type mailRetentionRule struct {
	Match            string
	ArchiveReadAfter string
	RetentionTTL     string
}

// mailRetentionOverride is one validated [[mail.recipient]] or [[mail.sender]]
// entry. A nil window is one the entry leaves unset.
type mailRetentionOverride struct {
	match            string
	archiveReadAfter *time.Duration
	retentionTTL     *time.Duration
}

// NewMailRetentionPolicy returns a policy that applies windows to every
// message, whatever its sender or recipient.
func NewMailRetentionPolicy(windows MailRetentionWindows) MailRetentionPolicy {
	return MailRetentionPolicy{defaults: windows}
}

// RetentionPolicy builds the per-sender and per-recipient read-mail retention
// policy from the [mail] section. It rejects the same values ValidateMailRetention rejects at
// config load, except the city-wide retention_ttl: an unparseable one disables
// the city-wide purge, as it always has, and is reported by ValidateDurations as
// a load warning rather than an error.
func (m MailConfig) RetentionPolicy() (MailRetentionPolicy, error) {
	retentionTTL, retentionErr := m.RetentionTTLDuration()
	if retentionErr != nil {
		retentionTTL = 0
	}
	archiveReadAfter := DefaultMailArchiveReadAfter
	if raw := strings.TrimSpace(m.ArchiveReadAfter); raw != "" {
		var err error
		archiveReadAfter, err = parseMailRetentionWindow("[mail]", "archive_read_after", raw)
		if err != nil {
			return MailRetentionPolicy{}, err
		}
	} else if strings.TrimSpace(m.RetentionTTL) != "" && retentionErr == nil {
		// Without archive_read_after, the sweep closes read mail at
		// retention_ttl, "0" included, as upstream does (gastownhall/gascity#5933).
		archiveReadAfter = retentionTTL
	}
	recipientRules := make([]mailRetentionRule, len(m.Recipients))
	for i, entry := range m.Recipients {
		recipientRules[i] = mailRetentionRule(entry)
	}
	recipients, err := parseMailRetentionOverrides("[[mail.recipient]]", recipientRules)
	if err != nil {
		return MailRetentionPolicy{}, err
	}
	senderRules := make([]mailRetentionRule, len(m.Senders))
	for i, entry := range m.Senders {
		senderRules[i] = mailRetentionRule(entry)
	}
	senders, err := parseMailRetentionOverrides("[[mail.sender]]", senderRules)
	if err != nil {
		return MailRetentionPolicy{}, err
	}
	return MailRetentionPolicy{
		defaults:   MailRetentionWindows{ArchiveReadAfter: archiveReadAfter, RetentionTTL: retentionTTL},
		recipients: recipients,
		senders:    senders,
	}, nil
}

// parseMailRetentionOverrides validates the entries of one rule table, named
// by table in its errors, and rejects a match that repeats within it.
func parseMailRetentionOverrides(table string, rules []mailRetentionRule) ([]mailRetentionOverride, error) {
	var overrides []mailRetentionOverride
	seen := make(map[string]bool, len(rules))
	for i, rule := range rules {
		override, err := parseMailRetentionOverride(fmt.Sprintf("%s #%d", table, i+1), rule)
		if err != nil {
			return nil, err
		}
		if seen[override.match] {
			return nil, fmt.Errorf("%s #%d: duplicate match %q", table, i+1, override.match)
		}
		seen[override.match] = true
		overrides = append(overrides, override)
	}
	return overrides, nil
}

func parseMailRetentionOverride(context string, rule mailRetentionRule) (mailRetentionOverride, error) {
	match := strings.TrimSpace(rule.Match)
	if match == "" {
		return mailRetentionOverride{}, fmt.Errorf("%s: match must not be empty", context)
	}
	if _, err := path.Match(match, ""); err != nil {
		return mailRetentionOverride{}, fmt.Errorf("%s: match %q is not a valid glob: %w", context, match, err)
	}
	context = fmt.Sprintf("%s (match %q)", context, match)
	override := mailRetentionOverride{match: match}
	if raw := strings.TrimSpace(rule.ArchiveReadAfter); raw != "" {
		d, err := parseMailRetentionWindow(context, "archive_read_after", raw)
		if err != nil {
			return mailRetentionOverride{}, err
		}
		override.archiveReadAfter = &d
	}
	if raw := strings.TrimSpace(rule.RetentionTTL); raw != "" {
		d, err := parseMailRetentionWindow(context, "retention_ttl", raw)
		if err != nil {
			return mailRetentionOverride{}, err
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

// ValidateMailRetention rejects [mail] archive_read_after, [[mail.recipient]]
// and [[mail.sender]] entries that would otherwise fail at sweep time: a bad
// or negative duration, an empty or invalid match glob, or a match repeated
// within one table.
func ValidateMailRetention(cfg *City, source string) error {
	if cfg == nil {
		return nil
	}
	if _, err := cfg.Mail.RetentionPolicy(); err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	return nil
}

// For returns the retention windows for a message from its sender and
// recipient addresses as the mail bead stores them. The first
// [[mail.recipient]] entry whose match equals or globs the recipient, and the
// first [[mail.sender]] entry whose match equals or globs the sender, are the
// matching entries. Each window is the longest one the matching entries set,
// zero (never) being longest; a window neither sets comes from [mail].
func (p MailRetentionPolicy) For(sender, recipient string) MailRetentionWindows {
	var matched [2]*mailRetentionOverride
	matched[0] = firstMatchingOverride(p.recipients, recipient)
	matched[1] = firstMatchingOverride(p.senders, sender)
	windows := p.defaults
	if d, ok := longestSetWindow(matched[:], func(o *mailRetentionOverride) *time.Duration { return o.archiveReadAfter }); ok {
		windows.ArchiveReadAfter = d
	}
	if d, ok := longestSetWindow(matched[:], func(o *mailRetentionOverride) *time.Duration { return o.retentionTTL }); ok {
		windows.RetentionTTL = d
	}
	return windows
}

func firstMatchingOverride(overrides []mailRetentionOverride, address string) *mailRetentionOverride {
	for i := range overrides {
		if overrides[i].matches(address) {
			return &overrides[i]
		}
	}
	return nil
}

// longestSetWindow returns the longest window the non-nil overrides set, zero
// being longest, and false when none sets one.
func longestSetWindow(overrides []*mailRetentionOverride, window func(*mailRetentionOverride) *time.Duration) (time.Duration, bool) {
	var longest time.Duration
	found := false
	for _, o := range overrides {
		if o == nil {
			continue
		}
		d := window(o)
		if d == nil {
			continue
		}
		if !found || (longest != 0 && (*d == 0 || *d > longest)) {
			longest = *d
		}
		found = true
	}
	return longest, found
}

func (o mailRetentionOverride) matches(address string) bool {
	if o.match == address {
		return true
	}
	ok, err := path.Match(o.match, address)
	return err == nil && ok
}

// ShortestArchiveReadAfter returns the shortest non-zero archive window any
// message can resolve to, or zero when no message's read mail is archived.
// The sweep lists candidates with this cutoff and filters each by its own
// window.
func (p MailRetentionPolicy) ShortestArchiveReadAfter() time.Duration {
	return p.shortest(func(w MailRetentionWindows) time.Duration { return w.ArchiveReadAfter })
}

// ShortestRetentionTTL returns the shortest non-zero purge window any message
// can resolve to, or zero when no message's read mail is purged.
func (p MailRetentionPolicy) ShortestRetentionTTL() time.Duration {
	return p.shortest(func(w MailRetentionWindows) time.Duration { return w.RetentionTTL })
}

// shortest is a lower bound on the window For can return: a message's window
// is the [mail] one or one some matching entry sets.
func (p MailRetentionPolicy) shortest(window func(MailRetentionWindows) time.Duration) time.Duration {
	shortest := window(p.defaults)
	for _, overrides := range [][]mailRetentionOverride{p.recipients, p.senders} {
		for _, override := range overrides {
			d := window(p.resolve(override))
			if d > 0 && (shortest == 0 || d < shortest) {
				shortest = d
			}
		}
	}
	return shortest
}

// resolve returns the windows override alone gives a message.
func (p MailRetentionPolicy) resolve(override mailRetentionOverride) MailRetentionWindows {
	windows := p.defaults
	if override.archiveReadAfter != nil {
		windows.ArchiveReadAfter = *override.archiveReadAfter
	}
	if override.retentionTTL != nil {
		windows.RetentionTTL = *override.retentionTTL
	}
	return windows
}

// ArchiveVariesByMessage reports whether some message can resolve to an
// archive window other than the [mail] one, through its recipient or its
// sender, so a sweep may skip candidates its cutoff selected.
func (p MailRetentionPolicy) ArchiveVariesByMessage() bool {
	for _, overrides := range [][]mailRetentionOverride{p.recipients, p.senders} {
		for _, override := range overrides {
			if override.archiveReadAfter != nil && *override.archiveReadAfter != p.defaults.ArchiveReadAfter {
				return true
			}
		}
	}
	return false
}
