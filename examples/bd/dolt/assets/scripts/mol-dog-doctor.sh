#!/usr/bin/env bash
# mol-dog-doctor — probe Dolt server health and report findings.
#
# Converted from the former mol-dog-doctor formula. All checks are read-only: SQL probe,
# PROCESSLIST count, disk usage, orphan DB detection, backup freshness.
# No LLM judgment needed — runs inline in the controller.
#
# Runs as an exec order (no LLM, no agent, no wisp).
#
# RPO note: BACKUP_STALE_S (default 43200 = 12h = 2x backup interval) bounds how
# long live commits may go unpushed. A database's backup is stale when the
# oldest live commit newer than its last push (the backup remote's manifest
# mtime) is itself older than BACKUP_STALE_S. A scope with nothing to push stays
# current however long ago its last push was: `bd backup sync` writes nothing
# for it, so the manifest age alone measures idleness, not data at risk. Nor
# does the first write after a long idle read as the push's age: its own age is
# how long that change has gone unbacked-up.
# With 6h backup syncs and fail-closed journal corruption recovery, maximum data
# loss on journal corruption without manual intervention is one 6h backup interval.
# If BACKUP_STALE_S exceeds 2x the backup interval, a single missed backup cycle
# is undetected. Keep BACKUP_STALE_S <= 2x the configured backup order
# interval (`interval` in orders/mol-dog-backup.toml, currently 6h).
set -euo pipefail

PACK_DIR="${GC_PACK_DIR:-$(CDPATH= cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
. "$PACK_DIR/assets/scripts/runtime.sh"
. "$PACK_DIR/assets/scripts/latency.sh"
. "$PACK_DIR/assets/scripts/advisory_state.sh"
. "$PACK_DIR/assets/scripts/_notify.sh"

PORT="$GC_DOLT_PORT"
HOST="${GC_DOLT_HOST:-127.0.0.1}"
USER="${GC_DOLT_USER:-root}"
# Latency warn threshold in milliseconds. GC_DOCTOR_LATENCY_WARN_MS takes
# precedence; otherwise derive from the legacy seconds knob (default 1s ->
# 1000ms) for backward compatibility.
LATENCY_WARN_MS="${GC_DOCTOR_LATENCY_WARN_MS:-$(( ${GC_DOCTOR_LATENCY_WARN_S:-1} * 1000 ))}"
CONN_WARN_PCT="${GC_DOCTOR_CONN_WARN_PCT:-80}"
BACKUP_STALE_S="${GC_DOCTOR_BACKUP_STALE_S:-43200}"  # 2x 6h backup interval
BACKUP_ARTIFACT_DIR="${GC_BACKUP_ARTIFACT_DIR:-$GC_CITY_PATH/.dolt-backup}"
# How many live commits the lag count reads; a lag that deep reports as "N+".
BACKUP_LAG_SCAN_COMMITS="${GC_DOCTOR_BACKUP_LAG_SCAN_COMMITS:-100}"
# Advisory dedup state (#3409): records the signature of the last-sent [MEDIUM]
# advisory so a persistent condition collapses into one rolling alert instead of
# a fresh bead every 5-min tick. DOLT_STATE_DIR is set by runtime.sh.
ADVISORY_STATE_FILE="${GC_DOCTOR_ADVISORY_STATE_FILE:-$DOLT_STATE_DIR/doctor-advisory-state}"
# Advisory mailbox hygiene: the subject the [MEDIUM] escalation sends under, the
# prefix the superseded-advisory sweep matches (severity-agnostic on purpose, so
# it also catches older sends under a different severity tag), and the recipient
# those sends go to. The recipient is the same env-var-with-default the core
# escalate.sh resolves (RECIPIENT="${GC_ESCALATION_RECIPIENT:-human}"); a pack
# that overrides escalate.sh with a different recipient makes the sweep a
# harmless no-op. The CRITICAL "server unreachable" escalation uses a different
# subject and is never matched by the prefix, so outage mail is never swept.
ADVISORY_SUBJECT="Dolt health advisory [MEDIUM]"
ADVISORY_SUBJECT_PREFIX="Dolt health advisory"
ADVISORY_RECIPIENT="${GC_ESCALATION_RECIPIENT:-human}"

dolt_sql() {
    DOLT_CLI_PASSWORD="${GC_DOLT_PASSWORD:-}" \
        run_bounded 10 \
        dolt --host "$HOST" --port "$PORT" --user "$USER" --no-tls sql "$@"
}

