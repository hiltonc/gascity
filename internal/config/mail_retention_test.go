package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/fsys"
)

func TestParseMailRetentionConfig(t *testing.T) {
	cfg, err := Parse([]byte(`
[workspace]
name = "test"

[mail]
archive_read_after = "2h"
retention_ttl = "168h"

[[mail.recipient]]
match = "human"
archive_read_after = "0"
retention_ttl = "0"

[[mail.recipient]]
match = "*/refinery"
retention_ttl = "24h"
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := MailConfig{
		ArchiveReadAfter: "2h",
		RetentionTTL:     "168h",
		Recipients: []MailRecipientRetention{
			{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
			{Match: "*/refinery", RetentionTTL: "24h"},
		},
	}
	got := cfg.Mail
	if got.ArchiveReadAfter != want.ArchiveReadAfter || got.RetentionTTL != want.RetentionTTL {
		t.Fatalf("[mail] = %+v, want %+v", got, want)
	}
	if len(got.Recipients) != len(want.Recipients) {
		t.Fatalf("recipients = %+v, want %+v", got.Recipients, want.Recipients)
	}
	for i := range want.Recipients {
		if got.Recipients[i] != want.Recipients[i] {
			t.Errorf("recipient[%d] = %+v, want %+v", i, got.Recipients[i], want.Recipients[i])
		}
	}
}

func TestMailRetentionPolicyRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name  string
		mail  MailConfig
		wants []string
	}{
		{
			name:  "bad archive_read_after",
			mail:  MailConfig{ArchiveReadAfter: "1d"},
			wants: []string{"[mail]", "archive_read_after", `"1d"`},
		},
		{
			name:  "negative archive_read_after",
			mail:  MailConfig{ArchiveReadAfter: "-1h"},
			wants: []string{"[mail]", "archive_read_after", "negative", `"-1h"`},
		},
		{
			name:  "empty match",
			mail:  MailConfig{Recipients: []MailRecipientRetention{{Match: "  ", RetentionTTL: "1h"}}},
			wants: []string{"[[mail.recipient]] #1", "match must not be empty"},
		},
		{
			name:  "invalid glob",
			mail:  MailConfig{Recipients: []MailRecipientRetention{{Match: "rig/[refinery"}}},
			wants: []string{"[[mail.recipient]] #1", "match", `"rig/[refinery"`, "glob"},
		},
		{
			name: "duplicate match",
			mail: MailConfig{Recipients: []MailRecipientRetention{
				{Match: "human", ArchiveReadAfter: "0"},
				{Match: "human", RetentionTTL: "0"},
			}},
			wants: []string{"[[mail.recipient]] #2", "duplicate match", `"human"`},
		},
		{
			name:  "bad recipient archive_read_after",
			mail:  MailConfig{Recipients: []MailRecipientRetention{{Match: "human", ArchiveReadAfter: "forever"}}},
			wants: []string{`(match "human")`, "archive_read_after", `"forever"`},
		},
		{
			name:  "negative recipient retention_ttl",
			mail:  MailConfig{Recipients: []MailRecipientRetention{{Match: "human", RetentionTTL: "-5m"}}},
			wants: []string{`(match "human")`, "retention_ttl", "negative", `"-5m"`},
		},
		{
			name:  "bad recipient retention_ttl",
			mail:  MailConfig{Recipients: []MailRecipientRetention{{Match: "human", RetentionTTL: "7d"}}},
			wants: []string{`(match "human")`, "retention_ttl", `"7d"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.mail.RetentionPolicy()
			if err == nil {
				t.Fatal("RetentionPolicy() succeeded, want error")
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			validateErr := ValidateMailRetention(&City{Mail: tt.mail}, "city.toml")
			if validateErr == nil || !strings.HasPrefix(validateErr.Error(), "city.toml: ") {
				t.Errorf("ValidateMailRetention = %v, want an error prefixed with the source", validateErr)
			}
		})
	}
}

