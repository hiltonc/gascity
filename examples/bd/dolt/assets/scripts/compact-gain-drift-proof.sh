#!/bin/sh
# compact-gain-drift-proof.sh — Option A row-preservation proof for the
# post-flatten gain+drift case (gastownhall/gascity#2846).
#
# When verify_counts sees a table gain rows AND its value hash drift, the
# safety property at stake ("pre-flight rows remain reachable") cannot be
# inferred from HEAD movement alone: a concurrent writer whose commit is
# ABSORBED into the flatten commit moves no HEAD, so the HEAD-proven gate
# misses it and a benign race is hard-quarantined — which then blocks all
# future GC of a busy DB (the memory-exhaustion failure the code calls out).
#
# This proves preservation DIRECTLY: for each gained+drifted table, diff the
# pre-flight snapshot HEAD against the flatten commit. If the only change is
# `added` rows (no `removed`/`modified`), every pre-flight row survived and the
# gain is concurrent-writer data — defer, exactly as the HEAD-proven path does.
# It is strictly more rigorous than the HEAD proxy: it proves reachability
# instead of inferring it. Any removed/modified row, or any probe failure,
# fails closed and falls through to quarantine.
#
# Depends on `query_single_cell` and `valid_table_name` from run.sh.

# diff_is_additive_only <db> <from_head> <to_head> <table>
# Returns 0 iff the table's <from>..<to> content diff contains only `added`
# rows. Returns non-zero (fail closed) if either commit endpoint is missing,
# the table name is missing or invalid, the diff probe fails or returns a
# non-numeric result, or the table shows removed/modified rows. Shared by the
# gain+drift preservation proof and the committed-root drift proof's
# first-committed table case (run.sh db_root_drift_within_verified_tables).
diff_is_additive_only() {
  _da_db="$1"
  _da_from="$2"
  _da_to="$3"
  _da_t="$4"
  # Without both commit endpoints there is nothing to diff against — fail closed.
  [ -n "$_da_from" ] && [ -n "$_da_to" ] && [ -n "$_da_t" ] || return 1
  valid_table_name "$_da_t" || return 1
  # Count rows that are NOT purely additive between the two commits. Zero means
  # every row present at <from> is reachable unchanged at <to> and the only
  # change was added rows.
  if ! _da_nonadded=$(query_single_cell "$_da_db" \
    "preservation diff probe failed for table=$_da_t" \
    "SELECT COUNT(*) FROM DOLT_DIFF('$_da_from', '$_da_to', '$_da_t') WHERE diff_type <> 'added'"); then
    return 1
  fi
  case "$_da_nonadded" in
    0) return 0 ;;             # only added rows — this table's <from> rows preserved
    ''|*[!0-9]*) return 1 ;;   # empty/non-numeric probe result — fail closed
    *) return 1 ;;             # one or more removed/modified rows — not preservable
  esac
}

# gain_drift_is_additive_only <db> <from_head> <to_head> <space-separated tables>
# Returns 0 iff every listed table's <from>..<to> content diff contains only
# `added` rows. Returns non-zero (fail closed) if the table list is empty or
# any table fails diff_is_additive_only.
gain_drift_is_additive_only() {
  _gd_db="$1"
  _gd_from="$2"
  _gd_to="$3"
  _gd_tables="$4"
  _gd_seen=0
  for _gd_t in $_gd_tables; do
    _gd_seen=1
    diff_is_additive_only "$_gd_db" "$_gd_from" "$_gd_to" "$_gd_t" || return 1
  done
  # An empty table list is not a proof of preservation.
  [ "$_gd_seen" = "1" ] || return 1
  return 0
}

# drift_removes_no_rows <db> <from_head> <to_head> <space-separated tables>
# Returns 0 iff no listed table lost a row between <from> and <to>: its content
# diff may hold `added` and `modified` rows (a proven concurrent writer's
# INSERT/UPDATE folded into the flatten) but zero `removed`. This is the
# same-count drift proof: a writer's UPDATE is exactly a modified row, so the
# additive-only proof above can never pass for it. Fails closed on an empty
# table list, a missing endpoint, an invalid table name, a probe failure or a
# non-numeric result.
drift_removes_no_rows() {
  _dr_db="$1"
  _dr_from="$2"
  _dr_to="$3"
  _dr_tables="$4"
  [ -n "$_dr_from" ] && [ -n "$_dr_to" ] || return 1
  _dr_seen=0
  for _dr_t in $_dr_tables; do
    _dr_seen=1
    valid_table_name "$_dr_t" || return 1
    if ! _dr_removed=$(query_single_cell "$_dr_db" \
      "row-loss diff probe failed for table=$_dr_t" \
      "SELECT COUNT(*) FROM DOLT_DIFF('$_dr_from', '$_dr_to', '$_dr_t') WHERE diff_type = 'removed'"); then
      return 1
    fi
    case "$_dr_removed" in
      0) ;;
      *) return 1 ;;
    esac
  done
  [ "$_dr_seen" = "1" ] || return 1
  return 0
}