# CONN_MAX: explicit override > server @@GLOBAL.max_connections > fallback.
if [ -n "${GC_DOCTOR_CONN_MAX:-}" ]; then
    CONN_MAX="$GC_DOCTOR_CONN_MAX"
else
    _server_max=$(dolt_sql -r csv -q "SELECT @@GLOBAL.max_connections" 2>/dev/null | tail -1 || true)
    case "${_server_max:-}" in
        ''|*[!0-9]*) CONN_MAX=256 ;;
        *) CONN_MAX="$_server_max" ;;
    esac
    unset _server_max
fi

file_mtime() {
    file_path="$1"
    file_mtime_value=$(stat -c %Y "$file_path" 2>/dev/null \
        || stat -f %m "$file_path" 2>/dev/null || echo "0")
    case "$file_mtime_value" in
        ''|*[!0-9]*) file_mtime_value=0 ;;
    esac
    printf '%s\n' "$file_mtime_value"
}

backup_path_matches_db() {
    db_name="$1"
    backup_rel_path="$2"
    case "$backup_rel_path" in
        "$db_name"|"$db_name"/*|"$db_name".*|"$db_name"-*|*"/$db_name"|*"/$db_name"/*|*"/$db_name".*|*"/$db_name"-*)
            return 0
            ;;
    esac
    return 1
}

# Age of the newest *restorable* backup for a database, which is the mtime of
# the remote's manifest and never the mtime of the newest file of any kind.
# `dolt backup sync` writes chunk data first and adopts it by rewriting the
# manifest last, so a sync killed in between leaves chunk files newer than
# anything the manifest references. Measured on a live city: an hq remote whose
# newest chunk landed at 21:04 carried a manifest still reading 15:04, and the
# manifest did not reference that chunk. Reading file mtimes there reports a
# six-hour-old backup as nine minutes old.
#
# A db-named directory is the remote the backup order configures
# (file://$BACKUP_ARTIFACT_DIR/<db>), so it is dated only by its own root
# manifest, the same file `gc dolt health` reads. With no manifest it is a
# remote whose first sync never completed: it holds nothing restorable and
# reports 0. Nothing else is read for it, because the prefix arms of
# backup_path_matches_db also accept a sibling such as <db>-dev/manifest or a
# moved-aside <db>.old/manifest, and either would date this database by a
# backup that is not its own.
#
# Only layouts without that directory fall back to the prefix-matched files,
# because BACKUP_ARTIFACT_DIR may point at something that is not a Dolt remote
# at all. A matched manifest still wins there and, with none, the newest
# matching file is the only age there is to read. The prefix arms can still
# credit a sibling's files on that path.
backup_commit_mtime_for_db() {
    db_name="$1"
    db_remote_dir="$BACKUP_ARTIFACT_DIR/$db_name"
    if [ -d "$db_remote_dir" ]; then
        if [ -f "$db_remote_dir/manifest" ]; then
            file_mtime "$db_remote_dir/manifest"
        else
            printf '0\n'
        fi
        return 0
    fi
    newest_mtime=0
    manifest_mtime=0
    while IFS= read -r -d '' backup_path; do
        backup_rel_path="${backup_path#$BACKUP_ARTIFACT_DIR/}"
        if backup_path_matches_db "$db_name" "$backup_rel_path"; then
            backup_mtime=$(file_mtime "$backup_path")
            case "$backup_path" in
                */manifest)
                    if [ "$backup_mtime" -gt "$manifest_mtime" ]; then
                        manifest_mtime="$backup_mtime"
                    fi
                    ;;
                *)
                    if [ "$backup_mtime" -gt "$newest_mtime" ]; then
                        newest_mtime="$backup_mtime"
                    fi
                    ;;
            esac
        fi
    done < <(find "$BACKUP_ARTIFACT_DIR" -type f -print0 2>/dev/null)
    if [ "$manifest_mtime" -gt 0 ]; then
        printf '%s\n' "$manifest_mtime"
    else
        printf '%s\n' "$newest_mtime"
    fi
}