func TestLoadRejectsInvalidMailRecipient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "city.toml")
	data := `
[workspace]
name = "test"

[[mail.recipient]]
match = "human"
archive_read_after = "1 hour"
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadWithIncludes(fsys.OSFS{}, path)
	if err == nil {
		t.Fatal("LoadWithIncludes succeeded, want the invalid recipient rejected at load")
	}
	if !strings.Contains(err.Error(), "archive_read_after") || !strings.Contains(err.Error(), `"1 hour"`) {
		t.Fatalf("error = %v, want the field and value", err)
	}
}

func TestMailRetentionPolicyFor(t *testing.T) {
	mailCfg := MailConfig{
		ArchiveReadAfter: "2h",
		RetentionTTL:     "168h",
		Recipients: []MailRecipientRetention{
			{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
			{Match: "*/refinery", RetentionTTL: "24h"},
			{Match: "gascity/refinery", ArchiveReadAfter: "5m"},
			{Match: "*/witness", ArchiveReadAfter: "30m"},
		},
	}
	policy, err := mailCfg.RetentionPolicy()
	if err != nil {
		t.Fatalf("RetentionPolicy: %v", err)
	}
	tests := []struct {
		name      string
		recipient string
		want      MailRetentionWindows
	}{
		{"exact match sets both knobs", "human", MailRetentionWindows{ArchiveReadAfter: 0, RetentionTTL: 0}},
		{"exact match does not match a prefix", "human2", MailRetentionWindows{ArchiveReadAfter: 2 * time.Hour, RetentionTTL: 168 * time.Hour}},
		{"glob match, unset archive falls back", "gascity/refinery", MailRetentionWindows{ArchiveReadAfter: 2 * time.Hour, RetentionTTL: 24 * time.Hour}},
		{"glob match, unset retention falls back", "other/witness", MailRetentionWindows{ArchiveReadAfter: 30 * time.Minute, RetentionTTL: 168 * time.Hour}},
		{"glob does not cross a path segment", "a/b/witness", MailRetentionWindows{ArchiveReadAfter: 2 * time.Hour, RetentionTTL: 168 * time.Hour}},
		{"no match falls back to [mail]", "gascity/worker", MailRetentionWindows{ArchiveReadAfter: 2 * time.Hour, RetentionTTL: 168 * time.Hour}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.For("", tt.recipient); got != tt.want {
				t.Fatalf("For(%q) = %+v, want %+v", tt.recipient, got, tt.want)
			}
		})
	}
}

func TestMailRetentionPolicyDefaults(t *testing.T) {
	tests := []struct {
		name string
		mail MailConfig
		want MailRetentionWindows
	}{
		{"no [mail] config", MailConfig{}, MailRetentionWindows{ArchiveReadAfter: time.Hour}},
		{"retention_ttl only sets both windows", MailConfig{RetentionTTL: "168h"}, MailRetentionWindows{ArchiveReadAfter: 168 * time.Hour, RetentionTTL: 168 * time.Hour}},
		{"retention_ttl zero disables archive and purge", MailConfig{RetentionTTL: "0"}, MailRetentionWindows{}},
		{"archive_read_after outranks retention_ttl", MailConfig{ArchiveReadAfter: "2h", RetentionTTL: "168h"}, MailRetentionWindows{ArchiveReadAfter: 2 * time.Hour, RetentionTTL: 168 * time.Hour}},
		{"archive_read_after zero", MailConfig{ArchiveReadAfter: "0"}, MailRetentionWindows{}},
		{"unparseable retention_ttl disables the purge as before", MailConfig{RetentionTTL: "7d"}, MailRetentionWindows{ArchiveReadAfter: time.Hour}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := tt.mail.RetentionPolicy()
			if err != nil {
				t.Fatalf("RetentionPolicy: %v", err)
			}
			for _, recipient := range []string{"human", "gascity/worker", ""} {
				if got := policy.For("", recipient); got != tt.want {
					t.Errorf("For(%q) = %+v, want %+v", recipient, got, tt.want)
				}
			}
			if policy.ArchiveVariesByMessage() {
				t.Error("ArchiveVariesByMessage() = true for a policy with no rule entries")
			}
		})
	}
}

func TestMailRetentionPolicyShortestWindows(t *testing.T) {
	tests := []struct {
		name          string
		mail          MailConfig
		wantArchive   time.Duration
		wantRetention time.Duration
		wantVaries    bool
	}{
		{
			name:        "defaults",
			mail:        MailConfig{},
			wantArchive: time.Hour,
		},
		{
			name: "zero override is not the shortest",
			mail: MailConfig{RetentionTTL: "168h", Recipients: []MailRecipientRetention{
				{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
			}},
			wantArchive:   168 * time.Hour,
			wantRetention: 168 * time.Hour,
			wantVaries:    true,
		},
		{
			name: "shorter override wins",
			mail: MailConfig{Recipients: []MailRecipientRetention{
				{Match: "*/witness", ArchiveReadAfter: "10m", RetentionTTL: "2h"},
			}},
			wantArchive:   10 * time.Minute,
			wantRetention: 2 * time.Hour,
			wantVaries:    true,
		},
		{
			name: "only a recipient purges",
			mail: MailConfig{ArchiveReadAfter: "0", Recipients: []MailRecipientRetention{
				{Match: "human", RetentionTTL: "720h"},
			}},
			wantRetention: 720 * time.Hour,
		},
		{
			name: "override equal to the default does not vary",
			mail: MailConfig{Recipients: []MailRecipientRetention{
				{Match: "human", ArchiveReadAfter: "1h"},
			}},
			wantArchive: time.Hour,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := tt.mail.RetentionPolicy()
			if err != nil {
				t.Fatalf("RetentionPolicy: %v", err)
			}
			if got := policy.ShortestArchiveReadAfter(); got != tt.wantArchive {
				t.Errorf("ShortestArchiveReadAfter() = %v, want %v", got, tt.wantArchive)
			}
			if got := policy.ShortestRetentionTTL(); got != tt.wantRetention {
				t.Errorf("ShortestRetentionTTL() = %v, want %v", got, tt.wantRetention)
			}
			if got := policy.ArchiveVariesByMessage(); got != tt.wantVaries {
				t.Errorf("ArchiveVariesByMessage() = %v, want %v", got, tt.wantVaries)
			}
		})
	}
}

func TestZeroMailRetentionPolicyNeverActs(t *testing.T) {
	var policy MailRetentionPolicy
	if got := policy.For("", "human"); got != (MailRetentionWindows{}) {
		t.Fatalf("zero policy For = %+v, want no windows", got)
	}
	if policy.ShortestArchiveReadAfter() != 0 || policy.ShortestRetentionTTL() != 0 {
		t.Fatal("zero policy reports a non-zero window")
	}
}

func TestParseMailSenderRetentionConfig(t *testing.T) {
	cfg, err := Parse([]byte(`
