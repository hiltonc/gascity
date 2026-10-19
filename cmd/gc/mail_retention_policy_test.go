package main

import (
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/mail"
)

// humanNeverArchivedConfig is a city whose read mail is archived after an hour
// and purged after a week, except mail to "human", which stays until archived by
// hand and is never purged.
func humanNeverArchivedConfig() *config.City {
	cfg := &config.City{}
	cfg.Daemon.WispGCInterval = "5m"
	cfg.Mail = config.MailConfig{
		ArchiveReadAfter: "1h",
		RetentionTTL:     "168h",
		Recipients: []config.MailRecipientRetention{
			{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
		},
	}
	return cfg
}

func addressedMailSeed(id, recipient string, createdAt time.Time) beads.Bead {
	b := mailSeed(id, createdAt)
	b.Assignee = recipient
	return b
}

// TestMailRetentionWithoutNewConfigMatchesToday pins the behavior of a city
// that sets none of the per-recipient knobs, which is upstream's: every
// recipient's read mail is archived after 60 minutes, or after retention_ttl
// when that is set (the sweep has honored it since #5933), and it is purged
// only as retention_ttl says.
func TestMailRetentionWithoutNewConfigMatchesToday(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *config.City
		wantArchive   time.Duration
		wantRetention time.Duration
	}{
		{"nil config", nil, 60 * time.Minute, 0},
		{"no [mail] section", &config.City{}, 60 * time.Minute, 0},
		{"retention_ttl set", &config.City{Mail: config.MailConfig{RetentionTTL: "168h"}}, 168 * time.Hour, 168 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := mailRetentionPolicyForConfig(tt.cfg)
			if err != nil {
				t.Fatalf("mailRetentionPolicyForConfig: %v", err)
			}
			want := config.MailRetentionWindows{ArchiveReadAfter: tt.wantArchive, RetentionTTL: tt.wantRetention}
			for _, recipient := range []string{"human", "gascity/worker", ""} {
				if got := policy.For("", recipient); got != want {
					t.Errorf("For(%q) = %+v, want %+v", recipient, got, want)
				}
			}

			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			store := beads.NewMemStoreFrom(100, []beads.Bead{
				addressedMailSeed("human-past", "human", now.Add(-tt.wantArchive-time.Second)),
				addressedMailSeed("worker-past", "gascity/worker", now.Add(-tt.wantArchive-time.Second)),
				addressedMailSeed("worker-within", "gascity/worker", now.Add(-tt.wantArchive+time.Second)),
			}, nil)
			result, err := sweepStaleNudgeMail(beads.NudgesStore{Store: store}, beads.MailStore{Store: store}, nil, now, nudgeMailSweepDefaultNudgeTTL, policy, nudgeMailSweepDefaultUnreadMailTTL, nudgeMailSweepWatchdogCloseBudget)
			if err != nil {
				t.Fatalf("sweepStaleNudgeMail: %v", err)
			}
			if got := strings.Join(result.MailClosedIDs, ","); got != "human-past,worker-past" {
				t.Fatalf("archived = %s, want human-past,worker-past", got)
			}
		})
	}
}

func TestSweepStaleNudgeMail_PerRecipientPolicy(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	policy, err := mailRetentionPolicyForConfig(humanNeverArchivedConfig())
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	store := beads.NewMemStoreFrom(100, []beads.Bead{
		addressedMailSeed("human-old", "human", now.Add(-72*time.Hour)),
		addressedMailSeed("worker-old", "gascity/worker", now.Add(-2*time.Hour)),
		addressedMailSeed("worker-recent", "gascity/worker", now.Add(-10*time.Minute)),
	}, nil)
	nudges, mails := beads.NudgesStore{Store: store}, beads.MailStore{Store: store}

	counts, err := countStaleNudgeMail(nudges, mails, nil, now, nudgeMailSweepDefaultNudgeTTL, policy, nudgeMailSweepDefaultUnreadMailTTL, 0)
	if err != nil {
		t.Fatalf("countStaleNudgeMail: %v", err)
	}
	result, err := sweepStaleNudgeMail(nudges, mails, nil, now, nudgeMailSweepDefaultNudgeTTL, policy, nudgeMailSweepDefaultUnreadMailTTL, 0)
	if err != nil {
		t.Fatalf("sweepStaleNudgeMail: %v", err)
	}
	if got := strings.Join(result.MailClosedIDs, ","); got != "worker-old" {
		t.Fatalf("archived = %s, want worker-old", got)
	}
	if counts.MailClosed != result.MailClosed {
		t.Fatalf("dry-run mail count = %d, sweep archived %d", counts.MailClosed, result.MailClosed)
	}
}