# Live commits in a database newer than its last push, read from `dolt log` in
# its data directory (read-only, like the `dolt backup` probe below): prints
# "<count> <epoch of the oldest such commit>", the epoch 0 when none. Fails when
# the log cannot be read or carries no parseable Date: line, so the caller falls
# back to judging the backup by age alone. Dates are parsed in awk rather than
# date(1) because GNU and BSD date disagree on parsing.
backup_live_lag() {
    lag_db_dir="$1"
    lag_pushed_at="$2"
    lag_log=$(cd "$lag_db_dir" && run_bounded 30 dolt log -n "$BACKUP_LAG_SCAN_COMMITS" 2>/dev/null) || return 1
    printf '%s\n' "$lag_log" | awk -v pushed="$lag_pushed_at" '
        function civil_days(y, m, d,   era, yoe, doy) {
            y -= (m <= 2)
            era = int((y >= 0 ? y : y - 399) / 400)
            yoe = y - era * 400
            doy = int((153 * ((m + 9) % 12) + 2) / 5) + d - 1
            return era * 146097 + yoe * 365 + int(yoe / 4) - int(yoe / 100) + doy - 719468
        }
        # Date:  Tue Oct 13 12:59:15 -0700 2026
        $1 == "Date:" && NF >= 7 && length($3) == 3 {
            m = (index("JanFebMarAprMayJunJulAugSepOctNovDec", $3) + 2) / 3
            if (m < 1 || m != int(m)) next
            if ($4 !~ /^[0-9]+$/ || $5 !~ /^[0-9]+:[0-9]+:[0-9]+$/ || $6 !~ /^[+-][0-9][0-9][0-9][0-9]$/ || $7 !~ /^[0-9]+$/) next
            split($5, hms, ":")
            off = (substr($6, 2, 2) * 3600 + substr($6, 4, 2) * 60) * (substr($6, 1, 1) == "-" ? -1 : 1)
            t = civil_days($7 + 0, m, $4 + 0) * 86400 + hms[1] * 3600 + hms[2] * 60 + hms[3] - off
            scanned++
            if (t > pushed) {
                count++
                if (!oldest || t < oldest) oldest = t
            }
        }
        END { if (!scanned) exit 1; print count + 0, oldest + 0 }'
}

# A duration in seconds as "Nh", or "Nm" under an hour.
backup_age_text() {
    if [ "$1" -ge 3600 ]; then
        printf '%sh\n' "$(($1 / 3600))"
    else
        printf '%sm\n' "$(($1 / 60))"
    fi
}

# Which scope owns each database: "<db>\t<rig>\t<suspended>" lines from
# `gc rig list --json` and each scope's .beads/metadata.json, the city's
# included. Read cold, never through bd, because any bd call restarts a
# suspended scope's retired proxy and Dolt. Fails when ownership cannot be
# fully known (no jq, rig list unavailable, a listed scope with no metadata),
# so no database is called unowned on a guess.
# The database a scope's bead store lives in, read cold: `.beads/metadata.json`
# (`dolt_database`, the proxied-server layout proxied_scope.sh reads), else the
# last path segment of `.beads/dolt-backup.json`'s backup_url, which is the
# destination `bd backup init` registered for that database (beast-gas-city's
# scopes carry only the latter). Prints nothing for a scope with neither file;
# fails when a file is present but unreadable.
backup_scope_db() {
    scope_meta="$1/.beads/metadata.json"
    scope_backup="$1/.beads/dolt-backup.json"
    if [ -f "$scope_meta" ]; then
        jq -r '.dolt_database // empty' "$scope_meta" 2>/dev/null || return 1
    elif [ -f "$scope_backup" ]; then
        jq -r '.backup_url // empty' "$scope_backup" 2>/dev/null | sed -e 's#/*$##' -e 's#.*/##' || return 1
    fi
}

backup_db_owners() {
    command -v jq >/dev/null 2>&1 || return 1
    owner_rigs=$(cd "$GC_CITY_PATH" && run_bounded "$GC_DOLT_RIG_LIST_TIMEOUT_SECS" gc rig list --json 2>/dev/null \
        | jq -r '.rigs[] | [.name, .path, (.suspended // false | tostring)] | @tsv' 2>/dev/null) || return 1
    [ -n "$owner_rigs" ] || return 1
    while IFS=$'\t' read -r owner_rig owner_path owner_suspended; do
        [ -f "$owner_path/.beads/metadata.json" ] || [ -f "$owner_path/.beads/dolt-backup.json" ] || return 1
        owner_db=$(backup_scope_db "$owner_path") || return 1
        if [ -n "$owner_db" ]; then
            printf '%s\t%s\t%s\n' "$owner_db" "$owner_rig" "$owner_suspended"
        fi
    done <<<"$owner_rigs"
    if [ -f "$GC_CITY_PATH/.beads/metadata.json" ] || [ -f "$GC_CITY_PATH/.beads/dolt-backup.json" ]; then
        owner_db=$(backup_scope_db "$GC_CITY_PATH") || return 1
        if [ -n "$owner_db" ]; then
            printf '%s\tcity\tfalse\n' "$owner_db"
        fi
    fi
}

