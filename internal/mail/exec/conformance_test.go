package exec //nolint:revive // internal package, always imported with alias

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gastownhall/gascity/internal/mail"
	"github.com/gastownhall/gascity/internal/mail/mailtest"
)

// statefulScript returns a shell script body that maintains message state
// in a temp directory. Each message is stored as a file with line-based
// format: id\nfrom\nto\nsubject\nbody\ntimestamp\nread-state\nthread_id\nreply_to\nclosed_at
// where read-state is "open" or "read" and a non-empty closed_at marks the
// message archived. Delete removes the file.
func statefulScript(stateDir string) string {
	return `#!/bin/sh
set -e
STATE="` + stateDir + `"
op="$1"
shift

# Initialize next_id if missing.
if [ ! -f "$STATE/next_id" ]; then
  echo 1 > "$STATE/next_id"
fi
mkdir -p "$STATE/messages"

not_found() {
  echo "gc-mail-error:not-found: message \"$1\" not found" >&2
  exit 1
}

# message_json prints one message file as a JSON object.
message_json() {
  f="$1"
  closed_at=$(sed -n '10p' "$f")
  read_flag="false"
  if [ "$(sed -n '7p' "$f")" = "read" ]; then
    read_flag="true"
  fi
  status="open"
  closed_field=""
  if [ -n "$closed_at" ]; then
    status="closed"
    closed_field=",\"closed_at\":\"$closed_at\""
  fi
  printf '{"id":"%s","from":"%s","to":"%s","subject":"%s","body":"%s","created_at":"%s","read":%s,"thread_id":"%s","reply_to":"%s","status":"%s"%s}' \
    "$(sed -n '1p' "$f")" "$(sed -n '2p' "$f")" "$(sed -n '3p' "$f")" "$(sed -n '4p' "$f")" "$(sed -n '5p' "$f")" \
    "$(sed -n '6p' "$f")" "$read_flag" "$(sed -n '8p' "$f")" "$(sed -n '9p' "$f")" "$status" "$closed_field"
}

is_archived() {
  [ -n "$(sed -n '10p' "$1")" ]
}

case "$op" in
  ensure-running)
    ;; # no-op
  send)
    to="$1"
    # Read JSON from stdin, extract fields.
    input=$(cat)
    from=$(echo "$input" | sed 's/.*"from":"\([^"]*\)".*/\1/')
    # Extract subject — may be empty string.
    subject=""
    if echo "$input" | grep -q '"subject"'; then
      subject=$(echo "$input" | sed 's/.*"subject":"\([^"]*\)".*/\1/')
    fi
    body=""
    if echo "$input" | grep -q '"body"'; then
      body=$(echo "$input" | sed 's/.*"body":"\([^"]*\)".*/\1/')
    fi
    id=$(cat "$STATE/next_id")
    echo $((id + 1)) > "$STATE/next_id"
    msgid="msg-$id"
    ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    thread_id="thread-$id"
    printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n' "$msgid" "$from" "$to" "$subject" "$body" "$ts" "open" "$thread_id" "" "" > "$STATE/messages/$msgid"
    printf '{"id":"%s","from":"%s","to":"%s","subject":"%s","body":"%s","created_at":"%s","thread_id":"%s"}\n' "$msgid" "$from" "$to" "$subject" "$body" "$ts" "$thread_id"
    ;;
  inbox|check|all|archived)
    recipient="$1"
    result=""
    for f in "$STATE"/messages/*; do
      [ -f "$f" ] || continue
      if [ "$op" = "archived" ]; then
        is_archived "$f" || continue
      else
        ! is_archived "$f" || continue
      fi
      if [ "$op" = "inbox" ] || [ "$op" = "check" ]; then
        [ "$(sed -n '7p' "$f")" = "open" ] || continue
      fi
      [ "$(sed -n '3p' "$f")" = "$recipient" ] || continue
      if [ -n "$result" ]; then
        result="$result,"
      fi
      result="${result}$(message_json "$f")"
    done
    if [ -n "$result" ]; then
      printf '[%s]\n' "$result"
    fi
    ;;
  get)
    msgid="$1"
    f="$STATE/messages/$msgid"
    [ -f "$f" ] || not_found "$msgid"
    message_json "$f"
    printf '\n'
    ;;
  read)
    msgid="$1"
    f="$STATE/messages/$msgid"
    [ -f "$f" ] || not_found "$msgid"
    sed '7s/.*/read/' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    message_json "$f"
    printf '\n'
    ;;
  mark-read)
    msgid="$1"
    f="$STATE/messages/$msgid"
    [ -f "$f" ] || not_found "$msgid"
    sed '7s/.*/read/' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    ;;
  mark-unread)
    msgid="$1"
    f="$STATE/messages/$msgid"
    [ -f "$f" ] || not_found "$msgid"
    sed '7s/.*/open/' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    ;;
  archive)
    msgid="$1"
    f="$STATE/messages/$msgid"
    if [ ! -f "$f" ]; then
      echo "message \"$msgid\" not found" >&2
      exit 1
    fi
    if is_archived "$f"; then
      echo "already archived" >&2
      exit 1
    fi
    ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    sed "10s/.*/$ts/" "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    ;;
  unarchive)
    msgid="$1"
    f="$STATE/messages/$msgid"
    [ -f "$f" ] || not_found "$msgid"
    if ! is_archived "$f"; then
      echo "not archived" >&2
      exit 1
    fi
    sed '10s/.*//' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    ;;
  delete)
    msgid="$1"
    f="$STATE/messages/$msgid"
    if [ ! -f "$f" ]; then
      echo "already archived" >&2
      exit 1
    fi
    rm -f "$f"
    ;;
  reply)
    msgid="$1"
    f="$STATE/messages/$msgid"
    [ -f "$f" ] || not_found "$msgid"
    orig_from=$(sed -n '2p' "$f")
    orig_thread=$(sed -n '8p' "$f")
    # Read JSON from stdin.
    input=$(cat)
    from=$(echo "$input" | sed 's/.*"from":"\([^"]*\)".*/\1/')
    subject=""
    if echo "$input" | grep -q '"subject"'; then
      subject=$(echo "$input" | sed 's/.*"subject":"\([^"]*\)".*/\1/')
    fi
    body=""
    if echo "$input" | grep -q '"body"'; then
      body=$(echo "$input" | sed 's/.*"body":"\([^"]*\)".*/\1/')
    fi
    id=$(cat "$STATE/next_id")
    echo $((id + 1)) > "$STATE/next_id"
    new_msgid="msg-$id"
    ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n' "$new_msgid" "$from" "$orig_from" "$subject" "$body" "$ts" "open" "$orig_thread" "$msgid" "" > "$STATE/messages/$new_msgid"
    printf '{"id":"%s","from":"%s","to":"%s","subject":"%s","body":"%s","created_at":"%s","thread_id":"%s","reply_to":"%s"}\n' "$new_msgid" "$from" "$orig_from" "$subject" "$body" "$ts" "$orig_thread" "$msgid"
    ;;
  thread)
    id="$1"
    thread_id="$id"
    if [ -f "$STATE/messages/$id" ]; then
      thread_id=$(sed -n '8p' "$STATE/messages/$id")
    fi
    result=""
    for f in "$STATE"/messages/*; do
      [ -f "$f" ] || continue
      ! is_archived "$f" || continue
      msg_thread=$(sed -n '8p' "$f")
      [ "$msg_thread" = "$thread_id" ] || continue
      msgid=$(sed -n '1p' "$f")
      from=$(sed -n '2p' "$f")
      msg_to=$(sed -n '3p' "$f")
      subject=$(sed -n '4p' "$f")
      body=$(sed -n '5p' "$f")
      ts=$(sed -n '6p' "$f")
      reply_to=$(sed -n '9p' "$f")
      if [ -n "$result" ]; then
        result="$result,"
      fi
      result="${result}{\"id\":\"$msgid\",\"from\":\"$from\",\"to\":\"$msg_to\",\"subject\":\"$subject\",\"body\":\"$body\",\"created_at\":\"$ts\",\"thread_id\":\"$thread_id\",\"reply_to\":\"$reply_to\"}"
    done
    if [ -n "$result" ]; then
      printf '[%s]\n' "$result"
    fi
    ;;
  count)
    recipient="$1"
    total=0
    unread=0
    for f in "$STATE"/messages/*; do
      [ -f "$f" ] || continue
      ! is_archived "$f" || continue
      status=$(sed -n '7p' "$f")
      msg_to=$(sed -n '3p' "$f")
      [ "$msg_to" = "$recipient" ] || continue
      total=$((total + 1))
      if [ "$status" = "open" ]; then
        unread=$((unread + 1))
      fi
    done
    printf '{"total":%d,"unread":%d}\n' "$total" "$unread"
    ;;
  *)
    exit 2 ;; # unknown operation
esac
`
}

func TestExecConformance(t *testing.T) {
	newProvider := func(t *testing.T) mail.Provider {
		dir := t.TempDir()
		stateDir := filepath.Join(dir, "state")
		if err := os.MkdirAll(filepath.Join(stateDir, "messages"), 0o755); err != nil {
			t.Fatal(err)
		}

		scriptPath := filepath.Join(dir, "mail-provider")
		if err := os.WriteFile(scriptPath, []byte(statefulScript(stateDir)), 0o755); err != nil {
			t.Fatal(err)
		}

		return NewProvider(scriptPath)
	}
	mailtest.RunProviderTests(t, newProvider)
	mailtest.RunArchiveRetentionTests(t, newProvider)
}