func TestSweepNudgeMailPolicy(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seed := []beads.Bead{
		addressedMailSeed("human-old", "human", now.Add(-72*time.Hour)),
		addressedMailSeed("worker-mid", "gascity/worker", now.Add(-30*time.Minute)),
		addressedMailSeed("worker-old", "gascity/worker", now.Add(-2*time.Hour)),
	}
	tests := []struct {
		name       string
		mailTTL    time.Duration
		mailTTLSet bool
		want       string
	}{
		{"city policy without --mail-ttl", 0, false, "worker-old"},
		{"--mail-ttl replaces the whole policy", 20 * time.Minute, true, "human-old,worker-old,worker-mid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := sweepNudgeMailPolicy(humanNeverArchivedConfig(), tt.mailTTL, tt.mailTTLSet)
			if err != nil {
				t.Fatalf("sweepNudgeMailPolicy: %v", err)
			}
			store := beads.NewMemStoreFrom(100, seed, nil)
			result, err := sweepStaleNudgeMail(beads.NudgesStore{Store: store}, beads.MailStore{Store: store}, nil, now, nudgeMailSweepDefaultNudgeTTL, policy, nudgeMailSweepDefaultUnreadMailTTL, 0)
			if err != nil {
				t.Fatalf("sweepStaleNudgeMail: %v", err)
			}
			if got := strings.Join(result.MailClosedIDs, ","); got != tt.want {
				t.Fatalf("archived = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSweepNudgeMailPolicyRejectsInvalidCityPolicy(t *testing.T) {
	cfg := &config.City{Mail: config.MailConfig{ArchiveReadAfter: "soon"}}
	if _, err := sweepNudgeMailPolicy(cfg, 0, false); err == nil {
		t.Fatal("sweepNudgeMailPolicy accepted an invalid [mail] archive_read_after")
	}
	if _, err := sweepNudgeMailPolicy(cfg, time.Hour, true); err != nil {
		t.Fatalf("an explicit --mail-ttl should not consult the city policy: %v", err)
	}
}

func TestWispGCForConfigPurgesPerRecipient(t *testing.T) {
	now := time.Now()
	readWispTo := func(id, recipient string, createdAt time.Time) beads.Bead {
		b := makeGCMessageWisp(id, createdAt, map[string]string{mail.ReadMetadataKey: "true"})
		b.Assignee = recipient
		return b
	}
	store := newGCStore([]beads.Bead{
		readWispTo("worker-old", "gascity/worker", now.Add(-200*time.Hour)),
		readWispTo("human-old", "human", now.Add(-2000*time.Hour)),
	})

	wg := newWispGCForConfig(humanNeverArchivedConfig())
	if wg == nil {
		t.Fatal("newWispGCForConfig returned nil for a city with a purge window")
	}
	if _, err := wg.runGC(beads.GraphStore{Store: store}, beads.SessionStore{}, beads.MailStore{Store: store}, now); err != nil {
		t.Fatalf("runGC: %v", err)
	}
	assertDeletedIDs(t, store.deletedIDs, "worker-old")
	if _, err := store.Get("human-old"); err != nil {
		t.Fatalf("human-old should be preserved: %v", err)
	}
}

func TestWispGCForConfigRunsWhenOnlyARecipientPurges(t *testing.T) {
	cfg := &config.City{}
	cfg.Daemon.WispGCInterval = "5m"
	cfg.Mail.Recipients = []config.MailRecipientRetention{{Match: "*/witness", RetentionTTL: "24h"}}

	wg := newWispGCForConfig(cfg)
	if wg == nil {
		t.Fatal("newWispGCForConfig returned nil; a recipient retention_ttl must enable the mail purge")
	}
	if got := wg.(*memoryWispGC).mailRetention.For("", "gascity/witness").RetentionTTL; got != 24*time.Hour {
		t.Fatalf("witness retention = %v, want 24h", got)
	}
}

// humanSenderKeptConfig is a city whose read mail is archived after an hour
// and purged after a week, except mail from "human", which no rule addressed
// to its recipients shortens.
func humanSenderKeptConfig() *config.City {
	cfg := &config.City{}
	cfg.Daemon.WispGCInterval = "5m"
	cfg.Mail = config.MailConfig{
		ArchiveReadAfter: "1h",
		RetentionTTL:     "168h",
		Senders: []config.MailSenderRetention{
			{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
		},
	}
	return cfg
}

func sentMailSeed(id, sender, recipient string, createdAt time.Time) beads.Bead {
	b := addressedMailSeed(id, recipient, createdAt)
	b.From = sender
	return b
}

func TestSweepStaleNudgeMail_PerSenderPolicy(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	policy, err := mailRetentionPolicyForConfig(humanSenderKeptConfig())
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	store := beads.NewMemStoreFrom(100, []beads.Bead{
		sentMailSeed("from-human-old", "human", "gascity/worker", now.Add(-72*time.Hour)),
		sentMailSeed("from-agent-old", "gascity/other", "gascity/worker", now.Add(-2*time.Hour)),
		sentMailSeed("from-agent-recent", "gascity/other", "gascity/worker", now.Add(-10*time.Minute)),
	}, nil)
	nudges, mails := beads.NudgesStore{Store: store}, beads.MailStore{Store: store}

	counts, err := countStaleNudgeMail(nudges, mails, nil, now, nudgeMailSweepDefaultNudgeTTL, policy, nudgeMailSweepDefaultUnreadMailTTL, 0)
	if err != nil {
		t.Fatalf("countStaleNudgeMail: %v", err)
	}
	result, err := sweepStaleNudgeMail(nudges, mails, nil, now, nudgeMailSweepDefaultNudgeTTL, policy, nudgeMailSweepDefaultUnreadMailTTL, 0)
	if err != nil {
		t.Fatalf("sweepStaleNudgeMail: %v", err)
	}
	if got := strings.Join(result.MailClosedIDs, ","); got != "from-agent-old" {
		t.Fatalf("archived = %s, want from-agent-old", got)
	}
	if counts.MailClosed != result.MailClosed {
		t.Fatalf("dry-run mail count = %d, sweep archived %d", counts.MailClosed, result.MailClosed)
	}
}

func TestSweepNudgeMailPolicyMailTTLOverridesSenderRules(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	policy, err := sweepNudgeMailPolicy(humanSenderKeptConfig(), 20*time.Minute, true)
	if err != nil {
		t.Fatalf("sweepNudgeMailPolicy: %v", err)
	}
	store := beads.NewMemStoreFrom(100, []beads.Bead{
		sentMailSeed("from-human-old", "human", "gascity/worker", now.Add(-72*time.Hour)),
		sentMailSeed("from-agent-mid", "gascity/other", "gascity/worker", now.Add(-30*time.Minute)),
	}, nil)
	result, err := sweepStaleNudgeMail(beads.NudgesStore{Store: store}, beads.MailStore{Store: store}, nil, now, nudgeMailSweepDefaultNudgeTTL, policy, nudgeMailSweepDefaultUnreadMailTTL, 0)
	if err != nil {
		t.Fatalf("sweepStaleNudgeMail: %v", err)
	}
	if got := strings.Join(result.MailClosedIDs, ","); got != "from-human-old,from-agent-mid" {
		t.Fatalf("archived = %s, want from-human-old,from-agent-mid", got)
	}
}

func TestWispGCForConfigPurgesPerSender(t *testing.T) {
	now := time.Now()
	readWisp := func(id, sender string, createdAt time.Time) beads.Bead {
		b := makeGCMessageWisp(id, createdAt, map[string]string{mail.ReadMetadataKey: "true"})
		b.From = sender
		b.Assignee = "gascity/worker"
		return b
	}
	store := newGCStore([]beads.Bead{
		readWisp("from-agent-old", "gascity/other", now.Add(-200*time.Hour)),
		readWisp("from-human-old", "human", now.Add(-2000*time.Hour)),
	})

	wg := newWispGCForConfig(humanSenderKeptConfig())
	if wg == nil {
		t.Fatal("newWispGCForConfig returned nil for a city with a purge window")
	}
	if _, err := wg.runGC(beads.GraphStore{Store: store}, beads.SessionStore{}, beads.MailStore{Store: store}, now); err != nil {
		t.Fatalf("runGC: %v", err)
	}
	assertDeletedIDs(t, store.deletedIDs, "from-agent-old")
	if _, err := store.Get("from-human-old"); err != nil {
		t.Fatalf("from-human-old should be preserved: %v", err)
	}
}

func TestWispGCForConfigRunsWhenOnlyASenderPurges(t *testing.T) {
	cfg := &config.City{}
	cfg.Daemon.WispGCInterval = "5m"
	cfg.Mail.Senders = []config.MailSenderRetention{{Match: "ops/*", RetentionTTL: "24h"}}

	wg := newWispGCForConfig(cfg)
	if wg == nil {
		t.Fatal("newWispGCForConfig returned nil; a sender retention_ttl must enable the mail purge")
	}
	if got := wg.(*memoryWispGC).mailRetention.For("ops/deploy", "gascity/worker").RetentionTTL; got != 24*time.Hour {
		t.Fatalf("ops sender retention = %v, want 24h", got)
	}
}