# The BACKUP_DB_OWNERS line for one database; fails when no scope owns it.
backup_db_owner() {
    printf '%s\n' "$BACKUP_DB_OWNERS" | awk -F'\t' -v db="$1" '$1 == db { print; found = 1; exit } END { exit !found }'
}

append_backup_stale() {
    backup_stale_item="$1"
    if [ -n "$BACKUP_STALE_ITEMS" ]; then
        BACKUP_STALE_ITEMS="$BACKUP_STALE_ITEMS, $backup_stale_item"
    else
        BACKUP_STALE_ITEMS="$backup_stale_item"
    fi
}

send_escalation() {
    local subject="$1"
    local message="$2"
    local err
    if ! err=$(dolt_escalate "$subject" "$message" 2>&1 >/dev/null); then
        if [ -n "$err" ]; then
            echo "doctor: escalation failed: $err" >&2
        else
            echo "doctor: escalation failed" >&2
        fi
        return 1
    fi
}

# --- Step 1: Probe connectivity and measure latency ---

PROBE_START_MS=$(now_ms)
if ! dolt_sql -q "SELECT active_branch()" >/dev/null 2>&1; then
    if send_escalation \
        "ESCALATION: Dolt server unreachable on port $PORT [CRITICAL]" \
        "Doctor probe failed: server did not respond to active_branch() query."; then
        dolt_notify_done "doctor — server: UNREACHABLE (escalated)"
        echo "doctor: server unreachable on port $PORT (escalated)"
    else
        dolt_notify_done "doctor — server: UNREACHABLE (escalation failed)"
        echo "doctor: server unreachable on port $PORT (escalation failed)"
    fi
    exit 0
fi
PROBE_END_MS=$(now_ms)
LATENCY_MS=$((PROBE_END_MS - PROBE_START_MS))
LATENCY_WARN=""
if latency_should_warn "$LATENCY_MS" "$LATENCY_WARN_MS"; then
    LATENCY_WARN=" [WARN: latency ${LATENCY_MS}ms >= threshold ${LATENCY_WARN_MS}ms]"
fi

# --- Step 2: Check resource conditions ---

CONN_COUNT=$(dolt_sql -r csv -q "SELECT COUNT(*) FROM information_schema.PROCESSLIST" 2>/dev/null \
    | tail -1 || echo "0")
CONN_WARN=""
CONN_WARN_AT=$(( (CONN_MAX * CONN_WARN_PCT) / 100 ))
if [ "${CONN_COUNT:-0}" -ge "$CONN_WARN_AT" ]; then
    CONN_WARN=" [WARN: ${CONN_COUNT} connections >= ${CONN_WARN_PCT}% of max ${CONN_MAX}]"
fi

# Disk usage of Dolt data directory.
DISK_USAGE=$(du -sh "$DOLT_DATA_DIR" 2>/dev/null | cut -f1 || echo "unknown")

# Orphan database detection.
ALL_DBS=$(dolt_sql -r csv -q "SHOW DATABASES" 2>/dev/null | tail -n +2 || true)
ORPHAN_PATTERNS="^(testdb_|beads_t|beads_pt|beads_vr|doctest_|doctortest_)"
SYSTEM_DBS="^(information_schema|mysql|dolt_cluster|__gc_probe|performance_schema|sys)$"
USER_DBS=$(printf '%s\n' "$ALL_DBS" | grep -viE "$SYSTEM_DBS" || true)
ORPHANS=$(printf '%s\n' "$USER_DBS" | grep -iE "$ORPHAN_PATTERNS" || true)
ORPHAN_COUNT=$(printf '%s\n' "$ORPHANS" | awk 'NF {count++} END {print count + 0}')
ORPHAN_WARN=""
if [ "${ORPHAN_COUNT:-0}" -gt 0 ]; then
    ORPHAN_WARN=" [WARN: $ORPHAN_COUNT orphan DBs detected — run gc dolt cleanup]"
fi