[workspace]
name = "test"

[[mail.sender]]
match = "human"
archive_read_after = "0"
retention_ttl = "0"

[[mail.sender]]
match = "gascity/*"
retention_ttl = "24h"
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []MailSenderRetention{
		{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
		{Match: "gascity/*", RetentionTTL: "24h"},
	}
	got := cfg.Mail.Senders
	if len(got) != len(want) {
		t.Fatalf("senders = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sender[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestMailRetentionPolicyRejectsInvalidSender(t *testing.T) {
	tests := []struct {
		name  string
		mail  MailConfig
		wants []string
	}{
		{
			name: "duplicate match",
			mail: MailConfig{Senders: []MailSenderRetention{
				{Match: "human", ArchiveReadAfter: "0"},
				{Match: "human", RetentionTTL: "0"},
			}},
			wants: []string{"[[mail.sender]] #2", "duplicate match", `"human"`},
		},
		{
			name:  "empty match",
			mail:  MailConfig{Senders: []MailSenderRetention{{Match: " ", RetentionTTL: "1h"}}},
			wants: []string{"[[mail.sender]] #1", "match must not be empty"},
		},
		{
			name:  "invalid glob",
			mail:  MailConfig{Senders: []MailSenderRetention{{Match: "rig/[x"}}},
			wants: []string{"[[mail.sender]] #1", "match", `"rig/[x"`, "glob"},
		},
		{
			name:  "bad archive_read_after",
			mail:  MailConfig{Senders: []MailSenderRetention{{Match: "human", ArchiveReadAfter: "forever"}}},
			wants: []string{"[[mail.sender]] #1", `(match "human")`, "archive_read_after", `"forever"`},
		},
		{
			name:  "negative retention_ttl",
			mail:  MailConfig{Senders: []MailSenderRetention{{Match: "human", RetentionTTL: "-5m"}}},
			wants: []string{"[[mail.sender]] #1", `(match "human")`, "retention_ttl", "negative", `"-5m"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.mail.RetentionPolicy()
			if err == nil {
				t.Fatal("RetentionPolicy() succeeded, want error")
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			validateErr := ValidateMailRetention(&City{Mail: tt.mail}, "city.toml")
			if validateErr == nil || !strings.HasPrefix(validateErr.Error(), "city.toml: ") {
				t.Errorf("ValidateMailRetention = %v, want an error prefixed with the source", validateErr)
			}
		})
	}
}

func TestMailRetentionPolicyAllowsSameMatchAsSenderAndRecipient(t *testing.T) {
	mailCfg := MailConfig{
		Recipients: []MailRecipientRetention{{Match: "human", ArchiveReadAfter: "0"}},
		Senders:    []MailSenderRetention{{Match: "human", ArchiveReadAfter: "0"}},
	}
	if _, err := mailCfg.RetentionPolicy(); err != nil {
		t.Fatalf("RetentionPolicy: %v; a match may appear once in each table", err)
	}
}

func TestLoadRejectsDuplicateMailSender(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "city.toml")
	data := `
[workspace]
name = "test"

[[mail.sender]]
match = "human"
archive_read_after = "0"

[[mail.sender]]
match = "human"
retention_ttl = "0"
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadWithIncludes(fsys.OSFS{}, path)
	if err == nil {
		t.Fatal("LoadWithIncludes succeeded, want the duplicate sender rejected at load")
	}
	if !strings.Contains(err.Error(), "[[mail.sender]] #2: duplicate match \"human\"") {
		t.Fatalf("error = %v, want the table, entry and match", err)
	}
}

func TestMailRetentionPolicyForSender(t *testing.T) {
	mailCfg := MailConfig{
		ArchiveReadAfter: "2h",
		RetentionTTL:     "168h",
		Recipients: []MailRecipientRetention{
			{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
			{Match: "gascity/short", ArchiveReadAfter: "10m", RetentionTTL: "1h"},
			{Match: "gascity/long", ArchiveReadAfter: "48h", RetentionTTL: "720h"},
			{Match: "gascity/archive-only", ArchiveReadAfter: "5m"},
		},
		Senders: []MailSenderRetention{
			{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
			{Match: "ops/*", ArchiveReadAfter: "24h", RetentionTTL: "336h"},
			{Match: "ops/noisy", ArchiveReadAfter: "1m"},
			{Match: "bot/purge-fast", RetentionTTL: "30m"},
		},
	}
	policy, err := mailCfg.RetentionPolicy()
	if err != nil {
		t.Fatalf("RetentionPolicy: %v", err)
	}
	defaults := MailRetentionWindows{ArchiveReadAfter: 2 * time.Hour, RetentionTTL: 168 * time.Hour}
	tests := []struct {
		name      string
		sender    string
		recipient string
		want      MailRetentionWindows
	}{
		{"no rule matches", "gascity/worker", "gascity/worker", defaults},
		{"never-archive sender to a recipient with no rule", "human", "gascity/worker", MailRetentionWindows{}},
		{"sender rule alone applies its windows", "ops/deploy", "gascity/worker", MailRetentionWindows{ArchiveReadAfter: 24 * time.Hour, RetentionTTL: 336 * time.Hour}},
		{"first sender match wins", "ops/noisy", "gascity/worker", MailRetentionWindows{ArchiveReadAfter: 24 * time.Hour, RetentionTTL: 336 * time.Hour}},
		{"sender rule shorter than recipient rule: recipient wins", "bot/purge-fast", "gascity/long", MailRetentionWindows{ArchiveReadAfter: 48 * time.Hour, RetentionTTL: 720 * time.Hour}},
		{"recipient rule shorter than sender rule: sender wins", "ops/deploy", "gascity/short", MailRetentionWindows{ArchiveReadAfter: 24 * time.Hour, RetentionTTL: 336 * time.Hour}},
		{"never outlasts any window, from either side", "human", "gascity/long", MailRetentionWindows{}},
		{"never recipient outlasts the sender rule", "ops/deploy", "human", MailRetentionWindows{}},
		{"each window takes its own longest", "bot/purge-fast", "gascity/archive-only", MailRetentionWindows{ArchiveReadAfter: 5 * time.Minute, RetentionTTL: 30 * time.Minute}},
		{"a sender knob left unset keeps the recipient's", "bot/purge-fast", "gascity/short", MailRetentionWindows{ArchiveReadAfter: 10 * time.Minute, RetentionTTL: time.Hour}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.For(tt.sender, tt.recipient); got != tt.want {
				t.Fatalf("For(%q, %q) = %+v, want %+v", tt.sender, tt.recipient, got, tt.want)
			}
		})
	}
}

func TestMailRetentionPolicyShortestWindowsCountSenders(t *testing.T) {
	tests := []struct {
		name          string
		mail          MailConfig
		wantArchive   time.Duration
		wantRetention time.Duration
		wantVaries    bool
	}{
		{
			name: "never-archive sender varies the archive window",
			mail: MailConfig{RetentionTTL: "168h", Senders: []MailSenderRetention{
				{Match: "human", ArchiveReadAfter: "0", RetentionTTL: "0"},
			}},
			wantArchive:   168 * time.Hour,
			wantRetention: 168 * time.Hour,
			wantVaries:    true,
		},
		{
			name: "shorter sender window is the shortest",
			mail: MailConfig{Senders: []MailSenderRetention{
				{Match: "ops/*", ArchiveReadAfter: "10m", RetentionTTL: "2h"},
			}},
			wantArchive:   10 * time.Minute,
			wantRetention: 2 * time.Hour,
			wantVaries:    true,
		},
		{
			name: "only a sender purges",
			mail: MailConfig{ArchiveReadAfter: "0", Senders: []MailSenderRetention{
				{Match: "ops/*", RetentionTTL: "720h"},
			}},
			wantRetention: 720 * time.Hour,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := tt.mail.RetentionPolicy()
			if err != nil {
				t.Fatalf("RetentionPolicy: %v", err)
			}
			if got := policy.ShortestArchiveReadAfter(); got != tt.wantArchive {
				t.Errorf("ShortestArchiveReadAfter() = %v, want %v", got, tt.wantArchive)
			}
			if got := policy.ShortestRetentionTTL(); got != tt.wantRetention {
				t.Errorf("ShortestRetentionTTL() = %v, want %v", got, tt.wantRetention)
			}
			if got := policy.ArchiveVariesByMessage(); got != tt.wantVaries {
				t.Errorf("ArchiveVariesByMessage() = %v, want %v", got, tt.wantVaries)
			}
		})
	}
}
