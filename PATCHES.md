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
**1.4.3**. That is upstream's number, not ours, and it moves when we rebase. It
is not evidence about which binary is running: verify provenance from the build
info rather than the version string, and see the supervisor section below for
the three places a stale binary hides.

## Patches on patches/on-main

Every commit here is ours except the three marked "Upstream PR ... cherry-picked"
(#7082, #4495, #6185); each of those drops when its PR merges. None will turn
into an empty commit and drop out on a rebase by itself; expect to carry each
one and to resolve real conflicts until upstream takes it. As of 2026-10-05,
rebased onto upstream/main `e38ce9cc5`, with the supervisor-API-only patches
dropped and related commits squashed the same day (the per-patch
outcome of each rewrite is under "Rebase log" below; what was dropped, and
where to restore it from, is under "Dropped 2026-10-05"):

| commit | what |
|---|---|
| `1a5119918` | `mise.toml` pins go 1.26.6, which `go.mod` requires and upstream does not. Build environment only. |
| `c4947f620` | Sends the heavy sweeps to the build host instead of this laptop. |
| `07d01c43e` | `perf(events)`: stops a backward tail walk at the AfterSeq floor. Ours, upstream PR #6374 (open); this commit matches its head `30e72bb91` (no drift). Drop when #6374 merges. |
| `a9a9780c0` | `perf(dashboardbff)`: projects run lanes on demand rather than on a timer. Ours, upstream PR #6373 (open); this commit matches its head `1d33d10a8` (no drift). Drop when #6373 merges. |
| `7c9e61c8f` | `feat(cli)`: delegates an unknown command to `gc-<name>` on PATH (Unix: process-image handover; Windows: a supervising fallback). `external_command_unix_test.go` owns the handover: the extension keeps the pid gc was launched as, and a signalled process group yields a death-by-signal exit status. |
| `7483c5b55` | `test(docsync)`: lets the gitignored `scratchpad/` hold notes without failing the gate (ga-e2nr). |
| `558f2cf27` | This register, restored onto `patches/on-main` on 2026-09-18. Docs only. |
| `6f6a9c447` | `fix(test)`: `scripts/test-local-parallel` names the failing jobs when a sharded run fails (PR #10). Its preflight-PID test fix is upstream now (67e786ee7); only the runner half is carried. |
| `668ceb808` | Upstream PR #7082 (julianknutsen, open, head `1349ca70a`), its commit `abfc769bd` only, cherry-picked: `fix(beads)`: the cache emits `bead.closed` exactly once for every close it observes. A close that a live list, dirty-row read or event patch installs over a cached open row is queued (`unannouncedCloses`) and drained as `bead.closed` (`ChangeRefresh`) on the next notification, read, event apply or reconcile pass; an `Update` that leaves a bead closed announces `bead.closed`, not `bead.updated`; writers claim the announcement under `c.mu`, so a close is announced once. Replaces our `silentCloses` patch (ga-bilu, PR #14), whose cases it covers; the core order cascade-nudge-on-blocker-close fires on those events. Upstream's `TestAutocloseSweepRunsAutocloseForASilentClose` (`cmd/gc`) fails with it, as it did with ours: its fixture needs the close to stay silent, and #7082's CI fails the same way. Drop when #7082 merges. |
| `e3872e481` | `perf(dispatch)`: the control-dispatcher follower skips its control-ready re-list while the scope's Dolt database hash is unchanged (ga-dtay). `perf(dispatch)`: the control-ready re-prime reads one brief issue-tier list (`ListQuery.Brief`, `CachingStore.PrimeActiveReadiness`) and shares one `bd version` probe across control stores (`BdVersionMemo`). Touches upstream-owned `internal/beads` (`query.go`, `bdstore.go`, `bdstore_ready_projection.go`, `caching_store.go`, `caching_store_reads.go`) and `cmd/gc/bd_env.go`, all additive (ga-dtay). |
| `ad525bcf5` | `feat(cli)`: `gc session pending`, `gc status readiness` and `gc session stop-turn <session>`, each with `--json` emitting its route's body, so dispatch can run `gc` on the host instead of calling the supervisor API. Each calls the function its route does: `session.Manager.CityPending` (extracted from the pending handler), `api.ProbeReadiness` (shared by both readiness routes) and the stop route / `Manager.StopTurn`. Touches upstream-owned `internal/session`, `internal/worker`, `internal/api` (pending and readiness handlers, plus `NewCityScopedClientWithHTTPClient` so client tests use an in-process transport) and `cmd/gc` (ga-6jam). |
| `141dab13e` | `feat(mail)`: read-mail retention is configurable per recipient. `[mail] archive_read_after` (default `"1h"`, `"0"` = never) replaces the watchdog's hard-coded hour, and `[[mail.recipient]]` entries (exact address or `path.Match` glob, first match wins) override it and `retention_ttl`. `config.MailRetentionPolicy` answers both windows per address for the archive sweep, its dry-run count and the wisp-GC purge; `gc order sweep-nudge-mail --mail-ttl` replaces the policy only when given. With `archive_read_after` unset the archive window is `retention_ttl` when set, else 1h, which is upstream's sweep behaviour since 06b47b6af; with no new config the behaviour is upstream's. Touches upstream-owned `internal/config` (`MailConfig`, load validation), `internal/mail/beadmail` (`SweepReadMessages`, `CountReadMessages`, `PurgeReadMessageWisps`) and `cmd/gc` (nudge-mail sweep and watchdog, `sweep-nudge-mail`, wisp GC), plus regenerated reference docs (ga-w27v3). |
| `1d86898e8` | Register: table and rebase log for the move onto upstream `4b26deb7c`. Docs only. |
| `b57ea02ac` | `fix(hook,controller)`: a workflow root is claimed and counted as demand only while it is the work. `gc hook --claim` skips a routed root while one of its steps is in progress under a session, or when it has live steps and none is ready work routed to the claimant (open, unassigned, not deferred, off every dispatch hold, not blocked by another live step of the root); spec sidecars and scope latches never count as ready. A root with no live steps (root-only molecule) is still a launch, and so is a fresh root whose first step is ready here; a failed step read fails OPEN. A session that claims a released step adopts that step's open, unassigned root (compare-and-swap). Before serving a held root as `existing_assignment`, the hook reads its live steps: a ready step routed here is claimed in its place, otherwise the root is released (`bead.claim_released` reason `workflow_root_not_work`). The controller's default scale check asks the same question (`demandServableForTemplates`). New `cmd/gc/workflow_root_held.go`; touches upstream-owned `cmd/gc/cmd_hook_claim.go`, `claim_class_route.go` and `build_desired_state.go`. Makes the city's `orders/root-adoption-sweep` unnecessary (ga-tf857, bgc-9yh, bgc-jt6h, bgc-3q3y). |
| `45260303d` | `feat(cli)`: `gc formula list --rig <rig>` lists that rig's scope (city layers beneath the rig's own, `FormulaLayers.SearchPaths(rig)`), the paths `gc formula show --rig` compiles against and `GET /formulas?scope_kind=rig` serves. It took the global `--rig` and ignored it. Without `--rig` the list is unchanged (every layer); an unbound `--rig` errors. Touches upstream-owned `cmd/gc/cmd_formula.go` (bgc-cya). |
| `8045df379` | `fix(controller)`: an answered nudge restarts the execution-claim backstop's window. Runtime activity later than the last nudge (or the observe marker, before any nudge) resets `execution_claim_nudge_count` to 0 and restarts the grace clock, logged as "showed activity since its last nudge"; a session that never answers still drains after 3, a no-nudge template still drains straight after grace unless it worked after observation, and an escalation latch is not unwound. Optional `activityRenewingBackstop` hook in upstream-owned `cmd/gc/nudge_backstop.go`, implemented in `execution_backstop.go`. Makes the city's `orders/claim-nudge-reset` unnecessary (bgc-u92). |
| `de0c9742d` | `feat(dispatch,hook,config)`: `[workflows] fail_halts` (off by default). With it on, a needs edge on a step whose terminal `gc.outcome` is fail halts the dependent: it closes skipped with `gc.halted_by` naming the failed step, at claim time in `gc hook --claim` and in control processing, cascading. Finalizers (workflow-finalize, scope-check, `gc.scope_role=teardown`) are never halted. A workflow root cannot close pass while any step's terminal outcome is fail (a retried step's is its last attempt's); it closes fail and names the step on the root and the source bead. Off, behaviour is unchanged. New files `internal/dispatch/fail_halts.go`, `cmd/gc/hook_claim_fail_halts.go`, `internal/config/workflows.go`; touches upstream-owned `internal/dispatch/runtime.go` (ProcessControl, scope body close, workflow finalize) and `cmd/gc/cmd_hook_claim.go`. City bead bgc-b9e (from bgc-0mh, evidence gcd-pozhh7: four fail steps, root closed pass). |
| `100e3c851` | `fix(api)`: readiness probes see a gateway env, from the process or the city's provider env. When the probe's environment has `ANTHROPIC_BASE_URL` plus `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` (the teamclaude proxy shape), `claude` reports `configured` ("authenticated through ANTHROPIC_BASE_URL") instead of running `claude auth status`, which sees only the scrubbed probe env; a base URL without a key, or a key without a base URL, still falls through to the login check. Each provider's probe also takes that provider's resolved, launch-expanded `[providers.<name>.env]` from the city config; its keys override the process env for that provider only (claude's gateway check, mimocode's `XIAOMI_API_KEY`, zcode's `ZCODE_*`) and are appended to `claude auth status`'s scrubbed env. The probe cache key hashes the env. `gc status readiness` loads the enclosing city best-effort and the city routes pass their config; the supervisor routes have no single city and keep the process env. New `api.ProbeCityReadiness`; touches upstream-owned `internal/api/handler_provider_readiness.go`, `huma_handlers_city.go`, `huma_handlers_supervisor.go` and `cmd/gc/cmd_status_readiness.go` (bgc-a5t, bgc-15s). |
| `d9ed79ada` | `fix(sling)`: a formula slung onto a blocked bead waits on ONE start bead. The graph.v2 bead-sling paths (`--on`, default formula, convoy batch) and `gc formula cook --attach` read the source bead's unsatisfied ready-blocking deps (`sourceworkflow.ReadSourceBlockers`; satisfied ones and workflow roots dropped) and pass them as `molecule.Options.Gates`; a blocker the workflow's store cannot resolve is recorded on the root as `gc.source_unprojected_blockers` and reported. `molecule.GateRecipe` adds `<root>.start-gate`, a control bead of the new kind `start-gate` titled "Wait for <blockers> (blockers of <source>)" that carries the gates as its own deps; the root and the entry steps (no needs) block on it, later steps wait through their needs. Applied before graph routing (sling materialize, cook `--attach`) so it routes to the control dispatcher; `dispatch.processStartGate` closes it pass once its deps are satisfied, so nothing is Ready, no pool demand, no session held. Graph workflows only. A late blocker is `bd dep add <start> <blocker>`, printed by `gc sling` (and its `--dry-run`, which names the blockers) and documented in Tutorial 06. `start-gate` joins `ControlKinds`, `ScopeCheckExemptKinds` and `EngineMintedOnlyKinds`. Touches upstream-owned `internal/beadmeta`, `internal/dispatch/runtime.go` (ProcessControl case), `internal/formula/types.go`, `internal/molecule/molecule.go`, `internal/sling` and `cmd/gc/cmd_formula.go`; new `internal/sourceworkflow/blockers.go`, `internal/molecule/start_gate.go`, `internal/dispatch/start_gate.go` (bgc-acn). |
| `56b7ef6f6` | `perf(bd)`: `gc bd` composes city.toml once per invocation, where `gc bd --rig X show` composed it six times and an auto-detected one up to ten. `resolveBdCity` resolves with a new `cityOnly` context mode (skips `rigFromCwdDir`'s rig decoration, two full loads); `doBd` loads with the no-refresh loader (full loader as fallback) and passes its cfg to `resolveBdBinaryForScopeWithConfig` and the new `bdOneShotRuntimeEnv` / `bdOneShotRuntimeEnvForRig`, whose city projection reads the workspace bd pin and the hosted binding from it (old signatures still read the file, for long-lived callers), and offers it to the CLI storage-routes memo, which takes it only for a city with no `[storage]` section. A positional subject of show/update/close/reopen/delete/heartbeat whose prefix one scope carries routes there without opening the store; flag values, other verbs' positionals, unknown flags and shared prefixes keep the probe. The passthrough's exec now also writes the `GC_BD_TRACE` line (`beads.TraceBDPassthrough`). `cityConfigLoads` counts compositions; `TestDoBdLoadsCityConfigOnce` pins one. Instructions on the-beast: `gc bd --rig show` 1724M -> 725M, auto-detected 2308M -> 718M, against `gc version` 618M. Touches upstream-owned `cmd/gc/cmd_bd.go`, `bd_env.go`, `cli_storage_routes.go`, `cmd_agent.go`, `main.go` and `internal/beads/bdstore.go` (bgc-vpn7). |
| `152ddaccd` | `perf(cli)`: `version`, `completion`, `login`, `logout` and `whoami` skip pack discovery (`rootCommandSkipsPackDiscovery`), so `gc version` inside a city costs what it does outside one (0.29s -> 0.055s CPU, median of 21). None reads city config and packs cannot shadow a core command; the completion scripts are byte-identical in and out of a city. `help`, bare `gc`, `--help`/`-h` and `__complete` keep discovery, because the root usage and dynamic completion list pack bindings. gc has no `--version` flag. Touches upstream-owned `cmd/gc/root_argv.go` (one `case` line and its comment) and `root_argv_test.go`, whose ambient-args test now uses `status` as its ordinary command (bgc-zfi5). |
| `1a3cfebbe` | `build`: `make build-release` builds gc as upstream's `.goreleaser.yml` ships it (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`) and signs it, refusing on macOS without `GC_SIGN_IDENTITY`; `make install-release` installs that binary through `install` without rebuilding it. Two new targets in upstream-owned `Makefile`, nothing else touched. See "Build and install" (bgc-70k2). |
| `5e4c2691e` | Upstream PR #4495 (atbrace, open), cherry-picked: `perf(cli)`: the registry rig-binding scan in context resolution loads without the builtin-pack refresh (sys-s3pd). This commit matches the PR head `8a17093de` (no drift). Drop when #4495 merges (bgc-58tk). |
| `00e2d8f57` | Upstream PR #6185 (atbrace, open), cherry-picked: `perf(nudge)`: the per-prompt `gc nudge drain` reuses the config `resolveNudgeTarget` loaded and the target store across its store opens (sys-2b9jfa). This commit matches the PR head `03b94edd9` (no drift). Drop when #6185 merges (bgc-58tk). |
| `6c9b3df0a` | `perf(config)`: one-shot commands compose the city config once per process, where `mail check` composed it 30 times, `mail inbox` 25, `sling` 21, `session list` 13, `hook` 12, `nudge drain` 7 and `events` 6 on the-beast. `internal/config/load_memo.go` memoizes `LoadWithIncludesOptions` when a process calls `EnableLoadMemo`; every caller gets a deep copy (reflective, proved complete by `TestLoadMemoCloneCoversConfigTypeGraph`); an entry is served while every file the composition read through its fs, plus `.gc/site.toml`, holds the same bytes; the key leaves out `SkipRevisionSnapshot` and `RepoCacheNonBlocking`, which cannot change a successful composition. `run()` turns it on only for `mail inbox`, `mail check`, `nudge drain`, `hook`, bare `events` (not `--follow`/`--watch`), `session list` and `sling`, confirmed by `root.Find` before the command runs, and off when the invocation returns; never the supervisor or any long-lived command. Pack-cache repairs invalidate it. `TestHotCommandsComposeTheCityConfigOnce` pins 1 composition per command and identical output. CPU on the-beast, median of 5: `mail check` 1.29s -> 0.77s, `sling` 1.44s -> 0.89s, `mail inbox` 1.19s -> 0.81s, `session list` 1.13s -> 0.96s. Touches upstream-owned `internal/config/compose.go` (wrapper), `internal/config/pack.go` (one `case`), `cmd/gc/main.go` (two lines), `cmd/gc/embed_builtin_packs.go` and `cmd/gc/legacy_pack_preflight.go` (bgc-58tk). |
| `1adbee005` | `fix(prompts)`: a blocked worker escalates to `GC_ESCALATION_RECIPIENT`, not a hard-coded human. The core `pool-worker.template.md` and `graph-worker.md` prompts said `gc mail send human`, so a GasCityDispatch implementation worker's BLOCKED mail (`gcd-40pr2a`, 2026-10-04) reached the operator's inbox instead of alice, who triages. Both now send to `"${GC_ESCALATION_RECIPIENT:-human}"` (a shell default; the Go template passes it through untouched), so a city with no recipient set behaves as before. Every host already exports the variable to its sessions (alice on the-beast, jane on the-ansible, locke on the-studio). `TestCoreWorkerPromptsEscalateToConfiguredRecipient` renders both prompts and pins the form. **Left alone on purpose:** the bundled gascity-packs `gc mail send human` lines (superpowers design/spec approval, github-issue-fix/-triage/pr-review human gates), which ask a human to DECIDE; `cmd_mail.go` help and test fixtures; the core formulas' `escalation_target` var, which defaults to `human` under its own tested contract (`TestCoreEscalationTargetDefaultsToHuman`) and is set per sling through `formula_vars`; and `orphan-sweep.sh` / `spawn-storm-detect.sh`, which read `GC_ESCALATION_TARGET` under `TestCoreEscalationScriptContract`. Touches upstream-owned prompt assets only (bgc-2e06). |
| (ga-yhxss) | `fix(session)`: `gc session submit` (the API's `session.send`) reports a delivered-but-unobserved message as sent, as `gc nudge` does. `Manager.Submit` treats `runtime.ErrNudgeDeliveredUnobserved` (submit Enter delivered, composer drained, busy state never observed) as delivered and logs it, so the sender does not see "Not Sent" and resend a message the session already has; `ErrNudgeSubmitUnconfirmed` is still a failure. `tmux.ErrNudgeSubmitDeliveredUnobserved` is now that runtime sentinel, so `internal/session` can match it without importing tmux. Touches upstream-owned `internal/runtime/runtime.go`, `internal/runtime/tmux/tmux.go` and `internal/session/submit.go`, plus the runtime-tmux test manifest (ga-yhxss, ga-8sua). Fill in the commit when the queue is rewritten. |
| (register) | This register's table and rebase log for upstream `e38ce9cc5`, the second-to-last commit. Docs only. |
| (census, last) | Re-baselines the resource census (+1 call in +1 file over upstream) for upstream `e38ce9cc5`. No dashboard bundle commit: no carried patch touches the SPA, so upstream's `dist/` is current. Redo both on every rebase (see below). |

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

- **Go 1.26.6 on the-forge.** `mise.toml` now pins 1.26.6 and the forge's mise
  lives in root-owned `/opt/mise`, so a login shell there fails with
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

It needs node, which the-forge does not have, so it runs locally; it does not
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

Our stack adds exactly +1 call in +1 file over upstream, and has at each base so
far (665 against 664, 670 against 669, 720 against 719 at `4b26deb7c`, and 728 against 727 at `e38ce9cc5`). A number
that is not upstream+1 means something else changed; find out what.

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