# Backup freshness: check each database's newest restorable backup, read from
# the backup remote's manifest. See backup_commit_mtime_for_db.
# Every user database is in scope. DBs without a configured backup remote
# are reported as a coverage gap rather than silently excluded —
# the exclusion is how unconfigured production DBs went unbacked-up until
# journal corruption made them unrecoverable (#3176). The remote is named
# as mol-dog-backup.sh resolves it: <db>-backup (the pre-bd registration),
# bd's `default` (`bd backup init`, #6751), else the only one registered.
# mol-dog-backup.sh auto-configures the remote on its next run, so this
# warning self-heals unless the backup dog itself is failing. A database no
# scope owns is never visited by a backup run, so it is named as left out
# rather than aged into a stale backup that no run will ever refresh.
BACKUP_ELIGIBLE_DBS=""
BACKUP_STALE_ITEMS=""
BACKUP_SCOPES_CHECKED=0
BACKUP_SCOPES_CURRENT=0
BACKUP_DB_OWNERS=""
BACKUP_OWNERS_KNOWN=false
if [ -n "$USER_DBS" ] && BACKUP_DB_OWNERS=$(backup_db_owners); then
    BACKUP_OWNERS_KNOWN=true
fi
for db in $USER_DBS; do
    db_dir="$DOLT_DATA_DIR/$db"
    if [ -d "$db_dir/.dolt" ]; then
        if [ "$BACKUP_OWNERS_KNOWN" = true ] && ! backup_db_owner "$db" >/dev/null; then
            append_backup_stale "$db: no rig; left out of backups"
            continue
        fi
        BACKUP_SCOPES_CHECKED=$((BACKUP_SCOPES_CHECKED + 1))
        if (cd "$db_dir" && run_bounded 30 dolt backup 2>/dev/null | awk -v db="$db" 'NF { n++ } $1 == (db "-backup") || $1 == "default" { ok = 1 } END { exit !(ok || n == 1) }'); then
            BACKUP_ELIGIBLE_DBS="$BACKUP_ELIGIBLE_DBS $db"
        else
            append_backup_stale "$db backup remote missing"
        fi
    fi
done
BACKUP_ELIGIBLE_DBS=$(printf '%s\n' "$BACKUP_ELIGIBLE_DBS" | tr ' ' '\n' | grep -v '^$' || true)

BACKUP_STALE=""
if [ -n "$BACKUP_ELIGIBLE_DBS" ]; then
    if [ ! -d "$BACKUP_ARTIFACT_DIR" ]; then
        BACKUP_STALE=" [WARN: backup artifact dir missing]"
    else
        NOW_S=$(date +%s)
        for db in $BACKUP_ELIGIBLE_DBS; do
            BACKUP_COMMIT_MTIME=$(backup_commit_mtime_for_db "$db")
            if [ "$BACKUP_COMMIT_MTIME" -le 0 ]; then
                append_backup_stale "$db backup missing"
                continue
            fi
            BACKUP_AGE=$((NOW_S - BACKUP_COMMIT_MTIME))
            if [ "$BACKUP_AGE" -le "$BACKUP_STALE_S" ]; then
                BACKUP_SCOPES_CURRENT=$((BACKUP_SCOPES_CURRENT + 1))
                continue
            fi
            if BACKUP_LAG=$(backup_live_lag "$DOLT_DATA_DIR/$db" "$BACKUP_COMMIT_MTIME"); then
                read -r BACKUP_LAG_COMMITS BACKUP_LAG_OLDEST <<<"$BACKUP_LAG"
                if [ "$BACKUP_LAG_COMMITS" -eq 0 ]; then
                    BACKUP_SCOPES_CURRENT=$((BACKUP_SCOPES_CURRENT + 1))
                    continue
                fi
                BACKUP_LAG_AGE=$((NOW_S - BACKUP_LAG_OLDEST))
                if [ "$BACKUP_LAG_COMMITS" -ge "$BACKUP_LAG_SCAN_COMMITS" ]; then
                    # The scan ended before reaching the push, so the oldest
                    # unpushed change is older than any commit read; the push's
                    # age past the threshold is the only bound left.
                    BACKUP_DETAIL="$db backup is ${BACKUP_LAG_COMMITS}+ commits behind; oldest unpushed change is at least $(backup_age_text "$BACKUP_LAG_AGE") old"
                elif [ "$BACKUP_LAG_AGE" -le "$BACKUP_STALE_S" ]; then
                    BACKUP_SCOPES_CURRENT=$((BACKUP_SCOPES_CURRENT + 1))
                    continue
                elif [ "$BACKUP_LAG_COMMITS" -eq 1 ]; then
                    BACKUP_DETAIL="$db backup is 1 commit behind; oldest unpushed change is $(backup_age_text "$BACKUP_LAG_AGE") old"
                else
                    BACKUP_DETAIL="$db backup is $BACKUP_LAG_COMMITS commits behind; oldest unpushed change is $(backup_age_text "$BACKUP_LAG_AGE") old"
                fi
            else
                BACKUP_DETAIL="$db backup is $((BACKUP_AGE / 3600))h old, live commits unreadable"
            fi
            # A suspended rig is left cold by mol-dog-backup.sh, so its lag
            # waits for the rig to resume.
            if BACKUP_OWNER=$(backup_db_owner "$db"); then
                IFS=$'\t' read -r _ BACKUP_OWNER_RIG BACKUP_OWNER_SUSPENDED <<<"$BACKUP_OWNER"
                if [ "$BACKUP_OWNER_SUSPENDED" = true ]; then
                    BACKUP_DETAIL="$BACKUP_DETAIL (rig $BACKUP_OWNER_RIG suspended)"
                fi
            fi
            append_backup_stale "$BACKUP_DETAIL"
        done
    fi
