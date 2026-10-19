# Local patch queue

Our fixes to gascity, carried on top of upstream and rebased forward as upstream
moves. This file is the fork's own register: it is NOT offered upstream. Since
2026-10-05 a patch of ours carries its own row in the same commit; a cherry-pick
of an upstream PR carries none, so it stays droppable on its own when the PR
merges, and its row is kept by the register commit.

Restored to this branch on 2026-09-18. It existed only on `patches/on-v1.4.1`
and was lost when the queue was regenerated onto main on 2026-09-17, so for a
day the live branch carried no record of its own patch queue, its rebase recipe,
or the install traps below. If you are reading this on a new branch, carry it
forward.

## Branch shape

The live branch is **`patches/on-main`**. It sits on upstream's `main` rather
than on a release tag, which is the change from the old `patches/on-<tag>`
naming: there is no longer one branch per upstream release.
`patches/on-v1.4.1` is FROZEN. Do not base work on it and do not merge into it.

History: `high/v1.4.1` -> `patches/on-v1.4.1` (2026-09-10) -> `patches/on-main`
(2026-09-17).

## What `gc version` says

A build of this branch self-reports whatever upstream's main claims, currently
**1.4.2**: a clean build of the 2026-10-26 tip is Go's pseudo-version
`v1.4.2-0.<utc>-<12 hex>`, normalized to `1.4.2` (a build with a dirty tree
says `1.4.1`). That is upstream's number, not ours, and it moves when we
rebase. It
is not evidence about which binary is running: verify provenance from the build
info rather than the version string, and see the supervisor section below for
the three places a stale binary hides.

## Patches on patches/on-main

