#!/bin/sh
# compact_quarantine.sh — read the compactor's quarantine markers for the
# reports people actually look at: `gc dolt health` and mol-dog-doctor's
# advisory mail.
#
# A quarantined database is skipped by every later compact and --gc-only run,
# so its oldgen is never collected. gcd sat quarantined for two days on a
# verifier race and grew to 7.5G (6.7G oldgen) with nothing but a one-time
# compact-time mail to show for it (bgc-wabb). Each entry therefore carries the
# marker's age and the db's oldgen size, the two numbers that say how long it
# has been stranded and what it is costing.
#
# Sourced by commands/health/run.sh and assets/scripts/mol-dog-doctor.sh.

# marker_epoch — convert an RFC3339 UTC timestamp (e.g. 2026-06-14T23:22:55Z)
# to epoch seconds, portably across GNU and BSD date(1). Empty output on a
# missing or unparseable timestamp so the caller can fall back to file mtime.
marker_epoch() {
  _ts="$1"
  case "$_ts" in
    ''|*[!0-9TZ:.+-]*) return 0 ;;
  esac
  # GNU date parses the RFC3339 string directly; BSD/macOS date needs an
  # explicit input format and the -j (do-not-set-clock) flag.
  # Use `if` rather than `&&` so a failed date(1) doesn't set a non-zero
  # exit status that would trigger `set -e` in the caller's subshell.
  if _e=$(date -u -d "$_ts" +%s 2>/dev/null); then printf '%s' "$_e"; return 0; fi
  if _e=$(date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "$_ts" +%s 2>/dev/null); then printf '%s' "$_e"; return 0; fi
  return 0
}

# human_duration — format a whole-second count as a compact age string
# (e.g. 12d3h, 5h2m, 7m1s, 9s). Used for compaction quarantine marker age.
human_duration() {
  _s="$1"
  case "$_s" in ''|*[!0-9]*) printf '0s'; return ;; esac
  _d=$((_s / 86400)); _h=$(((_s % 86400) / 3600))
  _m=$(((_s % 3600) / 60)); _sec=$((_s % 60))
  if [ "$_d" -gt 0 ]; then printf '%dd%dh' "$_d" "$_h"
  elif [ "$_h" -gt 0 ]; then printf '%dh%dm' "$_h" "$_m"
  elif [ "$_m" -gt 0 ]; then printf '%dm%ds' "$_m" "$_sec"
  else printf '%ds' "$_sec"; fi
}

# human_kb — format a KiB count as e.g. 6.7G, 161.0M, 12K.
human_kb() {
  _kb="$1"
  case "$_kb" in ''|*[!0-9]*) printf '0K'; return ;; esac
  if [ "$_kb" -ge 1048576 ]; then awk "BEGIN {printf \"%.1fG\", $_kb/1048576}"
  elif [ "$_kb" -ge 1024 ]; then awk "BEGIN {printf \"%.1fM\", $_kb/1024}"
  else printf '%dK' "$_kb"; fi
}

# compact_quarantine_scan QUARANTINE_DIR DATA_DIR — emit one line per
# quarantine marker: db|reason|age_sec|oldgen_kb. Filesystem-only, so it works
# while the server is wedged (a wedged server may itself be a symptom of the
# un-GC'd bloat). The directory and one-file-per-db key=value body layout
# mirror commands/compact/run.sh exactly. oldgen_kb is 0 when the db has no
# oldgen directory.
compact_quarantine_scan() {
  _cq_dir="$1"
  _cq_data="$2"
  [ -d "$_cq_dir" ] || return 0
  for _cq_marker in "$_cq_dir"/*; do
    [ -f "$_cq_marker" ] || continue
    _cq_db=$(basename "$_cq_marker")
    # compact/run.sh writes transient files into this same directory:
    # `mktemp "$dir/$db.tmp.XXXXXX"` (write_compact_marker) and
    # `mktemp "$dir/$db.probe.XXXXXX"` (ensure_compact_marker_writable, run on
    # EVERY flatten). Neither is a marker; reading one yields a phantom entry.
    case "$_cq_db" in *.tmp.*|*.probe.*) continue ;; esac
    # Anchor each key to column 1 with index()==1 — the same reader idiom
    # compact/run.sh uses; the substr offset skips the "reason="/"created_at="
    # key (8 and 12 = key length + 1).
    _cq_reason=$(awk 'index($0, "reason=") == 1 { print substr($0, 8); exit }' "$_cq_marker" 2>/dev/null || true)
    _cq_created=$(awk 'index($0, "created_at=") == 1 { print substr($0, 12); exit }' "$_cq_marker" 2>/dev/null || true)
    [ -n "$_cq_reason" ] || _cq_reason="unknown"
    _cq_epoch=$(marker_epoch "$_cq_created")
    if [ -z "$_cq_epoch" ]; then
      _cq_epoch=$(stat -c %Y "$_cq_marker" 2>/dev/null || stat -f %m "$_cq_marker" 2>/dev/null || echo "")
    fi
    _cq_age=0
    if [ -n "$_cq_epoch" ]; then
      _cq_age=$(( $(date +%s) - _cq_epoch ))
      [ "$_cq_age" -lt 0 ] && _cq_age=0
    fi
    _cq_oldgen_kb=0
    if [ -n "$_cq_data" ] && [ -d "$_cq_data/$_cq_db/.dolt/noms/oldgen" ]; then
      _cq_oldgen_kb=$(du -sk "$_cq_data/$_cq_db/.dolt/noms/oldgen" 2>/dev/null | cut -f1)
      case "$_cq_oldgen_kb" in ''|*[!0-9]*) _cq_oldgen_kb=0 ;; esac
    fi
    printf '%s|%s|%s|%s\n' "$_cq_db" "$_cq_reason" "$_cq_age" "$_cq_oldgen_kb"
  done
}