fi
if [ -n "$BACKUP_STALE_ITEMS" ]; then
    BACKUP_STALE="$BACKUP_STALE [WARN: backup freshness: $BACKUP_STALE_ITEMS]"
fi

# --- Step 3: Compose report and escalate if critical ---

WARNINGS="${LATENCY_WARN}${CONN_WARN}${ORPHAN_WARN}${BACKUP_STALE}"
if [ -n "$WARNINGS" ]; then
    # Dedup (#3409): key on which conditions are active — not their tick-volatile
    # values (exact latency ms, connection count, backup age) — and re-send only
    # when that set changes. Record after a successful send so a failed
    # escalation retries next tick. The CRITICAL "server unreachable" path above
    # is never deduped, so a true outage always alerts.
    ADVISORY_SIG=""
    if [ -n "$LATENCY_WARN" ]; then ADVISORY_SIG="${ADVISORY_SIG}latency "; fi
    if [ -n "$CONN_WARN" ]; then ADVISORY_SIG="${ADVISORY_SIG}conn "; fi
    if [ -n "$ORPHAN_WARN" ]; then ADVISORY_SIG="${ADVISORY_SIG}orphan "; fi
    if [ -n "$BACKUP_STALE" ]; then ADVISORY_SIG="${ADVISORY_SIG}backup "; fi
    if advisory_changed "$ADVISORY_SIG" "$ADVISORY_STATE_FILE"; then
        # Sweep superseded advisories before the fresh send: the new advisory
        # carries the full current status block, so older snapshots (and any
        # backlog minted before the dedup existed) are stale the moment it
        # lands. If the send below then fails, the sig stays unrecorded and the
        # next tick retries both the sweep (a no-op) and the send.
        advisory_archive_superseded "$ADVISORY_SUBJECT_PREFIX" "$ADVISORY_RECIPIENT"
        if send_escalation \
            "$ADVISORY_SUBJECT" \
            "Latency: ${LATENCY_MS}ms${LATENCY_WARN}
Connections: ${CONN_COUNT}/${CONN_MAX}${CONN_WARN}
Disk: ${DISK_USAGE}
Orphan DBs: ${ORPHAN_COUNT}${ORPHAN_WARN}${BACKUP_STALE}"; then
            advisory_record "$ADVISORY_SIG" "$ADVISORY_STATE_FILE"
        fi
    fi
else
    # Healthy: the condition cleared, so no advisory should stay open. Sweep
    # only when a prior advisory was recorded (state file present), so steady
    # healthy ticks cost no extra gc call; then forget the signature so a
    # future condition re-alerts.
    if [ -f "$ADVISORY_STATE_FILE" ]; then
        advisory_archive_superseded "$ADVISORY_SUBJECT_PREFIX" "$ADVISORY_RECIPIENT"
    fi
    advisory_clear "$ADVISORY_STATE_FILE"
fi

SUMMARY="doctor — server: ok, latency: ${LATENCY_MS}ms, conns: ${CONN_COUNT}/${CONN_MAX}, disk: ${DISK_USAGE}, orphans: ${ORPHAN_COUNT}"
if [ "$BACKUP_SCOPES_CHECKED" -gt 0 ]; then
    SUMMARY="$SUMMARY, backups current: ${BACKUP_SCOPES_CURRENT}/${BACKUP_SCOPES_CHECKED}"
fi
dolt_notify_done "$SUMMARY"
echo "doctor: $SUMMARY"
