# The backup order (`mol-dog-backup`)

`orders/mol-dog-backup.toml` runs `assets/scripts/mol-dog-backup.sh` every 6h.
For every bead scope (the city and each rig) it asks `gc bd` for the scope's
database, registers a `bd backup` destination under the artifact dir when the
scope has none, syncs the scope to it, and finally rsyncs the artifact dir to
an offsite path when one is configured. It is deterministic shell: no agent,
no LLM.

## Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `GC_BACKUP_ARTIFACT_DIR` | `<city>/.dolt-backup` | Where each scope's backup lives, one directory per database. |
| `GC_BACKUP_DATABASES` | all | Comma-separated database names; limits the run to those scopes. |
| `GC_BACKUP_OFFSITE_PATH` | unset | rsync destination for the artifact dir. Unset skips offsite publication. |
| `GC_BACKUP_OFFSITE_TIMEOUT` | `300` | Seconds the offsite rsync may take. |
| `GC_DOLT_BACKUP_SYNC_TIMEOUT_SECS` | `120` | Seconds one sync attempt of one scope may take, for both the snapshot sync and the in-place `bd backup sync`. A sync killed on this bound is reported as `<db>(timed out after Ns)`; a Dolt error as `<db>(sync failed)`. |
| `GC_DOLT_BACKUP_SYNC_ATTEMPTS` | `3` | In-place sync attempts per scope before it is reported failed. |
| `GC_DOLT_BACKUP_BD_TIMEOUT_SECS` | `60` | Seconds for the short bd calls (database lookup, backup status, backup init). |
| `GC_DOLT_BACKUP_SNAPSHOT` | `1` | `0` turns the snapshot step off; every scope then syncs in place. |
| `GC_DOLT_DATA_DIR` | `<city>/.beads/dolt` | The managed Dolt data dir the snapshot step clones from (`<data-dir>/<db>`). |
| `GC_DOLT_BACKUP_LOCK_FILE` | `<city>/.gc/runtime/packs/dolt/backup-sync.lock` | flock that keeps two runs from syncing at once. |
| `GC_DOLT_BACKUP_LOCK_WAIT_SECONDS` | `5` | Seconds to wait for that lock before the run reports "skipped: already running". |

`GC_DOLT_BACKUP_SYNC_TIMEOUT_SECS` and the order's own `timeout` (1800s) move
together: the order budget covers about ten scope syncs at the default bound,
so a city that raises the bound for a large store should check that the run
still fits.

## Snapshot-then-sync

`bd backup sync` runs inside the scope's sql-server, which has two costs on a
large store. The server cuts any statement at its `listener.read_timeout_millis`
whatever bound the script sets (on beast-gas-city, 2026-10-12, the 14 GB `gcd`
store died at exactly 600 s with `Error 1105 (HY000): connection was closed`
on every run, so its backup silently stopped advancing while the small stores
kept succeeding), and the sync serialises against live agent traffic, so the
same delta takes anywhere from seconds to many minutes depending on load.

When the scope's data dir is visible at `<GC_DOLT_DATA_DIR>/<db>` and sits on
a copy-on-write filesystem (APFS: `cp -c`; btrfs/xfs: `cp --reflink=always`),
and the `dolt` CLI is on `PATH`, the script first makes a CoW clone of the
data dir (about a second for 14 GB), strips `.dolt/sql-server.lock` and
`.dolt/sql-server.info` from the clone, and runs `dolt backup sync <name>`
inside it with no server in the loop. `<name>` is whatever the clone's
`.dolt/repo_state.json` registers, resolved as the doctor does: `<db>-backup`
(the pre-bd registration), then bd's `default`, else the only backup present.
The clone is removed afterwards whether the sync succeeded or not. The
measured offline sync of `gcd` took 17 min and advanced the backup.

The in-place `bd backup sync` still runs after a successful snapshot sync: it
pushes the handful of commits written since the clone, which takes well under
either timeout, and it is what records `last_sync` for `bd backup status` and
the doctor's backup-coverage check. A snapshot sync killed on the bound is
reported as a timeout at once, since the server path can only be slower; a
Dolt error offline falls back to the in-place sync. Without copy-on-write, a
`dolt` CLI, or a visible data dir (bd-owned proxied scopes keep their data
elsewhere), the scope syncs in place as before, and the run log says which
mode each scope used and why.

The clone is a crash-consistent copy of a running store, which is also what
`bd backup sync` of a live store produces; Dolt's storage is append-only
behind a manifest, so the clone reads as the last committed state.