Every commit here is ours except the one marked "Upstream PR ... cherry-picked"
(#4495), which drops when its PR merges. None will turn into an empty commit
and drop out on a rebase by itself; expect to carry each one and to resolve
real conflicts until upstream takes it. As of 2026-10-26, on
`patches/on-main`, rebased onto upstream/main `1904f5b20` (the per-patch
outcome of each rewrite is under "Rebase log" below; what was
dropped, and where to restore it from, is under "Dropped 2026-10-22",
"Dropped 2026-10-20", "Dropped 2026-10-13" and "Dropped 2026-10-05"):

| commit | what |
|---|---|
| `d49581802` | `mise.toml` pins go 1.26.10, which `go.mod` requires and upstream does not. Build environment only. |
| `351bad48c` | `perf(events)`: stops a backward tail walk at the AfterSeq floor. Ours, upstream PR #6374 (open). Re-expressed on upstream's `walkBackward` visit callback (#6063): the floor check sits beside `sinceWindow` in `readFilteredTailFrom` (an `io.ReaderAt`, for the byte-counting test). The PR's head `c0c4cb29e` is the pre-#6063 shape and needs this commit force-pushed to it. The test helper is `writeTailLog` (upstream took `writeLog` and `writeSeqLog`). Drop when #6374 merges. |
| `fa3e9b958` | `perf(dashboardbff)`: projects run lanes on demand rather than on a timer. Ours, upstream PR #6373 (open); its head `84e3f2b54` carries the same change but no `BUILD.bazel` entry for `runtailer_demand_test.go`, which this commit adds. Drop when #6373 merges. |
| `b251a9632` | `feat(cli)`: delegates an unknown command to `gc-<name>` on PATH (Unix: process-image handover; Windows: a supervising fallback). `external_command_unix_test.go` owns the handover: the extension keeps the pid gc was launched as, and a signalled process group yields a death-by-signal exit status. |
| `8fbe5b6e9` | `test(docsync)`: lets the gitignored `scratchpad/` hold notes without failing the gate (ga-e2nr). |
| `7cc380ccf` | This register, restored onto `patches/on-main` on 2026-09-18. Docs only. |
| `a6ec06224` | `fix(test)`: `scripts/test-local-parallel` names the failing jobs when a sharded run fails (PR #10). Its preflight-PID test fix is upstream now (67e786ee7); only the runner half is carried. |
| `8330f941e` | `feat(cli)`: `gc session pending`, `gc status readiness` and `gc session stop-turn <session>`, each with `--json` emitting its route's body, so dispatch can run `gc` on the host instead of calling the supervisor API. Each calls the function its route does: `session.Manager.CityPending` (`ProbeCityPending` reduced by `CityPendingEntries`; upstream's `probeCityPending` snapshot for the pending route now takes its rows from `ProbeCityPending`), `api.ProbeReadiness` (shared by both readiness routes) and the stop route / `Manager.StopTurn`. Command census ids 212-214 (`next_id` 215). Upstream's stop route now takes an optional `SessionStopBody` (`turn`, #8140); `Client.StopSessionTurn` sends an empty one, which stops whatever turn is running. Touches upstream-owned `internal/session`, `internal/worker`, `internal/api` (pending and readiness handlers, plus `NewCityScopedClientWithHTTPClient` so client tests use an in-process transport) and `cmd/gc` (ga-6jam). |
| `ed82efe1a` | `feat(mail)`: read-mail retention is configurable per recipient. `[mail] archive_read_after` (default `"1h"`, `"0"` = never) replaces the watchdog's hard-coded hour, and `[[mail.recipient]]` entries (exact address or `path.Match` glob, first match wins) override it and `retention_ttl`. `config.MailRetentionPolicy` answers both windows per address for the archive sweep, its dry-run count and the wisp-GC purge; `gc order sweep-nudge-mail --mail-ttl` replaces the policy only when given. With `archive_read_after` unset the archive window is `retention_ttl` when set, else 1h, which is upstream's sweep behaviour since 06b47b6af; with no new config the behaviour is upstream's. Touches upstream-owned `internal/config` (`MailConfig`, load validation), `internal/mail/beadmail` (`SweepReadMessages`, `CountReadMessages`, `PurgeReadMessageWisps`) and `cmd/gc` (nudge-mail sweep and watchdog, `sweep-nudge-mail`, wisp GC), plus regenerated reference docs (ga-w27v3). Upstream's unread-mail phase (`[mail] unread_retention_ttl`, `--unread-mail-ttl`, #5257) stays city-wide beside the policy. `[[mail.sender]]` entries (ga-1bzj, below) extend the same policy to the sender. |
| `315c34e77` | Register: table and rebase log for the move onto upstream `4b26deb7c`. Docs only. |
| `5ed376d3c` | `fix(hook,controller)`: a workflow root is claimed and counted as demand only while it is the work. `gc hook --claim` skips a routed root while one of its steps is in progress under a session, or when it has live steps and none is ready work routed to the claimant (open, unassigned, not deferred, off every dispatch hold, not blocked by another live step of the root); spec sidecars and scope latches never count as ready. A root with no live steps (root-only molecule) is still a launch, and so is a fresh root whose first step is ready here; a failed step read fails OPEN. A session that claims a released step adopts that step's open, unassigned root (compare-and-swap). Before serving a held root as `existing_assignment`, the hook reads its live steps: a ready step routed here is claimed in its place, otherwise the root is released (`bead.claim_released` reason `workflow_root_not_work`). The controller's default scale check asks the same question (`demandServableForTemplates`). New `cmd/gc/workflow_root_held.go`; touches upstream-owned `cmd/gc/cmd_hook_claim.go`, `claim_class_route.go` and `build_desired_state.go`. Makes the city's `orders/root-adoption-sweep` unnecessary (ga-tf857, bgc-9yh, bgc-jt6h, bgc-3q3y). The classification wraps upstream's adoption body (#8080's started-claim verdict, #7944's stopped-workflow check) in its default case. The root read `adoptReleasedHookWorkflowRoot` makes after a minted claim is one more bd call than upstream's `TestHookClaimAnnouncesClaimToRunProjection` allowed after the last write; the test now expects it. |
| `245b0043e` | `feat(cli)`: `gc formula list --rig <rig>` lists that rig's scope (city layers beneath the rig's own, `FormulaLayers.SearchPaths(rig)`), the paths `gc formula show --rig` compiles against and `GET /formulas?scope_kind=rig` serves. It took the global `--rig` and ignored it. Without `--rig` the list is unchanged (every layer); an unbound `--rig` errors. Touches upstream-owned `cmd/gc/cmd_formula.go` (bgc-cya). |
| `fd32b7477` | `feat(dispatch,hook,config)`: `[workflows] fail_halts` (off by default). With it on, a needs edge on a step whose terminal `gc.outcome` is fail halts the dependent: it closes skipped with `gc.halted_by` naming the failed step, at claim time in `gc hook --claim` and in control processing, cascading. Finalizers (workflow-finalize, scope-check, `gc.scope_role=teardown`) are never halted. A workflow root cannot close pass while any step's terminal outcome is fail (a retried step's is its last attempt's); it closes fail and names the step on the root and the source bead. Off, behaviour is unchanged. New files `internal/dispatch/fail_halts.go`, `cmd/gc/hook_claim_fail_halts.go`, `internal/config/workflows.go`; touches upstream-owned `internal/dispatch/runtime.go` (ProcessControl, scope body close, workflow finalize) and `cmd/gc/cmd_hook_claim.go`. On upstream's read-trimmed dispatcher (#7456, #7462, #7468) the switch is `opts.failHalts()` inside `terminalAbortScopeFailureMember`, and a halted scope body is closed skipped in the same single write as its propagated member metadata (`haltScopeBody` through the scope's `scopeBodyCloser`). The root's fail diagnostics ride upstream's quarantine-guarded root close (#7749): `completeWorkflowRoot` takes the close metadata and calls `closeWorkflowRootWithMetadata`, which `closeWorkflowRoot` now wraps. City bead bgc-b9e (from bgc-0mh, evidence gcd-pozhh7: four fail steps, root closed pass). |
| `7b10f07fd` | `fix(api)`: readiness probes see a gateway env, from the process or the city's provider env. When the probe's environment has `ANTHROPIC_BASE_URL` plus `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` (the teamclaude proxy shape), `claude` reports `configured` ("authenticated through ANTHROPIC_BASE_URL") instead of running `claude auth status`, which sees only the scrubbed probe env; a base URL without a key, or a key without a base URL, still falls through to the login check. Each provider's probe also takes that provider's resolved, launch-expanded `[providers.<name>.env]` from the city config; its keys override the process env for that provider only (claude's gateway check, mimocode's `XIAOMI_API_KEY`, zcode's `ZCODE_*`) and are appended to `claude auth status`'s scrubbed env. The probe cache key hashes the env. `gc status readiness` loads the enclosing city best-effort and the city routes pass their config; the supervisor routes have no single city and keep the process env. Upstream (#7264) now forwards the process's `ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN` to `claude auth status`; that forwarding is kept and the configured env is appended after it. New `api.ProbeCityReadiness`; touches upstream-owned `internal/api/handler_provider_readiness.go`, `huma_handlers_city.go`, `huma_handlers_supervisor.go` and `cmd/gc/cmd_status_readiness.go` (bgc-a5t, bgc-15s). |
| `a1a75c402` | `fix(sling)`: a formula slung onto a blocked bead waits on ONE start bead. The graph.v2 bead-sling paths (`--on`, default formula, convoy batch) and `gc formula cook --attach` read the source bead's unsatisfied ready-blocking deps (`sourceworkflow.ReadSourceBlockers`; satisfied ones and workflow roots dropped) and pass them as `molecule.Options.Gates`; a blocker the workflow's store cannot resolve is recorded on the root as `gc.source_unprojected_blockers` and reported. `molecule.GateRecipe` adds `<root>.start-gate`, a control bead of the new kind `start-gate` titled "Wait for <blockers> (blockers of <source>)" that carries the gates as its own deps; the root and the entry steps (no needs) block on it, later steps wait through their needs. Applied before graph routing (sling materialize, cook `--attach`) so it routes to the control dispatcher; `dispatch.processStartGate` closes it pass once its deps are satisfied, so nothing is Ready, no pool demand, no session held. Graph workflows only. A late blocker is `bd dep add <start> <blocker>`, printed by `gc sling` (and its `--dry-run`, which names the blockers) and documented in Tutorial 06. `start-gate` joins `ControlKinds`, `ScopeCheckExemptKinds` and `EngineMintedOnlyKinds`. Touches upstream-owned `internal/beadmeta`, `internal/dispatch/runtime.go` (ProcessControl case), `internal/formula/types.go`, `internal/molecule/molecule.go`, `internal/sling` and `cmd/gc/cmd_formula.go`; new `internal/sourceworkflow/blockers.go`, `internal/molecule/start_gate.go`, `internal/dispatch/start_gate.go` (bgc-acn). |
| `684c29a6b` | `perf(cli)`: `version`, `completion`, `login`, `logout` and `whoami` skip pack discovery (`rootCommandSkipsPackDiscovery`), so `gc version` inside a city costs what it does outside one (0.29s -> 0.055s CPU, median of 21). None reads city config and packs cannot shadow a core command; the completion scripts are byte-identical in and out of a city. `help`, bare `gc`, `--help`/`-h` and `__complete` keep discovery, because the root usage and dynamic completion list pack bindings. gc has no `--version` flag. Touches upstream-owned `cmd/gc/root_argv.go` (one `case` line and its comment) and `root_argv_test.go`, whose ambient-args test now uses `status` as its ordinary command (bgc-zfi5). |
| `40bd75dab` | `build`: `make build-release` builds gc as upstream's `.goreleaser.yml` ships it (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`) and signs it, refusing on macOS without `GC_SIGN_IDENTITY`; `make install-release` installs that binary through `install` without rebuilding it. Two new targets in upstream-owned `Makefile`, nothing else touched. See "Build and install" (bgc-70k2). |
| `608c1f5d0` | Upstream PR #4495 (atbrace, open), cherry-picked: `perf(cli)`: the registry rig-binding scan in context resolution loads without the builtin-pack refresh (sys-s3pd). This commit matches the PR head `8a17093de` except one test: upstream #7983's `TestGcBdPassthroughRunsNoFullConfigLoad` counted the registry scan's one cold readiness pass for its "registered rig cwd" and "GC_RIG only" shapes, and with this change that scan runs none, so both expect 0. Drop when #4495 merges (bgc-58tk). |
| `cfa8056bd` | `perf(config)`: one-shot commands compose the city config once per process, where `mail check` composed it 30 times, `mail inbox` 25, `sling` 21, `session list` 13, `hook` 12, `nudge drain` 7 and `events` 6 on the-beast. `internal/config/load_memo.go` memoizes `LoadWithIncludesOptions` when a process calls `EnableLoadMemo`; every caller gets a deep copy (reflective, proved complete by `TestLoadMemoCloneCoversConfigTypeGraph`); an entry is served while every file the composition read through its fs, plus `.gc/site.toml`, holds the same bytes; the key leaves out `SkipRevisionSnapshot` and `RepoCacheNonBlocking`, which cannot change a successful composition. `run()` turns it on only for `mail inbox`, `mail check`, `nudge drain`, `hook`, bare `events` (not `--follow`/`--watch`), `session list` and `sling`, confirmed by `root.Find` before the command runs, and off when the invocation returns; never the supervisor or any long-lived command. Pack-cache repairs invalidate it. `TestHotCommandsComposeTheCityConfigOnce` pins 1 composition per command and identical output. CPU on the-beast, median of 5: `mail check` 1.29s -> 0.77s, `sling` 1.44s -> 0.89s, `mail inbox` 1.19s -> 0.81s, `session list` 1.13s -> 0.96s. Upstream's one-store-per-invocation memo (#7601, `invocation_store_memo.go`) already reuses cfg across store opens for `nudge drain`, `mail check`, `hook`, `prime` and `mail send`, and upstream's own config memo (#7864, `cmd/gc/config_invocation_memo.go`, `beginInvocationConfigMemo`) now covers `bd`, `hook`, `mail`, `nudge`, `prime`, `ready` and `session submit`, so the gains above shrink there; ours still covers `sling`, `session list`, bare `events` and `mail inbox`. Re-measure, and consider retiring this for upstream's scope. Touches upstream-owned `internal/config/compose.go` (wrapper), `internal/config/pack.go` (one `case`), `cmd/gc/main.go` (two lines), `cmd/gc/embed_builtin_packs.go` and `cmd/gc/legacy_pack_preflight.go` (bgc-58tk). |
| `8f1600691` | `fix(prompts)`: a blocked worker escalates to `GC_ESCALATION_RECIPIENT`, not a hard-coded human. The core `pool-worker.template.md` and `graph-worker.md` prompts said `gc mail send human`, so a GasCityDispatch implementation worker's BLOCKED mail (`gcd-40pr2a`, 2026-10-04) reached the operator's inbox instead of alice, who triages. Both now send to `"${GC_ESCALATION_RECIPIENT:-human}"` (a shell default; the Go template passes it through untouched), so a city with no recipient set behaves as before. Every host already exports the variable to its sessions (alice on the-beast, jane on the-ansible, locke on the-studio). `TestCoreWorkerPromptsEscalateToConfiguredRecipient` renders both prompts and pins the form. **Left alone on purpose:** the bundled gascity-packs `gc mail send human` lines (superpowers design/spec approval, github-issue-fix/-triage/pr-review human gates), which ask a human to DECIDE; `cmd_mail.go` help and test fixtures; the core formulas' `escalation_target` var, which defaults to `human` under its own tested contract (`TestCoreEscalationTargetDefaultsToHuman`) and is set per sling through `formula_vars`; and `orphan-sweep.sh` / `spawn-storm-detect.sh`, which read `GC_ESCALATION_TARGET` under `TestCoreEscalationScriptContract`. Touches upstream-owned prompt assets only (bgc-2e06). |
| `a73d1e795` | `perf(dispatch)`: `ensureDrainItemRoot` queries live (non-closed) roots first and falls back to the closed-inclusive scan only when no live candidate exists. On a store with ~50k issues (~49.6k closed) the live-only query is 9ms vs 420ms for the full scan; during a drain burst the full scan was 37% of active Dolt samples. New `findExistingDrainItemRoot` in upstream-owned `internal/dispatch/drain.go`, on upstream's `sourceworkflow.KeyedRoots` (#7742: only a workflow root answers the key) for both queries. Upstream PR #7637 (open); its head is the pre-`KeyedRoots` shape. Squashed with its genclient revert (`19a4f143b`); no generated change is left. |
| `a2702a6bf` | `fix(nudge)`: a delivered tick that re-queues sleeps the poll interval (with exponential backoff for consecutive re-queues, capped at 30s) instead of spinning at query speed. `nudgePollSleep` test seam; `nudgePollDeliveredBackoff` computes the backoff, never below the interval. A tick that finds nothing due resets the backoff (alice's review commit): it counts ticks where the same item stays due, not deliveries over the poller's life, so a busy session's separate nudges keep one-interval latency. Before: one poller issued ~1.8k queries in 45s, a full core of Dolt server time. After: at most one tick per interval. Touches upstream-owned `cmd/gc/cmd_nudge.go` (ga-ddtkw, ga-yo1j). Upstream PR #7652 (open). Squashed with alice's review commit. |
| `604bcefd2` | `fix(retention)`: wisps-plane retention covers nudge and session wisps, not only order-tracking. The existing `runOrderTrackingRetentionWatchdog` (100 deletes / 15 min, 7d TTL) cannot keep pace with ~8.7k tracking wisps/day; nudge wisps (22.8k closed) and session wisps (6.2k closed) had no retention at all. New `runWispsPlaneRetentionWatchdog` sweeps closed nudge beads (by label `gc:nudge`) and closed session beads (by type `session`, skipping parents with children) on a 5-min interval with a 1000-delete budget, sharing `doctor.BulkDeleteSafe`. Default `delete_after_close` drops from 7d to 24h for all three categories (`ApplyBeadPolicyDefaults` now covers `"nudge"` and `"session"` alongside `"order_tracking"`). The order-tracking watchdog's own interval drops to 5 min / 1000 budget to match. Both watchdogs log the eligible-but-undeleted backlog when the budget is exhausted. Startup advisory warns when >100 closed nudge/session beads are present. **Hard constraint: archived mail (type `message`) is never deleted** — both sweep functions filter by label/type, and `TestSweepRetention_NeverDeletesArchivedMail` pins this. New `cmd/gc/wisps_plane_retention.go` and `wisps_plane_retention_test.go`; touches upstream-owned `cmd/gc/city_runtime.go`, `order_dispatch.go`, `orders_lane.go`, `doctor_order_tracking_retention.go`, `cmd_order.go` and `internal/config/config.go` (ga-g6ih, from ga-yix75). The nudge sweep skips every `type=message` bead regardless of labels (alice's review commit): closed mail addressed to a human is the archive. |
| `48a6fd28a` | `perf(retention)`: time-bound each wisps-plane retention pass and batch deletes. A 500-bead backlog at ~2.5 s per `deleteWorkflowBead` round trip blocked startup order dispatch for 20 min (alice, the-beast restart on 63bc69b97). Three changes: (1) each pass is bounded by a wall-clock deadline (1 s on startup, 5 s steady-state) in addition to the existing count budget — whichever triggers first stops the sweep; (2) deletes are batched via `deleteWorkflowBeadsBatch` in pages of 50, replacing per-bead `deleteWorkflowBead` with one set-level descendants check and one batch delete per page; (3) the first pass after startup uses a small time budget so order dispatch is never gated on a bulk delete. The deadline also bounds the round trips around the deletes (alice's review commit): the session sweep checks each aged session for children only until the deadline or the budget, deleting childless sessions a page at a time as it finds them, and the per-bead fallback for a page refused over live descendants checks the deadline before each delete. New `retentionSweepOpts` and `retentionSweepResult` types, `deleteBatchedWithDeadline` batching function. `TestSweepStopsAtDeadline`, `TestSweepBatchesDeletions`, `TestWatchdogStartupUsesSmallBudget`, `TestDeleteBatchedWithDeadline_DeadlineStopsBeforeBudget` and `TestRetentionSweepOpts_PastDeadline`, the session sweep's `ChildrenLookupsStopAtDeadline`/`ChildrenLookupsStopAtBudget`/`DeletesAcrossPagesWithinDeadline` and `TestDeleteBatchedWithDeadline_LiveDescendantsFallbackStopsAtDeadline` pin the behaviour with a fake clock (no `time.Sleep`). **Hard constraint preserved: archived mail (type `message`) is never deleted** — `TestSweepRetention_NeverDeletesArchivedMail` still passes. Touches upstream-owned `cmd/gc/wisps_plane_retention.go`, `wisps_plane_retention_test.go` and `city_runtime.go` (ga-pijy, follow-up to ga-g6ih). |
| `aab1e8abf` | `fix(backup)`: `mol-dog-backup.sh` syncs a large scope from a copy-on-write snapshot before `bd backup sync` records it, and names the bound when a sync is killed. `bd backup sync` runs inside the sql-server, so one sync is cut at `listener.read_timeout_millis` however high `GC_DOLT_BACKUP_SYNC_TIMEOUT_SECS` is set (beast-gas-city 2026-10-12: the 14 GB `gcd` store was killed at the script's 120 s bound on every run and, by hand with no bound, died at exactly 600 s with `connection was closed`; the same sync took 17 min from an APFS clone with no server in the loop). When `<GC_DOLT_DATA_DIR>/<db>/.dolt/repo_state.json` exists, `dolt` is on PATH and a CoW `cp` works (`cp -c`, then `cp --reflink=always`, probed once per run against the data dir), the script clones the data dir under the artifact dir's parent (or `$TMPDIR`), strips `sql-server.lock`/`.info`, runs the bounded `dolt backup sync <name>` inside the clone (`<name>` from the clone's `repo_state.json`: `<db>-backup`, then `default`, else the only one), removes the clone (also on EXIT), then runs the in-place `bd backup sync` as the cheap catch-up that records `last_sync` for bd and the doctor. A snapshot sync killed on the bound is reported as `<db>(timed out after Ns)` without an in-place retry; a Dolt error offline falls back to in-place; the in-place bound kill now also reads `timed out after Ns` instead of `sync failed` (`sync_scope` returns the final exit status; it prints upstream's first-attempt detail, #6363). `GC_DOLT_BACKUP_SNAPSHOT=0` turns the step off. No second bound knob: `GC_DOLT_BACKUP_SYNC_TIMEOUT_SECS` already exists on main. New `examples/bd/dolt/docs/backup-order.md` lists every `GC_BACKUP_*`/`GC_DOLT_BACKUP_*` variable and the snapshot route; `orders/mol-dog-backup.toml` header points at it. Tests in `examples/bd/dolt/backup_order_test.go`: `TestBackupOrderSyncsFromACopyOnWriteSnapshotBeforeBdRecordsIt` (clone cwd, lock stripped, live lock kept, clone removed, both syncs under the 7 s knob, `default` and legacy `prod-backup` names), `TestBackupOrderSyncsInPlaceWithoutCopyOnWrite`, `TestBackupOrderSnapshotCanBeTurnedOff`, `TestBackupOrderNamesTheBoundWhenTheSyncIsKilled` (snapshot and in-place), `TestBackupOrderFallsBackToInPlaceWhenTheSnapshotSyncErrors`; `writeRecordingTimeout` now shadows `gtimeout` too, so the two existing bound tests pass on a Mac with coreutils. The cities run this script from a pack pinned to upstream 32d68e0b8 (the old `run_bounded 120 dolt backup sync` shape); the pins move to the fork branch carrying this fix. Touches upstream-owned `examples/bd/dolt/assets/scripts/mol-dog-backup.sh`, `backup_order_test.go`, `orders/mol-dog-backup.toml`, adds `docs/backup-order.md` (ga-0c9lu). |
| `2a6479d14` | `fix(dolt)`: `mol-dog-doctor.sh` counts a database as backup-eligible when `dolt backup` lists a remote named `<db>-backup` **or** `default`, or exactly one remote under any name, the order `mol-dog-backup.sh` already resolves the name in (`snapshot_backup_name`). Since #6751 the backup dog syncs through `bd backup init/sync`, and bd registers the remote as `default` (`.beads/dolt/gcd/.dolt/repo_state.json` carries only `default -> file:///…/.dolt-backup/gcd` after the first run; the old `gcd-backup` registration is gone), so the doctor's `grep -qx "${db}-backup"` reported every store as `<db> backup remote missing` through `gc dolt health` on every 30 s tick although all seven had just synced (beast-gas-city 2026-10-13 00:55Z, right after the bd pack moved to `0a9337dea`). The comment above the check names the three accepted shapes and still cites #3176. New `TestDoctorAcceptsBdDefaultBackupRemote` in `examples/bd/dolt/dog_exec_scripts_test.go` (`default <url>` eligible, a single `offsite <url>` eligible, two unmatched remotes still reported); `TestDoctorWarnsOnUserDBsMissingBackupRemote` keeps the `<db>-backup` case green. Touches upstream-owned `examples/bd/dolt/assets/scripts/mol-dog-doctor.sh`, `dog_exec_scripts_test.go` (follow-up to ga-0c9lu). |
| `d4ac9cd95` | `fix(dolt)`: `mol-dog-doctor.sh` reports a backup stale only when it lags the live data, not when its manifest is merely old. A manifest older than `BACKUP_STALE_S` now triggers a read of `dolt log -n $GC_DOCTOR_BACKUP_LAG_SCAN_COMMITS` (default 100, bounded by `run_bounded 30`) in `$DOLT_DATA_DIR/<db>`, with Date: lines parsed to epoch in awk (GNU and BSD `date` disagree); the scope is stale only if commits are newer than the push, reported as `<db> backup is Nh old, K commits behind` (`100+` at the scan depth). No newer commit counts the scope current; an unreadable log falls back to the age rule as `..., live commits unreadable`. Ownership comes cold from `gc rig list --json` plus each scope's `.beads/metadata.json` `dolt_database` (never bd, which wakes a suspended scope): a suspended rig's lag carries `(rig <name> suspended)`, and a database no scope owns reads `<db>: no rig; left out of backups` instead of aging forever; ownership is treated as unknown (no such line) when jq, the rig list or any listed scope's metadata is missing. The ok line gains `backups current: X/Y`. A scope with no `.beads/metadata.json` is read from the `.beads/dolt-backup.json` that `bd backup init` wrote (its `backup_url` ends in the database name; beast-gas-city's scopes carry only that file). The header's RPO note says what is measured; `BACKUP_STALE_S` keeps its meaning and default. Evidence: hilton-gas-city 18:48Z (every quiet rig "12h old" with zero commits since its 06:46Z push) and beast-gas-city 19:02Z (suspended gsc/hcm/hfa/hli, orphan jrl), 2026-10-13. Tests in `examples/bd/dolt/dog_exec_scripts_test.go`: `TestDoctorBackupFreshnessMeasuresLagNotAge` (current, lagging, unreadable), `TestDoctorBackupLagCountIsCappedAtTheScanDepth`, `TestDoctorBackupFreshnessResolvesRigOwnership` (suspended quiet/dirty, no-rig), `TestDoctorBackupOwnershipUnknownWhenARigHasNoMetadata`, `TestDoctorBackupOwnershipReadsBdBackupRegistration`. Touches upstream-owned `examples/bd/dolt/assets/scripts/mol-dog-doctor.sh`, `dog_exec_scripts_test.go` (ga-870q). |
| `6de60e69d` | `fix(cli)`: `gc session pending` reaches the supervisor in a supervisor-managed city. `sessionPendingAPIClient` is `supervisorFallthroughAPIClient`, as `gc status` and maintenance are; plain `apiClient` returned nil whenever the controller socket was alive with no `[api]` port, so every run took the local fallback (logged `reason=controller-down`), opened the city store and probed each runtime from the CLI. Measured on hilton-gas-city: 22-29s and 5-6 Dolt connections per run before, 0.8-3.1s and none after; beast-gas-city 3.6-12s before, 0.4-1.9s after. Test `TestSessionPendingAPIClientRoutesToSupervisor` (ga-6d77). |
| `f9d73f383` | `fix(dolt)`: `mol-dog-doctor.sh` measures backup staleness from the oldest unpushed commit, not the push (fork PR #34, ga-574r, ga-bsxn). `backup_live_lag` counts a scope's lag on the server in one query through the doctor's SQL path, ``SET time_zone = '+00:00'; SELECT COUNT(*), FLOOR(UNIX_TIMESTAMP(MIN(date))) FROM `<db>`.dolt_log WHERE date > FROM_UNIXTIME(<push>)``, replacing ga-870q's `dolt log -n` read of the data directory: no depth cap, and no CLI contention with the server for the database's locks. The session is pinned to UTC because `dolt_log` dates are UTC. A scope whose push is past `BACKUP_STALE_S` is stale only when its oldest unpushed commit is itself past it (`<db> backup is K commits behind; oldest unpushed change is Nh old`). Each query is bounded by `GC_DOCTOR_BACKUP_LAG_TIMEOUT_SECS` (default 30) and all scopes share `GC_DOCTOR_BACKUP_LAG_BUDGET_SECS` (default 90); a spent budget reads `lag not read (freshness budget spent)`, a failed query `live commits unreadable`. Tests in `examples/bd/dolt/dog_exec_scripts_test.go`. Touches upstream-owned `mol-dog-doctor.sh`, `dog_exec_scripts_test.go`. |
| `548adfd30` | `fix(dolt)`: the doctor formats every backup age with `backup_age_text`, so a sub-hour stale threshold no longer reads `0h old` (fork PR #34). |
| `b4f7ff79a` | `fix(mail)`: supervisor mail is from `controller`, not `human` (fork PR #35, ga-ei24). Exec orders (the Dolt doctor's advisory, the reaper's escalation, orphan-sweep, spawn-storm, the human-gate notifiers) send via `gc mail send` with no `--from` and no session identity, so the sender fell back to the operator's `human`. `orderExecEnv` now sets `GC_MAIL_FROM=controller` (a reserved exec env key), and a `--from`-less send uses `$GC_MAIL_FROM` only when `GC_SESSION_ID`/`GC_ALIAS`/`GC_AGENT` are all unset, so a session still sends as itself and an operator shell is still `human`. |
| `1ac072677` | `fix(mail)`: states the exec-order sender rationale once (fork PR #35). Comments only. |
| `56f89c455` | `fix(reaper)`: the closed-wisp purge never deletes mail. Step 3 ran `gc bd purge --wisps-plane --older-than 168h`, which cannot leave a type out, so archived mail went 7 days after it closed (23 archived mails on one city). Step 3 now selects its rows read-only (`purge_candidates_query`: closed wisps-plane rows past `PURGE_AGE`, never a `message` by type or `gc:message` label, never a pinned row or one a not-yet-closed bead in either plane still depends on through parent-child, tracks or blocks) and deletes them through `gc bd delete --force --json --from-file`, oldest closed first, `GC_REAPER_PURGE_BATCH` at a time within `GC_REAPER_PURGE_BUDGET_SECS`. A batch that deletes nothing is an anomaly; a `GC_REAPER_PURGE_AGE` that is not whole hours skips the step. `GC_REAPER_PURGE_PLANE_FLAG` and the `bd-purge-unsupported` outcome go. The three `TestMaintenanceOrdersOnRealBd*` integration tests seed an old closed message and an old closed `gc:message` bead: on upstream's reaper both are purged, now both are kept. Upstream's workflow-root census tests saw "later steps ran" as a `bd purge` call; they now read the purge's candidate select. Touches upstream-owned `internal/bootstrap/packs/core/assets/scripts/reaper.sh` and `examples/gastown/maintenance_*_test.go` (ga-hxn2). |
| `b44f5eb10` | `fix(tmux)`: the city server forwards OSC 8 hyperlinks to every xterm client. tmux 3.7c enables `hyperlinks` only for terminals it recognizes by XTVERSION; Ghostty attaches as `xterm-256color` and had every link stripped. `ConfigureServer`, which runs after every session create, appends `xterm*:hyperlinks` to `terminal-features` after `exit-empty off`, only when no entry already equals it (each `-a` adds an array entry). A server that cannot report `terminal-features` (tmux < 3.2) is an error. Unit tests in `lifecycle_test.go`; `TestConfigureServerForwardsHyperlinksAcrossServerRestart` (integration, real tmux, test socket). Touches upstream-owned `internal/runtime/tmux/tmux.go`, `lifecycle_test.go`, `BUILD.bazel`; new `terminal_features_integration_test.go` (ga-hxn2). |
| (register) | This register's table and rebase log, last for upstream `1904f5b20`, the second-to-last commit. Docs only. |
| (census, last) | Re-baselines the resource census (+1 call in +1 file over upstream) for upstream `1904f5b20`: the generated `test/test-resource-counts.toml` gains the `external_command_unix_test.go` row. No dashboard bundle commit: no carried patch touches the SPA, so upstream's `dist/` is current. Redo both on every rebase (see below). |
| (ga-l14q) | `fix(workquery)`: a pool worker is offered the highest-priority ready bead first, FIFO by sling time within a priority, within each tier of the default work query. Tiers are still read in order, and the bd 1.0.4 assigned wisp tier serves a wisp with no dependencies before it checks those with dependencies, ranking each list on its own. The routed tier's executable-before-workflow-root preference (`preferExecutablePoolDemandScript`, #5474) moved every graph.v2 root behind every executable row, so a P1 root slung after P2 steps or tasks waited until they drained; it is now a stable `sort_by(priority, is_anchor)`, demoting a root only within its own priority band. The bd 1.0.4 wisp tiers (`legacyEphemeralReadyFilterJQ` and its dependency-candidate twin), which sort `bd query` rows in jq, sorted by `created_at` alone and now sort by `(priority // 2, created_at, id)`, the readers' canonical order. The migration fallback and the held-anchor continuation probe keep `--sort oldest` (#5629 left both deliberately). The reconciler count form is order-free, so which pool wakes is unchanged. Touches upstream-owned `internal/config/workquery.go` and its 36 workquery goldens; tests in `internal/config/workquery_priority_order_test.go` and `cmd/gc/cmd_hook_claim_priority_order_test.go`. |
| (ga-h56n) | `fix(session)`: a stale-key relaunch consumes the continuation reset it performs, and the reconciler clears an orphaned one on a live runtime. `Manager.ensureRunning` resumed a runtime-missing session (`gc session attach` after `gc session wake`), the `--resume` start died on the gone conversation, `clearStaleResumeMetadata` recorded `continuation_reset_pending=true`, and the fresh relaunch's mint left it pending, so the row read `active  reset-pending` once the runtime was live and the next wake would have discarded the new conversation (hilton-gas-city `hgc-wisp-ny920ve`, 2026-10-16). `retryFreshStartAfterStaleKey` now mints with `consumeReset`, so one CAS bumps `continuation_epoch` and clears the marker, and the relaunch runs under that epoch as `GC_CONTINUATION_EPOCH`; a relaunch that fails withdraws its mint, restoring the marker, and an orphan-cleanup failure mints nothing and keeps it. The legacy runtime-only relaunch keeps its caller's incarnation and leaves the marker to the caller's start commit. The legacy reconciler, on a running and alive runtime, calls `Store.ClearOrphanedContinuationReset` (fenced, re-decided on a fresh read by `OrphanedContinuationReset`: committed active/awake, no `restart_requested`, `reset_committed_at` or create claim, no tmux restart flag; a `wake_request` on that row is served and does not keep the marker) and keeps the epoch. Requirements SESSION-START-021, SESSION-RECON-022. Tests `internal/session/chat_stale_key_reset_test.go`, `continuation_reset_orphan_test.go`, `cmd/gc/session_reconciler_orphaned_reset_test.go`; `TestStaleKeyRetryMintsAgain` and four `manager_test.go` assertions that pinned the re-armed marker now expect it consumed. Touches upstream-owned `internal/session/chat.go`, `fields.go`, `REQUIREMENTS.md`, `cmd/gc/session_reconciler.go`. |
| (ga-1bzj) | `feat(mail)`: read-mail retention is configurable per sender. `[[mail.sender]]` entries take the fields and value semantics of `[[mail.recipient]]` (`match` as an exact address or `path.Match` glob against the bead's stored sender, so `human` works; `archive_read_after`; `retention_ttl`; first match wins; a duplicate `match` within the table fails load). When a sender entry and a recipient entry both match a message, each window is the longer of the ones they set (`"0"`, never, is longest), and a window neither sets comes from `[mail]`; with no match on either side the `[mail]` defaults apply. A sender rule decides only when mail is archived and purged, never whether it reads as read. `MailRetentionPolicy.For(sender, recipient)` answers both windows, `ShortestArchiveReadAfter`/`ShortestRetentionTTL` (the wisp-GC purge gate) and `ArchiveVariesByMessage` (was `ArchiveVariesByRecipient`) count sender entries, and `gc order sweep-nudge-mail --mail-ttl` overrides them as it does recipient entries. Touches the same upstream-owned files as `43e94f0e1`, plus regenerated reference docs. |

## Dropped 2026-10-22

Upstream has each of these, another way. To restore one, cherry-pick it from
tag `patches/on-main-pre-rebase-2026-10-22` or from
`origin/patches/on-main@9bfd5047c`, where every old SHA below lives.

| old commit | patch | why |
|---|---|---|
| `db61c95fe` | `fix(controller)`: an answered nudge restarts the execution-claim backstop's window (bgc-u92). | **Upstream has it another way.** #7984 (`d4c2e4fb3`) holds a started seat `executing` once its nudges are spent and never drains it, and since #8080 (`8d113cd48`) every `gc hook --claim` hand-over writes the `work_started` cell that `markedStarted` reads, so every hook-claimed seat is started. #7984 also pins that pane output cannot reset the pacing (`TestExecutionBackstopRidesOutNonTurnPaneOutput`, from a host `wall` broadcast that bumped every pane), and that a started seat answering its nudges gets exactly the 3-attempt cap (`TestExecutionBackstopNeverDrainsAStartedSeatWaitingOnAHuman`: 119 nudges in 8h with this patch). Under `session_reconciler = "v2"` the work ladder (#8086, `work_ladder` on by default) replaces the backstop. Left uncovered: an UNSTARTED seat that answers nudges still drains after 3. |
| `fe9fff26b` | `perf(bd)`: `gc bd` loads city.toml once and routes a by-ID subject by its prefix (bgc-vpn7). | **Upstream has it.** #7983 (`903c683f5`: the `cityOnly` resolution, `resolveCityPathOnly`, and `loadCityConfigForBdPassthrough`), #7864 (`f6e79b3b2`: one composed load per passthrough, `TestGcBdPassthroughLoadsCityConfigOnce`) and #7986 (`2538e2e5f`: `bdSubjectPrefixTarget` routes a by-id read or write by its subjects' prefix with no probe, with the same behaviour change ours documented). Not kept: the `GC_BD_TRACE` line for the passthrough exec (`beads.TraceBDPassthrough`) and `workspacePinnedBdBinary`'s single parse. |
| `a19ad35c1` | `fix(dolt)`: the doctor reads a scope's database from bd's backup registration when metadata.json is absent. | **Squashed**, not dropped: a review fix of ga-870q, now part of `2f8d57821`. |

## Dropped 2026-10-20

The build host is retired (ga-9oty), so nothing hands work to it. To restore
the patch, cherry-pick `f9a411891`; it stays in `patches/on-main`'s history.

| old commit | patch | why |
|---|---|---|
| `f9a411891` | Sends the heavy go sweeps to the build host instead of this laptop: the remote-run hand-off in `.githooks/lib/push-suite.sh`'s go mode (`run_go_suite`: `GC_REMOTE_RUN`/`GC_REMOTE_HOST`, exits 69/75 fall back to local) and the remote-run bullet under `AGENTS.md`'s "Build and test". | **Retired.** the-forge and high-gas-city are gone. Upstream never had the hand-off; `push-suite.sh`, `scripts/githooks_pre_push_suite_test.go`, `scripts/githooks_pre_push_stdin_test.go` and `AGENTS.md` are upstream's again (`8e3d40151`). |

## Dropped 2026-10-13

Upstream has each of these, or rewrote the code under it. To restore one,
cherry-pick it from tag `patches/on-main-pre-rebase-2026-10-13`, where every old
SHA lives.

| old commit | patch | why |
|---|---|---|
| `668ceb808` | Cherry-pick of #7082's `abfc769bd`: the cache emits `bead.closed` exactly once for every close it observes. | **Upstream has it.** #7082 closed unmerged, and its port landed as #7264 (`9d1f0159f`, "forward-port #6881 supervisor API fixes to main, 1/2"); `unannouncedCloses` and `caching_store_close_announce.go` are on upstream/main. |
| `016ff8b26` | `fix(session)`: `Manager.Submit` reports a delivered-but-unobserved submit as sent (ga-yhxss). | **Upstream has it.** #5977 (`08ec7f0bc`) adds `runtime.ErrNudgeDeliveredUnobserved`, wraps the tmux error in it, and `Manager.nudgeSession` treats it as delivered for send, submit and wait-idle (SESSION-RUNTIME-015, `TestSubmitDefaultTreatsDeliveredUnobservedNudgeAsDelivered`). Our upstream PR #7545 is redundant. |
| `00e2d8f57` | Cherry-pick of #6185 (atbrace): the per-prompt `gc nudge drain` reuses the loaded config and the target store. | **Upstream has it another way.** #6185 is still open, but #7601 (`515eb2b69`, `cmd/gc/invocation_store_memo.go`) opens each scope once per one-shot invocation (`nudge drain`, `mail check`, `hook`, `prime`, `handoff`) and reuses the memo's cfg; `hook_budget_test.go` pins `Opens: 1` for every `nudge drain --inject` case. |
| `e3872e481` | `perf(dispatch)`: the control-ready follower skips its re-list while the Dolt hash is unchanged, primes from one brief list, and shares one `bd version` probe (#13, ga-dtay). | **Obsolete.** #7404 (`8a52426d6`) reads control-ready state fresh on every scan and deleted the snapshot registry this hung on, and #7446 (`5fa137b28`) replaced the per-scan `PrimeActive` with one `Ready` per leg, no `bd version` (`TestTryControlReadyScanMakesOneBDCallPerBdLeg`). `PrimeActiveReadiness`, `ListQuery.Brief`, `BdVersionMemo` and the change-token probe have nothing left to serve. A residual idea (skip even the one `bd ready` on an idle sweep while `dolt_hashof_db()` is unchanged) would have to argue against #7404's freshness rationale. |

## Dropped 2026-10-05

GasCityDispatch no longer calls the gc supervisor's HTTP API; it runs `gc` and
`bd` on the host over SSH, and no city on any host (beast-gas-city,
personal-gas-city, hilton-gas-city) calls the API directly. These patches
changed only API routes or dashboard views nothing reaches, so they were rebase
cost with no user (audit: Alice, 2026-10-05). To restore one, cherry-pick it
from tag `patches/on-main-pre-rebase-2026-10-05` or from
`origin/patches/on-main@826807e9b`, where every old SHA below lives.

| old commit | patch | why it had no user |
|---|---|---|
| `b9ca20854` | `fix(api)`: an output turn carries the entry id its cursors need. | Session-peek output API and the dashboard's `SessionPeek` only. |
| `6d298285b` | `perf(api)`: keys the agent list cache on time, not the event sequence. | Agent list route cache only. |
| `696d9d108` | `feat(api)`: shows what a tool was asked to do, not just its name. | Output-turn API only. |
| `88d7e7a71` | `feat(api)`: names the client on every request, and reports a full-history scan. | Request identity for API clients; nothing calls the API. |
| `74264ce4d` | `fix(api)`: resolves the committed conflict `88d7e7a71` carries in `client_remote.go`. | Repairs only `88d7e7a71`; dropped with it. |
| `2d91b46fa` | The bead API serves a close time and attribution (ga-88ni, PR #9). | Bead API projection fields only. |
| `75755da9e` (API half) | `/runs` confirms a stale non-terminal workflow root against its store (`internal/api/run_root_reconcile.go`, the `huma_handlers_runs.go` change, `server.go` wiring) (ga-bilu, PR #14). | `/runs` route only. Its `internal/beads` half is carried as `c562a7252`. |
| `87552c1d7` | `runs/summary` and the run census apply the `/runs` reconcile (`WithRunCensusSource`) (ga-vw6t, PR #15). | Dashboard BFF lanes only; built on #14's API half. |
| `499356883` | Closed mail served with its status under `GET /mail?status=closed\|any`, `POST /mail/{id}/unarchive`, and `mail.archived` / `mail.unarchived` / `mail.deleted` events (ga-85n6, PR #16). | Mail API routes and their events; nothing reaches them. |
| `21fcfc500` | Dashboard bundle and census re-baseline. | Build output; replaced by `0819ec529` (census only). |

## Rebase log

### 2026-10-26: onto upstream/main `1904f5b20` (ga-xmiz)

30 commits on `340ed8581` (`ea8f01220`, tagged and pushed as
`backup/pre-main-rebase-20261026T194505`) -> 36 on `1904f5b20`, 77 upstream
commits later, in `worktrees/rebase-2026-10-26` on `scratch/rebase-2026-10-26`.
`1904f5b20` is the last upstream commit before #8291 (`f78779e41`, "require
Dolt 2.6.0"); the hosts run Dolt 2.4.0, so the base stops there. It carries
v1.5.1 through #8270 (`814867f5c`), which is a squash, not a merge: the
`v1.5.1` tag is not an ancestor, and `git describe` does not name it. This
replaces ga-hxn2's `patches/on-v1.5.1` (a September base ~1,060 commits
older). One scripted `rebase -i`: the 28 code and docs patches, then fork PRs
#34 and #35 (two commits each), then ga-hxn2's two new patches from
`patches/on-v1.5.1`, the register second to last and a new census last. No
patch was absorbed upstream (no empty commits), so nothing is dropped. Every
"Drop when #NNNN merges" row was checked: #6374, #6373, #4495, #7637 and
#7652 are all still open, so all five are carried.

| old | new | outcome |
|---|---|---|
| `7504f0018` chore(mise): pin go 1.26.10, which go.mod requires and up... | `d49581802` | Carried. |
| `c964fa89b` perf(events): stop a backward tail walk at the AfterSeq f... | `351bad48c` | Carried. |
| `c50ce767a` perf(dashboardbff): project run lanes on demand, not on a... | `fa3e9b958` | Carried. |
| `6fb693caf` feat(cli): delegate an unknown command to gc-<name> on PATH | `b251a9632` | Carried. |
| `8bed24106` test(docsync): let the gitignored scratchpad hold notes w... | `8fbe5b6e9` | Carried. |
| `d2003c8e0` Restore PATCHES.md, which the move to patches/on-main dro... | `7cc380ccf` | Carried. |
| `bfda3e444` fix(test): scripts/test-local-parallel names the failing ... | `a6ec06224` | Carried. |
| `d67b9b04c` gc session pending, gc status readiness and gc session st... | `8330f941e` | Carried. |
| `bf0225b74` Mail retention is configurable per recipient: when read m... | `ed82efe1a` | Carried with conflict: `cmd/gc/nudge_mail_sweep.go`, beside upstream's reworded `liveNudgeIDSet` comment. |
| `27b923532` docs(patches): table and rebase log for the move onto ups... | `315c34e77` | Carried. |
| `30d7193e4` fix(hook,controller): a workflow root is claimed and coun... | `5ed376d3c` | Carried with conflict (below). |
| `c081cacee` gc formula list --rig lists that rig's scope (bgc-cya) | `245b0043e` | Carried. |
| `438ac926f` [workflows] fail_halts: a fail halts its dependent work s... | `fd32b7477` | Carried. |
| `c67d1843c` fix(api): readiness probes see a gateway env, from the pr... | `7b10f07fd` | Carried. |
| `a036bcda1` fix(sling): a formula slung onto a blocked bead waits on ... | `a1a75c402` | Carried. |
| `99ca834f4` perf(cli): skip pack discovery for version, completion, l... | `684c29a6b` | Carried. |
| `a63e2de5d` build: make build-release builds gc as upstream releases ... | `40bd75dab` | Carried. |
| `d2d2903b6` perf(cli): skip builtin-pack refresh in registry rig-bind... | `608c1f5d0` | Carried. |
| `d46d8c2cd` perf(config): one-shot commands compose the city config o... | `cfa8056bd` | Carried. |
| `6602678b3` fix(prompts): a blocked worker escalates to GC_ESCALATION... | `8f1600691` | Carried. |
| `a63dbfdc0` perf(dispatch): query live roots first in ensureDrainItem... | `a73d1e795` | Carried. |
| `4d32634bc` fix(nudge): sleep the poll interval after a delivered tick | `a2702a6bf` | **Reworked** (below). |
| `b96e1f39c` fix(retention): wisps-plane retention covers nudge and se... | `604bcefd2` | Carried. |
| `0de0336c1` perf(retention): time-bound wisps-plane retention passes ... | `48a6fd28a` | Carried. |
| `35582ef5c` fix(backup): sync a large scope from a copy-on-write snap... | `aab1e8abf` | Carried with conflict: upstream took the `gtimeout` half of `writeRecordingTimeout`; ours adds `FAKE_TIMEOUT_EXPIRE_ON` to its stub. |
| `104cf6679` fix(dolt): doctor accepts bd's `default` backup remote as... | `2a6479d14` | Carried. |
| `2f8d57821` fix(dolt): doctor measures a backup's lag behind live dat... | `d4ac9cd95` | Carried. |
| `8f37dc184` fix(cli): gc session pending reaches the supervisor in a ... | `6de60e69d` | Carried. |
| `5f0293ba1` fix(dolt): doctor measures backup staleness from the olde... | `f9d73f383` | **New to the stack** (fork PR #34); register conflict only. |
| `c4c2296a6` fix(dolt): doctor formats every backup age with backup_ag... | `548adfd30` | **New to the stack** (fork PR #34). |
| `747e25f26` fix(mail): supervisor mail is from controller, not human | `b4f7ff79a` | **New to the stack** (fork PR #35); register conflict only. |
| `dd79dc89f` fix(mail): state the exec-order sender rationale once | `1ac072677` | **New to the stack** (fork PR #35). |
| `3cd1440ee` fix(reaper): the closed-wisp purge never deletes mail | `56f89c455` | **New, reworked** from `patches/on-v1.5.1` (below). |
| `2a00eeec0` fix(tmux): the city server forwards OSC 8 hyperlinks to e... | `b44f5eb10` | **New** from `patches/on-v1.5.1`; `lifecycle_test.go` import conflict, `BUILD.bazel` entry added. |
| `d141f941b` docs(patches): the register for upstream 340ed8581 | register | Rewritten second to last; holds this entry. |
| `ea8f01220` Re-baseline the census for upstream 340ed8581 | replaced | New census commit, last. |

**Needs discussion** (carried, but reworked or with an open question):

- `30d7193e4` workflow root: upstream (#6475 and the hook-claim work after
  #8214) added `retireDoneWorkflowCandidates` before the existing-assignment
  branch of `tryHookClaim`: a candidate whose workflow's input convoy work is
  already terminal is retired (root and open steps skipped) before any tier
  looks at it. It runs first; our held-root classification (`heldRoot`) runs
  on what is left.
- `4d32634bc` nudge poll: upstream moved the loop onto a due-work gate with a
  census cache (`nudgePollGate`) and batched skip counts
  (`countNudgePollSkip`, flushed every 30s). The delivered backoff sits on that
  shape: `gate.hasDue` resets `consecutiveDelivered`, a delivered tick sleeps
  `nudgePollDeliveredBackoff`, a non-delivered one resets the counter, counts
  its skip and sleeps one interval through the `nudgePollSleep` seam.
- `3cd1440ee` reaper mail: written on v1.5.1's reaper; `reaper.sh` merged
  onto main's (workflow-root census, session prune, run budget) without a
  conflict. Main's `TestMaintenanceOrdersOnRealBd*` is three top-level tests
  with workflow-root seeds; the mail seeds and assertions were added to them,
  and their `bdSupportsPurgeWispsPlane` gate removed. Main's workflow-root
  census tests (`TestReaperWorkflowRootCensus*`) asserted "the scope's later
  steps ran" as an `args=purge` bd call, which no longer exists; they read
  the purge's candidate select (`purge_candidate_id`) in the dolt log, or the
  `--rig api sql --csv SELECT w.id AS purge_candidate_id` call for the rig.
  Confirmed against the base: with `1904f5b20`'s `reaper.sh` the three
  integration tests fail only on the mail assertions, in every scope of every
  topology; with the patch all three pass (bd 1.3.1, Dolt 2.4.0).
- Upstream #8214 made the v2 session reconciler the default where a city
  qualifies. Our cities use Dolt-backed bd stores, not `sqlite-beads`, so they
  fall back to legacy (with a boot warning), where `30d7193e4`'s
  `demandServableForTemplates` scale check still applies.

Generated artifacts needed nothing beyond the census: `go run ./cmd/genspec`,
`go run ./cmd/genschema` and `go run ./cmd/gen-command-census -check` leave
the tree clean, and `TestGeneratedClientInSync` passes with oapi-codegen
v2.6.0 on PATH (the mise shim trap again: the `oapi-codegen` shim has no
version under go 1.26.10). No carried patch touches the SPA source, so
upstream's `dist/` stands. Census 765 / 239 against upstream's 764 / 238.
`internal/runtime/tmux/BUILD.bazel` lists the new
`terminal_features_integration_test.go`; every other new Go file was already
listed (checked by script; no Bazel here, so deps were not checked).

Verified on this laptop, cgo with the ICU flags: no conflict markers, gofmt
clean on every changed Go file, every one of the 36 commits builds (`go build
./...`) and vets (`go vet ./cmd/gc ./internal/... ./examples/...`) on its
own, and `go test -race` on `internal/api/...`, `internal/beadmeta`,
`internal/config/...`, `internal/dispatch/...`, `internal/events/...`,
`internal/formula/...`, `internal/mail/...`, `internal/molecule/...`,
`internal/orders/...`, `internal/productmetrics/...`,
`internal/session/...`, `internal/sling/...`, `internal/sourceworkflow/...`,
`internal/worker/...`, `internal/runtime/tmux`, `internal/bootstrap/...`,
`internal/testpolicy/resourcecensus`, `test/docsync`, `examples/bd/dolt`,
`examples/gastown`, `cmd/gc` filtered (in chunks) to
`Retention|Wisps|DeleteBatched|Nudge|Mail|Session|Dispatch|Drain|Hook|WorkflowRoot|FailHalts|Formula|Sling|Bd|StatusReadiness|ExternalCommand|RootArgv|RootCommand|PackDiscovery|ConfigLoadMemo|LoadMemo|ComposeTheCityConfigOnce|Backstop|Prompt|Escalat|ProductMetrics|ProviderFactoryCensus|NoBuiltinRefresh|Order|StopTurn|Pending|Readiness|StartGate|Census|ExecEnv|Supervisor|ConfigureServer|Reaper`
(process tests skip without `GC_FAST_UNIT=0`), and with `-tags integration`
`examples/gastown` `TestMaintenanceOrdersOnRealBd*` and `internal/runtime/tmux`
`TestConfigureServer*`. Failures that reproduce on a clean `1904f5b20`
checkout on this host, so are not ours: `internal/api`
`TestStreamSessionPeekAcceptsPeekCapability`; `examples/bd/dolt`
`TestDoctorCheckVersionFloor*` and `TestRunBoundedPython3Fallback*`;
`examples/gastown` `TestRefineryFormulaNoGh{RESTLookupExecutesNumberAndBranchPaths,PRCreateSendsJSONContentType}`,
`TestOrphanSweep{PreservesProtectedInProgressEphemeralMoleculeWisp,PreservesLivePoolOwnerInOversizedSessionSnapshot,ReportsProtectedIdentitiesWhenEscalationMailFails}`
and `TestReaperSession{StatePruneFailureEscalates,PruneAnomalyEscalates}`;
`cmd/gc` `TestSupervisorCreatesControllerSocketForManagedCity` and
`TestSessionSubmitFallbackDedupe` (both race reports). One more is this
host's mise, not code: seven `cmd/gc` `TestGcBeadsBd*` tests fail whenever
`mise.toml` (`7504f0018`'s pin) sits in the tree, on the stack and on clean
`1904f5b20` alike, and pass on the stack with it moved aside. Not run:
`make check`, the sharded suites, `bazel test`, `make bazel-sync`, `make
dashboard-build`.

### 2026-10-22: onto upstream/main `340ed8581` (ga-okvs)

35 commits on `8e3d40151` (`9bfd5047c`, tagged
`patches/on-main-pre-rebase-2026-10-22` locally) -> 30 on `340ed8581`, 289
upstream commits later, in `~/Developer/gascity-rebase-1022` on
`patches/on-main-rebase-2026-10-22`; `patches/on-main` was not moved. One
scripted `rebase -i`: the retired build-host pair (`f9a411891` and its revert
`9bfd5047c`, net zero to code) dropped, `a19ad35c1` squashed into its patch,
the register and census moved last. Each conflict stop was built, vetted and
given its patch's targeted tests before `--continue`. 2 patches absorbed
upstream and dropped (see "Dropped 2026-10-22"). Every "Drop when #NNNN
merges" row was checked: #6374, #6373, #4495, #7637 and #7652 are all still
open, so all five are carried.

| old | new | outcome |
|---|---|---|
| `a6ba926fd` chore(mise): pin go | `7504f0018` | Carried; pin bumped to 1.26.10 (upstream `go.mod`). |
| `f9a411891` heavy sweeps to the build host | - | **drop: retired 2026-10-20** (with its revert `9bfd5047c`). |
| `d14449af9` perf(events): AfterSeq floor | `c964fa89b` | Carried; `BUILD.bazel` conflict only (upstream's `reader_types_test.go`). |
| `0da1f8abe` perf(dashboardbff): run lanes on demand | `c50ce767a` | Carried with conflict (below). |
| `ba742a2dc` feat(cli): gc-<name> delegation | `6fb693caf` | Carried. |
| `94af97874` test(docsync): scratchpad | `8bed24106` | Carried. |
| `2e39216d7` restore PATCHES.md | `d2003c8e0` | Carried. |
| `47bd05a4c` fix(test): runner names failing jobs | `bfda3e444` | Carried. |
| `350d537be` session pending / readiness / stop-turn | `d67b9b04c` | **Reworked** (below). |
| `43e94f0e1` per-recipient mail retention | `bf0225b74` | Carried. |
| `b552cfa03` docs(patches): 4b26deb7c table | `27b923532` | Carried. |
| `cc5158274` workflow root only while it is the work | `30d7193e4` | **Reworked** (below). |
| `bd359c5cd` formula list --rig | `c081cacee` | Carried. |
| `db61c95fe` answered nudge resets the backstop | - | **drop: upstream has it another way (#7984, #8080)** |
| `aa11eabf3` fail_halts | `438ac926f` | **Reworked** (below). |
| `087cbd38b` readiness gateway env | `c67d1843c` | Carried. |
| `82585653f` start gate for a blocked bead | `a036bcda1` | Carried with conflict (below). |
| `fe9fff26b` gc bd load-once and by-ID route | - | **drop: upstream has it (#7983, #7864, #7986)** |
| `c1e6a1488` version etc. skip pack discovery | `99ca834f4` | Carried with conflict (below). |
| `7b726929a` build-release | `a63e2de5d` | Carried. |
| `e4d425ae4` #4495 cherry-pick | `d2d2903b6` | Carried with one test change (below). |
| `519e3a959` config composed once | `d46d8c2cd` | Carried; `BUILD.bazel` conflict only (needs discussion, below). |
| `6af88a779` escalation recipient | `6602678b3` | Carried. |
| `817158b4b` drain live roots first | `a63dbfdc0` | **Reworked** (below). |
| `0845d4c78` nudge poll sleeps after a delivered tick | `4d32634bc` | Carried; `BUILD.bazel` conflict only. |
| `6646f5a6d` wisps-plane retention | `b96e1f39c` | Carried. |
| `207234933` retention deadline and batches | `0de0336c1` | Carried. |
| `cbd13b8d2` backup from a CoW snapshot | `35582ef5c` | Carried. |
| `19442343d` doctor accepts `default` backup remote | `104cf6679` | Carried. |
| `205d46006` register for 8e3d40151 | register | Rewritten second to last; holds this entry. |
| `289a60384` census for 8e3d40151 | replaced | New census commit, last. |
| `d311a0b30` doctor measures backup lag | `2f8d57821` | Carried with conflict (below); `a19ad35c1` squashed in. |
| `a19ad35c1` doctor reads bd's backup registration | squashed | Into `d311a0b30`'s new commit. |
| `3bb5d644d` session pending reaches the supervisor | `8f37dc184` | Carried. |
| `9bfd5047c` drop the remote-build patch | - | **drop**, with `f9a411891`. |

**Needs discussion** (carried, but reworked or with an open question):

- `350d537be` session pending / readiness / stop-turn: upstream took command
  id 211 (`agent-is-foreign`), so ours are 212-214 (`next_id` 215), census
  artifacts regenerated with `go run ./cmd/gen-command-census`. Upstream #8140
  gave `POST .../session/{id}/stop` an optional `SessionStopBody` (`turn`), so
  `Client.StopSessionTurn` now sends an empty body: stop whatever turn is
  running. `gc session stop-turn` takes no `--turn` yet.
- `cc5158274` workflow root: upstream reshaped the adoption path (#8080's
  started-claim fence and `hookAdoptionVerdict`, #7944's
  `hookClaimStoppedAdoption`). Our root classification still runs first; its
  default case is upstream's new adoption body. Upstream's
  `TestHookClaimAnnouncesClaimToRunProjection` (#7944) pins "only the edge
  confirmation after the last write"; our `adoptReleasedHookWorkflowRoot`
  reads the step's root after a minted claim, so the test now expects that
  one read too. If upstream ever takes this patch, the read should move before
  the stamp or ride the claim's readback.
- `aa11eabf3` fail_halts: upstream #7749 made the finalizer's root close
  quarantine-guarded (`completeWorkflowRoot` / `closeWorkflowRoot`, refusing
  pass on a control-quarantined root). Ours wrote the root with
  `updateMetadataAndClose` directly; it now passes its close metadata (outcome
  plus the failed step's diagnostics) through `completeWorkflowRoot` to a new
  `closeWorkflowRootWithMetadata`, so the guard still runs and the
  diagnostics still ride the single close write. `annotateSourceBeadFailure`
  takes the root bead (upstream). The hook side sits beside #8080's
  `StartedFence` / work_started ops and after #7944's workflow-stop re-check.
- `817158b4b` drain live roots first: upstream #7742 (architecture B) made the
  item-root lookup `sourceworkflow.KeyedRoots`, so a task carrying the key is
  no root. `findExistingDrainItemRoot` keeps the live-first, closed-fallback
  shape on `KeyedRoots`. Upstream PR #7637's head is the old shape.
- `519e3a959` config load memo: upstream's #7864 memo (`cmd/gc`, opened by
  `bd`, `hook`, `mail`, `nudge`, `prime`, `ready` and `session submit`) now runs
  above ours (`internal/config`, `LoadWithIncludesOptions`). Ours still covers
  `sling`, `session list`, bare `events` and `mail inbox`. Opening upstream's
  scope in those four would retire ours; not tried here.
- `0da1f8abe` run lanes on demand: upstream #7944 added `sync` (fold the log
  before a cancel's next read, `SyncRunProjection`). With no watcher our fold
  defers its projection, and the next point read's `awaitProjection` builds it,
  so read-your-writes still holds; `sync` itself does not project.
- `e4d425ae4` #4495 cherry-pick: upstream #7983's
  `TestGcBdPassthroughRunsNoFullConfigLoad` pins the readiness passes the
  city-deciding step runs, 1 for the registry shapes ("registered rig cwd",
  "GC_RIG only"). The cherry-pick's whole point is that the registry
  rig-binding scan loads without that refresh, so it runs 0, which is the
  test's own stated goal ("no builtin readiness pass"). Both expectations are
  0 in this commit, the one drift from the PR head; the PR (open) would need
  the same change against current main.
- `db61c95fe` dropped (see "Dropped 2026-10-22"): whether the city's
  `orders/claim-nudge-reset` is needed again. Started seats are no longer
  drained upstream, so probably not.

Other carried patches with conflicts, no design change: `82585653f`
(`molecule.Instantiate` refuses session steps before applying the start gate,
upstream's check first; `TestSessionFieldUniverse` carries upstream's
`gc.cancel_requested` beside our `gc.source_unprojected_blockers`;
`sourceworkflow` `BUILD.bazel`), `c1e6a1488` (upstream #8084 added `ready` to
the same pack-discovery switch; both lists and both test sets kept),
`d311a0b30` (test-only, beside upstream's compactor tests from #7996). Every
`BUILD.bazel` conflict was a srcs list and was merged as a sorted union; every
Go file the stack adds is listed in its package's `BUILD.bazel` (checked by
script). Census 758 / 234 against upstream's 757 / 233.

Generated artifacts needed nothing beyond the census: `go run ./cmd/genspec`,
`go generate ./internal/api/genclient` (oapi-codegen v2.6.0), `go run
./cmd/genschema` and `go run ./cmd/gen-command-census -check` leave the tree
clean, and no carried patch touches the SPA source, so upstream's `dist/`
stands.

Verified on this laptop (load 20-110 during the runs), cgo with the ICU
flags: no conflict markers, gofmt clean on every changed Go file, every one of
the 30 commits builds (`go build ./...`) and vets (`go vet ./cmd/gc
./internal/...`) on its own, `make build` at the tip, the resource census, and
`go test` on every package a carried patch touches: `internal/api/...`,
`internal/beadmeta`, `internal/config/...`, `internal/dispatch/...`,
`internal/events/...`, `internal/formula/...`, `internal/mail/...`,
`internal/molecule/...`, `internal/productmetrics/...`,
`internal/session/...`, `internal/sling/...`, `internal/sourceworkflow/...`,
`internal/worker/...`, `internal/testpolicy/resourcecensus`, `test/docsync`,
`examples/bd/dolt`, and `cmd/gc` filtered to
`Retention|Wisps|DeleteBatched|Nudge|Mail|Session|Dispatch|Drain|Hook|WorkflowRoot|FailHalts|Formula|Sling|Bd|StatusReadiness|ExternalCommand|RootArgv|RootCommand|PackDiscovery|ConfigLoadMemo|LoadMemo|ComposeTheCityConfigOnce|Backstop|Prompt|Escalat|ProductMetrics|ProviderFactoryCensus|NoBuiltinRefresh|Order|StopTurn|Pending|Readiness|StartGate|Census`.
Three real regressions surfaced and were fixed in their own patches: the
stop route's new body (`350d537be`), upstream's run-projection call budget
(`cc5158274`) and upstream's passthrough readiness-pass count (#4495). Failures
that reproduce on a clean `upstream/main` (`340ed8581`) checkout on this host,
so are not ours: `examples/bd/dolt` `TestDoctorCheckVersionFloor*` and
`TestRunBoundedPython3Fallback*`, and the `cmd/gc` dolt leak guard tripping on
`TestSessionNudgeLaneRoutesUnservedSessions` (it leaves one `dolt sql-server`
behind; every test passes). `internal/mail/exec` (no carried patch touches it)
hit the 10-minute timeout at load ~60 and passed alone in 595 s. Not run:
`make check`, the sharded suites, `bazel test`, `make bazel-sync` (no Bazel
here, so `BUILD.bazel` deps were not checked; srcs were), `make
dashboard-build`, and the pre-commit hook's `lint-changed` (Bazel); the
pre-commit's `golangci-lint fmt` (v2.12.0) was run by hand on the changed file.

### 2026-10-13: onto upstream/main `8e3d40151` (ga-psnq)

39 commits on `e38ce9cc5` (`d83408e81`, tagged
`patches/on-main-pre-rebase-2026-10-13`) -> 31 on `8e3d40151`, 491 upstream
commits later, in `worktrees/rebase-2026-10-13` on
`scratch/rebase-2026-10-13`, pushed as `patches/on-main-rebase-2026-10-13`;
`patches/on-main` was not moved. Triage first, every patch against upstream's
log and code (not `git cherry`, which found nothing): 4 dropped (see "Dropped
2026-10-13"), 4 fix-up commits squashed into their patches, the old census
replaced, 31 carried. Then a scripted `rebase -i`, the register and census
moved last, and a second pass (`exec` after each pick) that gave each patch its
own `BUILD.bazel` entries and bumped the mise pin.

| old | new | outcome |
|---|---|---|
| `1a5119918` chore(mise): pin go 1.26.6, which go.mod requires and upstream does... | `a6ba926fd` | Carried; pin bumped to 1.26.9 (upstream `go.mod`). |
| `c4947f620` Send the heavy sweeps to the build host instead of this laptop | `f9a411891` | **Reworked** (needs discussion, below). |
| `07d01c43e` perf(events): stop a backward tail walk at the AfterSeq floor | `d14449af9` | **Reworked** (needs discussion, below). |
| `a9a9780c0` perf(dashboardbff): project run lanes on demand, not on a timer | `0da1f8abe` | Carried. |
| `7c9e61c8f` feat(cli): delegate an unknown command to gc-<name> on PATH | `ba742a2dc` | Carried. |
| `7483c5b55` test(docsync): let the gitignored scratchpad hold notes without fai... | `94af97874` | Carried. |
| `558f2cf27` Restore PATCHES.md, which the move to patches/on-main dropped | `2e39216d7` | Carried. |
| `6f6a9c447` fix(test): scripts/test-local-parallel names the failing jobs when ... | `47bd05a4c` | Carried. |
| `668ceb808` fix(beads): emit bead.closed exactly once for every close the cache... | - | **drop: upstream has it (#7264, `9d1f0159f`)** |
| `e3872e481` perf(dispatch): the control-ready follower skips its re-list while ... | - | **drop: obsolete (#7404 `8a52426d6`, #7446 `5fa137b28`)** |
| `ad525bcf5` gc session pending, gc status readiness and gc session stop-turn, e... | `350d537be` | **Reworked** (needs discussion, below). |
| `141dab13e` Mail retention is configurable per recipient: when read mail is arc... | `43e94f0e1` | **Reworked** (needs discussion, below). |
| `1d86898e8` docs(patches): table and rebase log for the move onto upstream 4b26... | `b552cfa03` | Carried. |
| `b57ea02ac` fix(hook,controller): a workflow root is claimed and counted as dem... | `cc5158274` | Carried with conflict (below). |
| `45260303d` gc formula list --rig lists that rig's scope (bgc-cya) | `bd359c5cd` | Carried. |
| `8045df379` An answered nudge restarts the execution backstop's window (bgc-u92) | `db61c95fe` | Carried. |
| `de0c9742d` [workflows] fail_halts: a fail halts its dependent work steps, fina... | `aa11eabf3` | **Reworked** (needs discussion, below). |
| `100e3c851` fix(api): readiness probes see a gateway env, from the process or t... | `087cbd38b` | **Reworked** (needs discussion, below). |
| `d9ed79ada` fix(sling): a formula slung onto a blocked bead waits on one start ... | `82585653f` | Carried with conflict (below). |
| `56b7ef6f6` perf(bd): gc bd loads city.toml once and routes a by-ID subject by ... | `fe9fff26b` | **Reworked** (needs discussion, below). |
| `152ddaccd` perf(cli): skip pack discovery for version, completion, login, logo... | `c1e6a1488` | Carried. |
| `1a3cfebbe` build: make build-release builds gc as upstream releases ship it (b... | `7b726929a` | Carried. |
| `5e4c2691e` perf(cli): skip builtin-pack refresh in registry rig-binding scan (... | `e4d425ae4` | Carried. |
| `00e2d8f57` perf(nudge): reuse the loaded config and the target store across th... | - | **drop: upstream has it another way (#7601, `515eb2b69`)** |
| `6c9b3df0a` perf(config): one-shot commands compose the city config once per pr... | `519e3a959` | Carried with conflict (below). |
| `1adbee005` fix(prompts): a blocked worker escalates to GC_ESCALATION_RECIPIENT... | `6af88a779` | Carried. |
| `1bfa51181` docs(patches): the register for upstream e38ce9cc5: supervisor-API-... | register | Moved second to last; holds this entry. |
| `1a4f63c72` Re-baseline the census for upstream e38ce9cc5 | replaced | New census commit, last. |
| `016ff8b26` fix(session): report a delivered-but-unobserved submit as sent | - | **drop: upstream has it (#5977, `08ec7f0bc`)** |
| `313c82865` perf(dispatch): query live roots first in ensureDrainItemRoot | `817158b4b` | Carried with conflict (below). |
| `19a4f143b` chore(genclient): drop the oapi-codegen v2.8.0 regeneration from th... | squashed | Into `313c82865`'s new commit. |
| `744264d28` fix(nudge): sleep the poll interval after a delivered tick | `0845d4c78` | Carried; its two tests now call `markNudgePollCityRunningForTest` (upstream #6857's stopped-city exit). |
| `2c578233a` fix(nudge): the delivered backoff resets once the queue drains, and... | squashed | Into `744264d28`'s new commit. |
| `6c6fe579e` fix(retention): wisps-plane retention covers nudge and session wisp... | `6646f5a6d` | Carried. |
| `63bc69b97` fix(retention): the nudge sweep never deletes a message bead, whate... | squashed | Into `6c6fe579e`'s new commit. |
| `96f433477` perf(retention): time-bound wisps-plane retention passes and batch ... | `207234933` | Carried. |
| `a77984b8e` fix(retention): the deadline bounds the session children checks and... | squashed | Into `96f433477`'s new commit. |
| `0a9337dea` fix(backup): sync a large scope from a copy-on-write snapshot befor... | `cbd13b8d2` | Carried with conflict (below). |
| `d83408e81` fix(dolt): doctor accepts bd's `default` backup remote as backup co... | `19442343d` | Carried with conflict (below). |

**Needs discussion** (carried, but reworked or with an open question):

- `c4947f620` heavy sweeps: upstream replaced the pre-push suite with
  `.githooks/lib/push-suite.sh` (Bazel test, read-only cache or remote
  execution; `make test-fast-parallel` only in its `go` mode). The remote-run
  hand-off now lives in that `go` mode, which this Bazel-less laptop takes. If
  the hosts get Bazel and an rbe executor in `~/.bazelrc`, the patch can go.
  (Dropped 2026-10-20 with the build host; see "Dropped 2026-10-20".)
- `07d01c43e` events AfterSeq floor: rewritten onto upstream's `walkBackward`
  (#6063). Upstream PR #6374's head (`c0c4cb29e`) is the old shape and needs
  this commit force-pushed to it.
- `ad525bcf5` session pending: upstream split the city-pending handler into a
  `probeCityPending` snapshot (#7264, #7083). The shared code is now
  `session.Manager.ProbeCityPending` plus `CityPendingEntries`, and the API
  snapshot takes its rows from it, so route and command still cannot drift.
- `141dab13e` per-recipient mail retention: upstream added an opt-in,
  city-wide unread-mail phase (`[mail] unread_retention_ttl`,
  `--unread-mail-ttl`, #5257). It is kept city-wide beside our read-mail
  policy; whether `[[mail.recipient]]` should also override the unread TTL is
  open. Merge note: `UnreadRetentionTTL` auto-merged onto
  `MailRecipientRetention` and was moved back onto `MailConfig`.
- `100e3c851` readiness gateway env: upstream (#7264) now forwards
  `ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN` to `claude auth status` and
  calls API-key setups unsupported; ours short-circuits a base URL plus key or
  token to `configured`. Both kept; upstream's forwarding test runs beside ours.
- `de0c9742d` fail_halts: re-planted on the read-trimmed dispatcher (#7456,
  #7462, #7468): `opts.failHalts()` inside `terminalAbortScopeFailureMember`,
  and a halted scope body closes skipped in the single write that carries its
  propagated member metadata. Upstream PR #7174 is the place to settle it.
- `56b7ef6f6` gc bd load-once: upstream's #7430 rewrote `resolveBdScopeTarget`
  into probe candidates. Our by-ID prefix route is now `bdScopeProbe.owned`.
  One behaviour differs from upstream: `gc bd show <city-id>` from a rig cwd
  goes to the city without a probe, so an absent id is reported by the city
  store, not the rig's. Upstream's `TestResolveBdScopeTargetSkipsProbesThatCannotChangeTheAnswer`
  cases moved to a verb outside `bdSubjectIDVerbs` (`comments`), plus three
  by-ID cases.
- `6c9b3df0a` config load memo: upstream's #7601 store memo now reuses cfg
  across store opens for most of the same commands; re-measure, and consider
  one command allowlist with `invocationStoreMemoCommands`.
- `1a5119918` mise pin, now go 1.26.9 (upstream's `go.mod`): the mise
  `oapi-codegen` shim resolves only under the Go install that installed it, so
  under 1.26.9 `TestGeneratedClientInSync` fails with "No version is set for
  shim: oapi-codegen" until `make install-tools` runs under 1.26.9.
- `BUILD.bazel`: upstream gates "BUILD files in sync", and this laptop has no
  Bazel, so the entries were added by hand (srcs sorted, imports checked against
  deps; `errgroup` moved from `//internal/api` to `//internal/session` with the
  pending probe). Run `make bazel-sync` on a Bazel host before any of this goes
  upstream.

Other carried patches with conflicts, no design change: `b57ea02ac` (the
continuation nudge from #3842 kept after our root adoption; the demand skip
sits before #7054's per-template append), `d9ed79ada` (`BUILD.bazel` beside
`scope_settle.go`; `gc.source_unprojected_blockers` joins the work-bead keys of
upstream's new `TestSessionFieldUniverse`), `0a9337dea` (`sync_scope` prints
upstream's first-attempt detail, #6363, and returns the last attempt's status),
`d83408e81` (took #7381's freshness comment, kept the remote-name sentences),
`313c82865` (genclient conflict only; net zero after its revert),
`6c9b3df0a` (`compose.go`: upstream's convention-revision capture moved into
the memoized inner function). Command census: ours are 211-213, `next_id` 214
(upstream took 205-210). Census 748 / 226 against upstream's 747 / 225.

Generated artifacts needed nothing: `go run ./cmd/genspec`, `go generate
./internal/api/genclient` (oapi-codegen v2.6.0), `go run ./cmd/genschema` and
`go run ./cmd/gen-command-census -check` all leave the tree clean, and no
carried patch touches the SPA source, so upstream's `dist/` stands.

Verified on this laptop (load ~30-40), cgo with the ICU flags: no conflict
markers, gofmt clean on every changed Go file, `go build ./...`, `go vet ./...`,
the resource census, and `go test -race` on every package a carried patch
touches: `internal/events/...`, `internal/api/...`, `internal/session/...`,
`internal/worker/...`, `internal/config/...`, `internal/mail/...`,
`internal/dispatch/...`, `internal/beadmeta`, `internal/molecule`,
`internal/sling`, `internal/sourceworkflow`, `internal/formula`,
`internal/beads/...`, `internal/productmetrics`, `internal/runtime/tmux`,
`test/docsync`, `examples/bd/dolt`, and `cmd/gc` filtered to
`Retention|Wisps|DeleteBatched|Nudge|Mail|Session|Dispatch|Drain|Hook|WorkflowRoot|FailHalts|Formula|Sling|Bd|StatusReadiness|ExternalCommand|RootArgv|PackDiscovery|ConfigLoadMemo|ComposeTheCityConfigOnce|Backstop|Prompt|Escalat|ProductMetrics|ProviderFactoryCensus|NoBuiltinRefresh|Order`.
Two real regressions surfaced and were fixed in their own patches: upstream's
new `TestSessionFieldUniverse` (the start-gate key) and upstream #6857's
stopped-city exit in the nudge-poll tests. Failures that reproduce on a clean
`upstream/main` checkout on this host, so are not ours:
`internal/api` `TestHandleSessionMessageLogsLateProviderResultAfterTimeout` and
`TestStreamSessionPeekAcceptsPeekCapability` (panic in
`session_key_guard_test.go:21`), and `examples/bd/dolt`
`TestDoctorCheckVersionFloor*` and `TestRunBoundedPython3Fallback*`.
`internal/genclient`'s `TestGeneratedClientInSync` passes with oapi-codegen
v2.6.0 on PATH (see the mise note above). Not run: `make check`, the sharded
suites, `bazel test`, `make bazel-sync`, `make dashboard-build`.

### 2026-10-05: related commits squashed, #14 replaced by upstream #7082, same base `e38ce9cc5` (ga-31j)

50 commits on `e38ce9cc5` (`b92c0b12e`, `drop-api-patches-2026-10-05`) -> 28,
in `worktrees/ga-31j` on `consolidate-patches-2026-10-05`. Squashing first, with
a scripted `rebase -i`: each patch absorbed its fix-ups, its follow-ups and its
"docs(patches): row for ..." commit, and its message was rewritten to say what
it does now. The squashed tree was byte-identical to `b92c0b12e`
(`git diff b92c0b12e HEAD` empty) before any replacement. Every PATCHES.md
conflict on the way was a moved row and took the commit's own row; the
intermediate tables are not curated, and the table above is the one to trust.
The first four commits are unchanged. Not squashed: #6374, #6373, #4495, #6185
and #7082 each back a separate upstream PR, so each stays a single commit that
can be dropped alone, and a cherry-pick carries no PATCHES.md row. The census
commit stays last.

| new | absorbed (old SHAs on `b92c0b12e`) |
|---|---|
| `7c9e61c8f` gc-<name> delegation | `c14383845` + `17184be21` (its handover test) |
| `6f6a9c447` runner names failing jobs (#10) | `a8c744703`, reworded: its preflight-PID half is upstream |
| `e3872e481` control-ready probe (#13) | `157711e83`, reworded from the issue title to what it does |
| `b57ea02ac` workflow root only while it is the work | `30435192d` + `f7acf109f` + `5405cd265` + `323e61146` + `4e38b1db7` + `f73c2fd54` |
| `45260303d` formula list --rig | `023af8ec6` + `ae93f8498` |
| `8045df379` answered nudge resets the backstop | `8bac82778` + `f353fcba7` |
| `de0c9742d` fail_halts | `a6085673c` + `af489ade1` |
| `100e3c851` readiness sees a gateway env | `4c6177418` + `fd228c2c3` + `b395794a0` + `3b7d53987` |
| `d9ed79ada` blocked bead's workflow waits on one start bead | `30e4bb3cb` + `23f205dde` + `163763b9c` + `0a5a45bb0` |
| `56b7ef6f6` gc bd loads city.toml once | `2a6c617d4` + `c9d38eba2` |
| `152ddaccd` version etc. skip pack discovery | `53cafd906` + `184ddf673` |
| `1a3cfebbe` build-release | `c7bc0f94d` + `ec4633c4b` |
| `6c9b3df0a` config composed once | `2ce10c42f` + `ae58ede60` (which also held the #4495 and #6185 rows) |
| `1adbee005` escalation recipient | `eb157b18f` + `598a9e7b0` |
| register (second to last) | `90f43ec41` + `425ecf657` + `b92c0b12e` + this entry |
| census (last) | `0819ec529` |

Carried one-to-one with a new SHA: `1af0ff2d3` -> `7483c5b55`, `5a5438e38` ->
`558f2cf27`, `c3d80358f` -> `ad525bcf5`, `679e8ac2f` -> `141dab13e`,
`dfcd32923` -> `1d86898e8`, `b4e1b6783` -> `5e4c2691e`, `28deb47e2` ->
`00e2d8f57` (both cherry-picks gained a `Cherry-picked-from:` trailer). Moving
the `4b26deb7c` table (`dfcd32923`) into the restore commit, or later into the
register, conflicted on every row in between, so it stays where it was.

Then every patch was searched against gastownhall/gascity PRs, open and merged
after `e38ce9cc5`. One replacement was credible: `c562a7252` (our
`silentCloses`, #14) became `668ceb808`, a cherry-pick of `abfc769bd` from
#7082 (julianknutsen, maintainer, open, head `1349ca70a`). That commit forward-ports
#6881's close-announcement fix, a superset of ours (it also announces an
`Update` to closed and claims each announcement once); it applied cleanly to
`e38ce9cc5`, and our #13 above it auto-merged. Our own silent-close tests and
the `silentCloses` oracle and differential fields went with our commit, and
#7082's tests replace them. Nothing remains to carry. Upstream's
`TestAutocloseSweepRunsAutocloseForASilentClose` fails with it, as it already
did with ours (its fixture needs the close to stay silent); #7082's own CI shows
the same failure.

### 2026-10-05: supervisor-API-only patches dropped, same base `e38ce9cc5` (ga-jfz)

57 commits on `e38ce9cc5` (`826807e9b`) -> 50: 48 carried, the census
re-baseline and this entry. Done in `worktrees/ga-jfz` on
`drop-api-patches-2026-10-05` with a scripted `rebase -i`: the nine commits in
"Dropped 2026-10-05" dropped, #14 stopped for `edit` and split. Every SHA from
`c14383845` up is new; the first four are unchanged.

| old | new | outcome |
|---|---|---|
| `75755da9e` /runs closed-pass roots (#14) | `c562a7252` | **Split.** Kept the `internal/beads` half (`silentCloses`, `absorbReadThroughLocked`, the four reconcile test lines, `caching_store_silent_close_internal_test.go`); removed `run_root_reconcile.go` and its test, and restored `huma_handlers_runs.go` and `server.go` to the parent. Reworded to the beads half. |
| `d9732056e` session pending / readiness / stop-turn (#17) | `c3d80358f` | PATCHES.md conflict only (the #16 row). Command ids 210-212 unchanged. |
| `55682ad76` per-recipient mail retention (#18) | `679e8ac2f` | Carried with conflict, as expected. Kept #18's retention policy everywhere; removed #16's `recordMailLifecycleEvents` calls, the recorder on `newWispGCForConfig` and `cmdOrderSweepNudgeMailRun`, and #18's `TestCmdOrderSweepNudgeMailRun_RecordsMailArchivedPerMessage`, which tested #16's event. Doc comments go back to upstream's `isRemovedMessageBead` wording. **Kept from #16:** `SweepReadMessages` and `PurgeReadMessageWisps` return the swept IDs (and `nudgeMailSweepResult.MailClosedIDs`), because #18's per-recipient tests assert which messages each policy swept; upstream's count tests take `len(...)`. |
| `326820d41` config composed once (bgc-58tk) | `2ce10c42f` | Conflict in `cmd/gc/main.go`: kept `confirmConfigLoadMemoCommand`, dropped `88d7e7a71`'s `announceClientIdentity`. |
| docs(patches) commits | new SHAs | Took each commit's own PATCHES.md; this entry fixes the table. |
| `21fcfc500` bundle + census | `0819ec529` | Census re-banked at 728 / 214 (upstream 727 / 213): the drop changed neither figure. `make dashboard-build` reproduced upstream's `dist/` byte for byte, so there is no bundle commit. |

Generated artifacts needed nothing: `go run ./cmd/genspec`, `go generate
./internal/api/genclient`, `go run ./cmd/genschema` and `npm run
generate:client` all left the tree clean, because each dropped patch carried
its own generated changes out with it.

Behaviour change from dropping #16: a closed message bead is again upstream's
"removed" unless the retention sweep closed it (`isRemovedMessageBead`), and
`gc mail delete` is again an alias for archive.

### 2026-10-05: onto upstream/main `e38ce9cc5` (bgc-yfnk)

55 patches on merge-base `4b26deb7c`, 76 upstream commits (mostly the unwired v2
controller, CachingStore fences and RBE/CI). Done in `worktrees/bgc-yfnk` on
`scratch/rebase-2026-10-05`. No patch was absorbed; upstream PRs #4495 and #6185
are still open, so their cherry-picks are carried. Every patch not listed below
carried clean.

| old | new | outcome |
|---|---|---|
| `79ffa1441` /runs closed-pass roots (#14) | `75755da9e` | Carried with conflict. Upstream fenced the cache's read paths (#6920, #6928, #6964, #6979: `rowAnswersEdges`, `scanRacedLocked`, an `opts` variable). Took upstream's `caching_store_reads.go` and swapped our `absorbReadThroughLocked` in at the four silent read-path absorbs (three in `refreshCachedBeads`, the dirty `Get` read-through). Upstream's new `RefreshRow` keeps `absorbFreshLocked` on purpose: it notifies `bead.closed` itself, and the plain absorb clears the mark. Census, differential, diffutil and oracle tests keep upstream's `retainedAt`/`fenceFloor`/`retainedIDs` beside our `silentCloses`; the oracle takes upstream's `expectedWriteSeq(st)`. |
| `e6a3942f2` control-ready re-list (#13) | `66eb385a6` | **Auto-merged and did not compile.** Upstream's `PrimeActive` now captures `scanGen` and merges nothing if a full scan landed during the listing (#6979). Our shared `absorbActivePrime` takes `startScan` as a parameter, and `PrimeActiveReadiness` captures it too. Folded into the patch with a fixup. |
| `d8b0666df` session pending / readiness / stop-turn (#17) | `d9732056e` | Carried with conflict. Upstream took command ids 205-209 (`worktree-verify` through `storage-repair-sequence`), so ours are 210-212 (`next_id` 213). The manifest carries the patch's three new entries **and** its change to the existing `gc status` entry (`shape` runnable -> runnable-group, because `status readiness` makes it a group). Missing the second passes `gen-command-census -check` and fails `TestProductMetricsCommandCensusMatchesProductionBuiltins`. |
| `1b6091994` per-recipient mail retention (#18) | `55682ad76` | Carried with conflict. Upstream wrapped a wisp-gc test in `withCloseAbandonedTTL` and added nine `newWispGC(..., 0)` calls (#4690); `0` maps to `config.MailRetentionPolicy{}`, as the patch maps it. |
| `fd28d4c86` formula sling waits for blockers (bgc-acn) | `aa95fb1d0` | Carried with conflict. Upstream's `default_merge_strategy` (#4814) widened `pendingGraphWorkflowLaunch`; kept upstream's call inside our `finalize` wrapper. |
| `131ed2681` dashboard bundle + census | `21fcfc500` | Dropped as build output, regenerated at the end: `make dashboard-build`, census 728 / 214 against upstream's 727 / 213. |

**Trap seen on this rebase:** never chain `git rebase --continue || git rebase
--skip` to clear an expected-empty build-output commit. When `--continue` stops on
the NEXT patch's real conflict, `--skip` drops that patch. It dropped
`fd28d4c86` here; it was repaired by putting it back at the head of
`git-rebase-todo` and skipping the half-applied stop. Compare subjects
(`comm -3` of `git log --format=%s` on both stacks) before moving the branch.

Verified: no conflict markers, gofmt clean, `go build ./cmd/gc`,
`gen-command-census -check`, the resource census, and `go test` on
`internal/beads`, `sling`, `sourceworkflow`, `molecule`, `productmetrics`,
`api`, `api/dashboardbff`, `events`, `mail/...` and `cmd/gc` filtered to the
patched commands, wisp GC, mail and the product-metrics census. Two `cmd/gc`
Dolt port tests failed at host load ~100 and passed alone.

### 2026-10-01: onto upstream/main `4b26deb7c` (bgc-8cq)

22 patches on merge-base `ebbb019f5`, 243 upstream commits. Done on the scratch
branch `scratch/rebase-2026-10-01`; `patches/on-main` was not moved by the
rebase. No patch was absorbed whole (no empty commits from `git cherry`). Two
were absorbed in part, one dropped as build output, and none are wrong on the
new base. The intermediate dashboard `dist/` conflicts (every patch that rebuilt
the SPA) were resolved by taking upstream's bundle and then regenerating once,
at the end.

| old | new | outcome |
|---|---|---|
| `317e8996b` mise pin | `d1dd950b8` | Carried clean, then amended: upstream's `go.mod` now says `go 1.26.6`, so the pin is 1.26.6. |
| `ef0df25c8` heavy sweeps to the build host | `81b503e84` | Carried clean. |
| `268ea8430` events AfterSeq floor | `ee3ca443f` | Carried clean. |
| `5c840db79` dashboardbff run lanes on demand | `75bfd9a90` | Carried clean. |
| `b394c28c5` output-turn entry id | `fd2bbc4bc` | Carried; conflicts only in `dist/` (build output, regenerated). |
| `ee4a18af9` agent list cache keyed on time | `f22607fb1` | Carried clean. |
| `049164791` tool input on tool_use turns | `f7268f446` | Carried clean. |
| `29c3b58f1` unknown command -> `gc-<name>` | `a10abb766` | Carried clean. Upstream touched `cmd/gc/main.go` 4 times; none adds an equivalent. |
| `d817d2682` external-command handover test | `8bfc82299` | Carried clean. |
| `e789fd70a` client name on every request | `3fa6b051c` | Carried clean (still replays its committed markers; the next row removes them). |
| `d36ff8c31` docsync scratchpad | `b4f2cbd2a` | Carried clean. |
| `38efa4272` remove committed markers | `428675b23` | Carried clean. |
| `5866e0335` bead close time and attribution (#9) | `8e14c9258` | Carried; conflicts only in `dist/`. |
| `dfe5e301e` dashboard bundle + census re-baseline | dropped | Build output for the old base. Its conflicts (dist, census) took upstream's side and the commit became empty. Replaced by `131ed2681` at the end of the queue. |
| `64e99402b` restore PATCHES.md | `64131b735` | Carried clean. |
| `7689bb9dc` preflight PID false positive + runner (#10) | `1be36772f` | **Absorbed in part.** The `storage_preflight_test.go` assertion is fixed upstream by `67e786ee7` (#5995), which asserts on `"controller: PID "`; took upstream's line. The `scripts/test-local-parallel` half is carried. |
| `9a52da32a` /runs closed-pass roots (#14) | `79ffa1441` | Carried with conflict. `CachingStore.Get` read-through: kept our `absorbReadThroughLocked` with upstream's new `depsFromFieldsIfCarried` (`8c02a6208`). Oracle test: kept upstream's `writeSeq` (`9248e8c42`) and our `silentCloses`. |
| `748b0182b` control-ready re-list (#13) | `e6a3942f2` | Carried with conflict. Upstream `9a396fca2` moved the ready projection after the deps read in `PrimeActive`; our `absorbActivePrime` (shared by `PrimeActive` and `PrimeActiveReadiness`) now reads deps, then projects, in that order. `dispatch_control_ready.go`: upstream's `bindingEngine(source)` with our `PrimeActiveReadiness()`. |
| `b1b99d0b7` runs/summary reconcile (#15) | `ae5f1014f` | Carried clean. |
| `eb33c849c` closed mail status, archive, unarchive (#16) | `153dfebe3` | **Absorbed in part.** "Archive closes instead of deleting" is upstream now (`9cfb391f6`, #6306), and upstream `6537ca1fc` (#6687) makes Archive read live; both kept. Carried: `status=closed\|any`, `closed_at`, unarchive, `Archived`, lifecycle events. Conflicts with `06b47b6af` in the sweep (kept its TTL guard, added our archived-ID events), `wisp_gc.go` (upstream's session-purge cursor beside our recorder), and `beadmail_test.go` (our `TestClosedMessageBeadReadsAsArchived` replaces the eager-delete-era `TestLegacyClosedMessageBeadTreatedAsRemoved`, as the patch always did). Generated TS client regenerated. |
| `71bbeb7c5` session pending / readiness / stop-turn (#17) | `d8b0666df` | Carried with conflict. Upstream took command ids 207 and 208 (`nudge-drop`, `beads-city-migrate-proxied`), so ours are renumbered 209-211 (`next_id` 212) and the census artifacts regenerated with `go run ./cmd/gen-command-census`. |
| `a2ca7d082` per-recipient mail retention (#18) | `1b6091994` | Carried with a **semantic** conflict. Upstream `06b47b6af` (#5933) made the sweep close read mail at `retention_ttl`. Our policy replaces its two helpers; an unset `archive_read_after` now falls back to `retention_ttl` when set (`"0"` disables the archive phase), else 1h, so a city without the new key gets upstream's behaviour. Upstream's `--mail-ttl` validation kept (explicit 0 disables the phase for one run). Our fail-closed config load kept, so upstream's three `ForCity` warn-and-fall-back tests are dropped; its other #5877 tests are ported to the policy, and our `TestMailRetentionWithoutNewConfigMatchesToday`, `TestMailRetentionPolicyDefaults` and `TestMailRetentionPolicyShortestWindows` now pin the retention_ttl fallback. The commit message says the same. This city sets `archive_read_after = "1h"` and a `human` override, so its behaviour is unchanged either way. |

Two things this base needs on the hosts before the branch moves:

- **Go 1.26.6 on the-forge (moot: the host was retired 2026-10-20).**
  `mise.toml` now pins 1.26.6 and the forge's mise lives in root-owned
  `/opt/mise`, so a login shell there fails with
  `failed create_dir_all: /opt/mise/installs/go/1.26.6: Permission denied`.
  That breaks the pre-push sweep, the refinery's verify and any remote-run of
  this branch until someone with root runs `mise install go@1.26.6` there.
  `make test-fast-parallel` fails every job within a second, because each job
  is `env -i ... bash -lc`, so mise resolves go from `mise.toml` and cannot
  install it. The verification for this rebase pointed the REMOTE COPY's
  `mise.toml` back at the installed 1.26.5 and let `GOTOOLCHAIN=auto` run the
  cached go1.26.6 toolchain that `go.mod` requires; the committed pin is 1.26.6.
- **bd stays 1.3.0 on the hosts.** Upstream pins beads v1.3.1 (`8870d710c`),
  which is v1.3.1-rc.2 plus version stamps. Every runtime floor in the code is
  at or below 1.3.0 (`bdFreshProviderMinVersion = "1.3.0"`, `bdListBriefMinVersion = "1.3.0"`).
  No host-bd test was run against 1.3.0: the forge has no `bd` on PATH.

## Rebasing onto a newer upstream

    git fetch upstream
    git branch -f scratch/rebase-trial patches/on-main
    git checkout scratch/rebase-trial
    git rebase upstream/main

**Rebase onto a scratch branch first.** On 2026-09-17 a rebase of this queue
left committed conflict markers on the shared tip and pushed them, which made
`make test-fast-parallel` red for every author on the branch: `ScanRepository`
parses every Go file before it narrows to `*_test.go`, so one unparseable file
fails the whole resource census, and the census is the pre-push gate. Verify,
then move the real branch.

Two conflict classes to expect, neither of which is a real code conflict:

**The dashboard bundle.** Nearly every conflict will be in
`internal/api/dashboardspa/dist`, where Vite's content-hashed filenames collide
rename/rename because both sides rebuilt the SPA. Those are build output.
Resolve them with anything to keep the rebase moving, then regenerate properly:

    make dashboard-build

It needs node and runs locally; it does not
run the Go suite. Regenerating is NOT optional whenever a carried patch touches
the SPA source or the generated supervisor client: keeping upstream's bundle
would ship a dashboard built without them, and nothing fails to say so. Since
2026-10-05 none does, so the rebuild should reproduce upstream's `dist/` byte
for byte; if it does not, find out which patch changed the SPA.

**The census baseline.** `test/test-resources.toml`,
`internal/testpolicy/resourcecensus/census.go` and `TESTING.md` all carry the
same subprocess count and all three conflict. Take UPSTREAM's numbers, finish
the rebase, then re-measure on the rebased tree and bank the real figure:

    go test ./internal/testpolicy/resourcecensus/ -count=1

Upstream now derives the per-file counts into `test/test-resource-counts.toml`,
which is generated and never hand-merged: take upstream's, then on a host
without Bazel run

    GC_TEST_REPO_ROOT="$PWD" go test ./internal/testpolicy/resourcecensus/ -count=1 \
      -run '^TestRepositoryLedgerMatchesCensusAndDocumentation$' -update

and commit the result.

Our stack adds exactly +1 call in +1 file over upstream, and has at each base so
far (665 against 664, 670 against 669, 720 against 719 at `4b26deb7c`, 728 against 727 at `e38ce9cc5`, 748 against 747 at `8e3d40151`, 758 against 757 at `340ed8581`, and 765 against 764 at `1904f5b20`). A number
that is not upstream+1 means something else changed; find out what.

**BUILD.bazel.** Upstream gates "BUILD files in sync" (gazelle via `make
bazel-sync`). A carried patch that adds a Go file must list it in its
package's `BUILD.bazel`, in the same commit. On a host with Bazel, run `make
bazel-sync` after the rebase and fold each hunk into its patch; on this laptop
(no Bazel) add the srcs by hand, sorted, and check the new files' imports
against the rule's deps.

Then verify before moving the branch: no `<<<<<<<` anywhere
(`git grep -n '^<<<<<<< ' HEAD`), `gofmt -l -e` clean on the changed Go files,
the census green, and `go build ./cmd/gc` succeeding. Tag the old tip first
(`backup/on-main-pre-rebase-<date>`, pushed with `--no-verify`, since a tag push
otherwise runs the whole test suite for a commit already on origin), then
`git push --force-with-lease`.

A patch upstream has merged becomes an empty commit and drops out, which is the
signal to delete its row from the table above.

## A patch is not live until the supervisor restarts

`make install` writes `~/go/bin/gc` and symlinks `~/.local/bin/gc`, which is
ahead of brew on PATH, so a shell's `gc` is patched immediately. The long-lived
`gc supervisor` process keeps executing whatever binary launched it. On
2026-09-10 that process had been up 2d14h and was still `/opt/homebrew/bin/gc`,
so the liveness fix installed on 2026-09-09 had never once run.

There is a THIRD layer, and it is the one that will fool you. The supervisor runs
as a launchd service whose plist HARDCODES the binary path. Ours said:

    ProgramArguments = ["/opt/homebrew/bin/gc", "supervisor", "run"]

launchd never consults PATH, so `gc supervisor stop && gc supervisor start`
faithfully relaunches whatever the plist names. You get a genuine restart, a new
PID, and the same stock binary. Point the service at the patched build:

    cp ~/Library/LaunchAgents/com.gascity.supervisor.plist /tmp/plist.bak
    gc supervisor install --force

`gc supervisor install` resolves a STABLE path (it prefers `~/.local/bin/gc`,
then `~/go/bin/gc`, whichever matches the running binary), so the plist does not
end up pointing into a build directory. It refuses without `--force` when the
existing plist names a different binary, which is a good guard: read the refusal
before overriding it.

Check the API, not the filesystem, and not the PID:

    curl -s "http://127.0.0.1:8372/v0/city/<city>/agent/<agent>/output?tail=1"

A turn carrying an `id` field means the patched binary is serving. Verified here
2026-09-10: PID 69545 on `~/.local/bin/gc`, `id` present, and `[Bash]` carrying
its command instead of a bare label.

Installing is safe at any time; restarting drops whatever workflow is mid-flight.

## Remotes

    origin    https://github.com/hiltonc/gascity.git      fetch + push   OURS
    upstream  https://github.com/gastownhall/gascity.git  fetch only
    upstream  DISABLED_read_only                          push

We have READ only on `gastownhall/gascity`, so `upstream`'s push URL is
deliberately set to a string that cannot resolve. A stray `git push upstream`
fails immediately instead of erroring after it has done half the work.
`git fetch upstream --tags` still works, which is what the rebase below needs.

The branch is published at
<https://github.com/hiltonc/gascity/tree/patches/on-main>. Pushing it is not
ceremony: it is the staging ground for offering these patches upstream, and it
means the stack survives this laptop. The branch has already lost work to local
churn twice on the previous branch, where several SHAs the old patch table named
no longer resolve in this clone at all, so an unpushed commit here should be
treated as one that does not exist yet.

## Build and install

    export GC_SIGN_IDENTITY="Apple Development: apple@crosswaterbridge.com (HSMS46GP35)"
    make build-release   # -> bin/gc, CGO_ENABLED=0 -trimpath -s -w, signed
    make install-release # -> $GOPATH/bin/gc, NOT brew's prefix

This is the binary we roll out. It is built the way upstream's `.goreleaser.yml`
ships gc: no cgo, `-trimpath`, `-ldflags "-s -w"`. Against `make build` on the
same commit (the-beast, 2026-10-02) it is 129MB instead of 269MB, links no
Homebrew `icu4c` dylib, and `gc version` in the city costs about 0.29s CPU and
60MB RSS a call instead of 0.31s and 83MB. No ICU exports are needed to build it.

**Set `GC_SIGN_IDENTITY` explicitly.** `build-release` refuses to run on macOS
without it. Left to auto-detect, `scripts/sign-darwin-local.sh` picks a
different team on the-beast. A different team is a different designated
requirement, so every host would lose its Full Disk Access grant. `codesign -dv
bin/gc` must show `TeamIdentifier=X352NFZ594`.

**What a no-cgo gc cannot do: open an embedded-Dolt store in process.** The
beads library's no-cgo `OpenBestAvailable` refuses embedded mode. gc never
depends on it. Preflight's `dolt_mode_safe` check already sends every embedded
scope to per-call `bd`, a failed native open falls back to `bd` too, and `bd` is
its own binary. Every store gc opens on the three hosts is in server mode
(checked 2026-10-02, bgc-70k2). The one embedded store found,
`~/Developer/hilton-gas-city/iOS/.beads` on the-studio, belongs to no scope: the
iOS rig lives at `~/Developer/iOS`.

Under `-trimpath`, `go version -m` omits `-ldflags`. Read provenance from `gc
version --long` instead. `go version -m` still shows `CGO_ENABLED=0`, which tells
a release build apart from a `make build`.

`make build` and `make install` are still the cgo build, which is what `go test`
compiles by default. `install-release` runs `install` without letting it
rebuild, so everything below about `~/.local/bin/gc` applies to it too.

`make install` leaves `/opt/homebrew/bin/gc` and the Cellar alone, so
`brew upgrade gascity` is unaffected. It does NOT leave `~/.local/bin/gc` alone,
and that is where the obvious rollback goes wrong.

The install target removes whatever sits at `~/.local/bin/gc`, symlink included,
and repoints it at the GOPATH copy:

    rm -f "$(HOME)/.local/bin/$(BINARY)"
    ln -sf "$(INSTALL_DIR)/$(BINARY)" "$(HOME)/.local/bin/$(BINARY)"

On a host where `~/.local/bin/gc` was a symlink into brew, that link is the only
thing making brew reachable under that name, and installing destroys it. So the
two rollbacks that look obvious both fail:

- **Removing the GOPATH copy** leaves `~/.local/bin/gc` dangling at a deleted
  target. It does not fall back to brew.
- **Putting brew first on PATH** does nothing, because the entry doing the
  shadowing IS `~/.local/bin/gc`. On the workshop `~/.local/bin` is first on
  PATH and `/opt/homebrew/bin` is third, so reordering would mean moving
  `~/.local/bin` after brew, which is not what anyone means by that sentence.

**The rollback that works is to restore the symlink:**

    ln -sf /opt/homebrew/bin/gc ~/.local/bin/gc

**Check what `~/.local/bin/gc` IS before you install.** The one-liner above
restores a symlink into brew, which is right only if that is what was there. If
it was a real file rather than a symlink, installing deleted something the
one-liner does not bring back, and you need to reinstall it from wherever it
came from. Read the link before you overwrite it:

    ls -la ~/.local/bin/gc

Found by Jane on the-ansible, 2026-09-10, where `~/.local/bin/gc` had pointed
into brew since 2026-07-31. The workshop has the same shape after installing.
The host-shaped caveat is personal-gas-city's mayor, same day. This is the
sentence someone reaches for while something is already broken, so it is worth
being exactly right.

A bare cgo `go test` still needs the ICU flags, because the Makefile sets them
and a bare `go test` does not inherit them. Without them the test build fails on
`unicode/regex.h` from the Dolt go-icu-regex cgo dependency:

    ICU=$(brew --prefix icu4c)
    export CGO_CPPFLAGS="-I$ICU/include" CGO_LDFLAGS="-L$ICU/lib"

Or test what we ship, which needs no ICU: `CGO_ENABLED=0 go test ./cmd/gc
./internal/beads/...`.

## Verifying a patch before installing

    go test ./internal/api/ -run 'AgentSession|ResolveAgentRuntime' -count=1

Do not swap the binary while workflows are in flight. The supervisor and every
running worker are executing the binary being replaced.
