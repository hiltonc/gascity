# Local patch queue

Our fixes to gascity, carried on top of upstream and rebased forward as upstream
moves. This file is the fork's own register: it is NOT offered upstream, so a
patch and the row recording it stay separate commits.

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

Every commit here is OURS. None will turn into an empty commit and drop out on a
rebase by itself; expect to carry each one and to resolve real conflicts until
upstream takes it. As of 2026-10-05, rebased onto upstream/main `e38ce9cc5`
(the per-patch outcome of that rebase is under "Rebase log" below):

| commit | what |
|---|---|
| `1a5119918` | `mise.toml` pins go 1.26.6, which `go.mod` requires and upstream does not. Build environment only. |
| `c4947f620` | Sends the heavy sweeps to the build host instead of this laptop. |
| `07d01c43e` | `perf(events)`: stops a backward tail walk at the AfterSeq floor. |
| `a9a9780c0` | `perf(dashboardbff)`: projects run lanes on demand rather than on a timer. |
| `b9ca20854` | `fix(api)`: an output turn carries the entry id its cursors need. |
| `6d298285b` | `perf(api)`: keys the agent list cache on time, not the event sequence. |
| `696d9d108` | `feat(api)`: shows what a tool was asked to do, not just its name. |
| `341684860` | `feat(cli)`: delegates an unknown command to `gc-<name>` on PATH. |
| `3ea811fe2` | `test(cli)`: owns the external-command process handover. |
| `88d7e7a71` | `feat(api)`: names the client on every request, and reports a full-history scan. |
| `3ab717dc9` | `test(docsync)`: lets the gitignored `scratchpad/` hold notes without failing the gate (ga-e2nr). |
| `74264ce4d` | `fix(api)`: resolves a committed conflict in `client_remote.go` that broke every repository scan. See the warning below. |
| `2d91b46fa` | The bead API serves a close time and attribution (ga-88ni, PR #9). |
| `4fadad90c` | `fix(test)`: `scripts/test-local-parallel` names the failing jobs when a sharded run fails (PR #10). Its preflight-PID test fix is upstream now (67e786ee7); only the runner half is carried. |
| `75755da9e` | `fix(runs)`: the beads cache announces a close a read path absorbed silently, and `/runs` confirms a stale non-terminal workflow root against its store, so a root whose close never reached the event log stops reading active (ga-bilu). |
| `87552c1d7` | `fix(runs)`: the dashboard's `runs/summary` and run census apply the same per-city run-root reconcile `/runs` uses (handed to the plane by `WithRunCensusSource`), so a root the store reports closed leaves `lanes` and the counts agree (ga-vw6t). |
| `66eb385a6` | `perf(dispatch)`: the control-dispatcher follower skips its control-ready re-list while the scope's Dolt database hash is unchanged (ga-dtay). `perf(dispatch)`: the control-ready re-prime reads one brief issue-tier list (`ListQuery.Brief`, `CachingStore.PrimeActiveReadiness`) and shares one `bd version` probe across control stores (`BdVersionMemo`). Touches upstream-owned `internal/beads` (`query.go`, `bdstore.go`, `bdstore_ready_projection.go`, `caching_store.go`, `caching_store_reads.go`) and `cmd/gc/bd_env.go`, all additive (ga-dtay). |
| `499356883` | `feat(mail)`: closed mail is served under `GET /mail?status=closed\|any` with `status`/`closed_at` on every mail response; archive closes, delete deletes, `POST /mail/{id}/unarchive` reopens, and close, archive, unarchive, sweep and purge emit `mail.archived` / `mail.unarchived` / `mail.deleted`. Touches upstream-owned `internal/mail` (`mail.go`, `beadmail`, `exec`, `fake.go`, `mailtest`), `internal/beads/memstore.go`, `internal/api` (mail handlers and types, event payloads, routes, regenerated OpenAPI and clients), `internal/events/events.go` and `cmd/gc` (mail lifecycle events, nudge-mail sweep, wisp GC) (ga-85n6, PR #16). |
| `d9732056e` | `feat(cli)`: `gc session pending`, `gc status readiness` and `gc session stop-turn <session>`, each with `--json` emitting its route's body, so dispatch can run `gc` on the host instead of calling the supervisor API. Each calls the function its route does: `session.Manager.CityPending` (extracted from the pending handler), `api.ProbeReadiness` (shared by both readiness routes) and the stop route / `Manager.StopTurn`. Touches upstream-owned `internal/session`, `internal/worker`, `internal/api` (pending and readiness handlers, plus `NewCityScopedClientWithHTTPClient` so client tests use an in-process transport) and `cmd/gc` (ga-6jam). |
| `55682ad76` | `feat(mail)`: read-mail retention is configurable per recipient. `[mail] archive_read_after` (default `"1h"`, `"0"` = never) replaces the watchdog's hard-coded hour, and `[[mail.recipient]]` entries (exact address or `path.Match` glob, first match wins) override it and `retention_ttl`. `config.MailRetentionPolicy` answers both windows per address for the archive sweep, its dry-run count and the wisp-GC purge; `gc order sweep-nudge-mail --mail-ttl` replaces the policy only when given. With `archive_read_after` unset the archive window is `retention_ttl` when set, else 1h, which is upstream's sweep behaviour since 06b47b6af; with no new config the behaviour is upstream's. Touches upstream-owned `internal/config` (`MailConfig`, load validation), `internal/mail/beadmail` (`SweepReadMessages`, `CountReadMessages`, `PurgeReadMessageWisps`) and `cmd/gc` (nudge-mail sweep and watchdog, `sweep-nudge-mail`, wisp GC), plus regenerated reference docs (ga-w27v3). |
| `01d09320b` | `fix(hook,controller)`: a running workflow root is neither claimed nor counted as demand. `gc hook --claim` skips a routed root while one of its steps is in progress under a session, and a session that claims a released step adopts that step's open, unassigned root (compare-and-swap). The controller's default scale check stops counting such a root as pool demand. A failed step read fails OPEN in both places. New `cmd/gc/workflow_root_held.go`; touches upstream-owned `cmd/gc/cmd_hook_claim.go`, `claim_class_route.go` and `build_desired_state.go`. Makes the city's `orders/root-adoption-sweep` (bgc-lck) unnecessary (ga-tf857, bgc-9yh). |
| `ba9084dcb` | `feat(cli)`: `gc formula list --rig <rig>` lists that rig's scope (city layers beneath the rig's own, `FormulaLayers.SearchPaths(rig)`), the paths `gc formula show --rig` compiles against and `GET /formulas?scope_kind=rig` serves. It took the global `--rig` and ignored it. Without `--rig` the list is unchanged (every layer); an unbound `--rig` errors. Touches upstream-owned `cmd/gc/cmd_formula.go` (bgc-cya). |
| `7642df452` | `fix(controller)`: an answered nudge restarts the execution-claim backstop's window. Runtime activity later than the last nudge (or the observe marker, before any nudge) resets `execution_claim_nudge_count` to 0 and restarts the grace clock, logged as "showed activity since its last nudge"; a session that never answers still drains after 3, a no-nudge template still drains straight after grace unless it worked after observation, and an escalation latch is not unwound. Optional `activityRenewingBackstop` hook in upstream-owned `cmd/gc/nudge_backstop.go`, implemented in `execution_backstop.go`. Makes the city's `orders/claim-nudge-reset` unnecessary (bgc-u92). |
| `02c53fe58` | `feat(dispatch,hook,config)`: `[workflows] fail_halts` (off by default). With it on, a needs edge on a step whose terminal `gc.outcome` is fail halts the dependent: it closes skipped with `gc.halted_by` naming the failed step, at claim time in `gc hook --claim` and in control processing, cascading. Finalizers (workflow-finalize, scope-check, `gc.scope_role=teardown`) are never halted. A workflow root cannot close pass while any step's terminal outcome is fail (a retried step's is its last attempt's); it closes fail and names the step on the root and the source bead. Off, behaviour is unchanged. New files `internal/dispatch/fail_halts.go`, `cmd/gc/hook_claim_fail_halts.go`, `internal/config/workflows.go`; touches upstream-owned `internal/dispatch/runtime.go` (ProcessControl, scope body close, workflow finalize) and `cmd/gc/cmd_hook_claim.go`. City bead bgc-b9e (from bgc-0mh, evidence gcd-pozhh7: four fail steps, root closed pass). |
| `3ec0ecd69` | `fix(api)`: the claude readiness probe counts a gateway env as authenticated. When the probe's environment has `ANTHROPIC_BASE_URL` plus `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` (the teamclaude proxy shape), `claude` reports `configured` ("authenticated through ANTHROPIC_BASE_URL") instead of running `claude auth status`, which sees only the scrubbed probe env and reported `needs_auth` on every proxy-routed host. A base URL without a key, or a key without a base URL, still falls through to the login check. On its own it reads the PROCESS env only; `ebe699931` (below) adds the city's `[providers.claude.env]`. `internal/api/handler_provider_readiness.go` (`probeClaude`); the claude probe tests clear the gateway vars so they stay hermetic inside an agent session (bgc-a5t). |
| `ebe699931` | `fix(api)`: readiness probes see the city's provider env. Each provider's probe takes that provider's resolved, launch-expanded `[providers.<name>.env]` from the city config; its keys override the process env for that provider only (claude's gateway check, mimocode's `XIAOMI_API_KEY`, zcode's `ZCODE_*`) and are appended to `claude auth status`'s scrubbed env. The probe cache key hashes the env. `gc status readiness` loads the enclosing city best-effort and the city routes pass their config, so readiness over SSH sees a gateway configured only in city.toml; the supervisor routes have no single city and keep the process env. New `api.ProbeCityReadiness`; touches upstream-owned `internal/api/handler_provider_readiness.go`, `huma_handlers_city.go`, `huma_handlers_supervisor.go` and `cmd/gc/cmd_status_readiness.go` (bgc-15s, off bgc-a5t). |
| `aa95fb1d0` | `fix(sling)`: a formula slung onto a blocked bead waits for its blockers. The graph.v2 bead-sling paths (`--on`, default formula, convoy batch) and `gc formula cook --attach` read the source bead's unsatisfied ready-blocking deps (`sourceworkflow.ReadSourceBlockers`; satisfied ones and workflow roots dropped) and pass them as the new `molecule.Options.Gates`. A blocker the workflow's store cannot resolve is recorded on the root as `gc.source_unprojected_blockers` and reported. `gc sling` and its `--dry-run` name the blockers. Its per-step gate edges are replaced by `0f9523539` (below). Touches upstream-owned `internal/molecule` (`Options.Gates`), `internal/sling` (`sling_core.go`), `internal/sourceworkflow`, `internal/beadmeta` and `cmd/gc/cmd_formula.go`, all additive (bgc-acn). |
| `0f9523539` | `fix(sling)`: a blocked bead's workflow waits on ONE start bead (rework of `aa95fb1d0`, Hilton). `molecule.GateRecipe` adds `<root>.start-gate`, a control bead of the new kind `start-gate` titled "Wait for <blockers> (blockers of <source>)" that carries the gates as its own deps; the root and the entry steps (no needs) block on it, later steps wait through their needs. Applied before graph routing (sling materialize, cook `--attach`) so it routes to the control dispatcher; `dispatch.processStartGate` closes it pass once its deps are satisfied, so nothing is Ready, no pool demand, no session held. Graph workflows only (no parent-child to the root). A late blocker is `bd dep add <start> <blocker>`, printed by `gc sling` and documented in Tutorial 06. `start-gate` joins `ControlKinds`, `ScopeCheckExemptKinds` and `EngineMintedOnlyKinds`. Touches upstream-owned `internal/beadmeta/{values,kindsets}.go`, `internal/dispatch/runtime.go` (ProcessControl case), `internal/formula/types.go`, `internal/molecule/molecule.go` and `internal/sling/sling.go`; new `internal/molecule/start_gate.go`, `internal/dispatch/start_gate.go` (bgc-acn). |
| `a631d53e7` | `fix(hook,controller)`: a workflow root with nothing ready for the claimant is not a launch. Extends `01d09320b`: `gc hook --claim` and the controller's default scale check also skip a routed root that has live steps, none of them ready work routed to the claimant (open, unassigned, not deferred, off every dispatch hold, not blocked by another live step of the root). This closes the gap between steps, where a drain-acked worker's released root was re-claimed by the next session on its route: 266 claims across 63 Dispatch roots on 2026-10-01, Apple and portable alike. A root with no live steps (root-only molecule) is still a launch, and so is a fresh root whose first step is ready here. An unrouted step and a blocker outside the root count as ready; a failed step read fails OPEN. `workflowRootSkipReason` replaces `workflowRootHeldStep` in `cmd/gc/workflow_root_held.go`; touches upstream-owned `cmd/gc/cmd_hook_claim.go` and `build_desired_state.go` (bgc-jt6h). |
| `5598d102d` | `perf(bd)`: `gc bd` composes city.toml once per invocation, where `gc bd --rig X show` composed it six times and an auto-detected one up to ten. `resolveBdCity` resolves with a new `cityOnly` context mode (skips `rigFromCwdDir`'s rig decoration, two full loads); `doBd` loads with the no-refresh loader (full loader as fallback) and passes its cfg to `resolveBdBinaryForScopeWithConfig` and the new `bdOneShotRuntimeEnv` / `bdOneShotRuntimeEnvForRig`, whose city projection reads the workspace bd pin and the hosted binding from it (old signatures still read the file, for long-lived callers), and offers it to the CLI storage-routes memo, which takes it only for a city with no `[storage]` section. A positional subject of show/update/close/reopen/delete/heartbeat whose prefix one scope carries routes there without opening the store; flag values, other verbs' positionals, unknown flags and shared prefixes keep the probe. The passthrough's exec now also writes the `GC_BD_TRACE` line (`beads.TraceBDPassthrough`). `cityConfigLoads` counts compositions; `TestDoBdLoadsCityConfigOnce` pins one. Instructions on the-beast: `gc bd --rig show` 1724M -> 725M, auto-detected 2308M -> 718M, against `gc version` 618M. Touches upstream-owned `cmd/gc/cmd_bd.go`, `bd_env.go`, `cli_storage_routes.go`, `cmd_agent.go`, `main.go` and `internal/beads/bdstore.go` (bgc-vpn7). |
| `f455ef66e` | `perf(cli)`: `version`, `completion`, `login`, `logout` and `whoami` skip pack discovery (`rootCommandSkipsPackDiscovery`), so `gc version` inside a city costs what it does outside one (0.29s -> 0.055s CPU, median of 21). None reads city config and packs cannot shadow a core command; the completion scripts are byte-identical in and out of a city. `help`, bare `gc`, `--help`/`-h` and `__complete` keep discovery, because the root usage and dynamic completion list pack bindings. gc has no `--version` flag. Touches upstream-owned `cmd/gc/root_argv.go` (one `case` line and its comment) and `root_argv_test.go`, whose ambient-args test now uses `status` as its ordinary command (bgc-zfi5). |
| `63c146776` | `build`: `make build-release` builds gc as upstream's `.goreleaser.yml` ships it (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`) and signs it, refusing on macOS without `GC_SIGN_IDENTITY`; `make install-release` installs that binary through `install` without rebuilding it. Two new targets in upstream-owned `Makefile`, nothing else touched. See "Build and install" (bgc-70k2). |
| `1590ea5f0` | Upstream PR #4495 (open), cherry-picked: `perf(cli)`: the registry rig-binding scan in context resolution loads without the builtin-pack refresh (sys-s3pd). Drop on the rebase that brings #4495 in from upstream (bgc-58tk). |
| `b78ee83d8` | Upstream PR #6185 (open), cherry-picked: `perf(nudge)`: the per-prompt `gc nudge drain` reuses the config `resolveNudgeTarget` loaded and the target store across its store opens (sys-2b9jfa). Drop on the rebase that brings #6185 in from upstream (bgc-58tk). |
| `326820d41` | `perf(config)`: one-shot commands compose the city config once per process, where `mail check` composed it 30 times, `mail inbox` 25, `sling` 21, `session list` 13, `hook` 12, `nudge drain` 7 and `events` 6 on the-beast. `internal/config/load_memo.go` memoizes `LoadWithIncludesOptions` when a process calls `EnableLoadMemo`; every caller gets a deep copy (reflective, proved complete by `TestLoadMemoCloneCoversConfigTypeGraph`); an entry is served while every file the composition read through its fs, plus `.gc/site.toml`, holds the same bytes; the key leaves out `SkipRevisionSnapshot` and `RepoCacheNonBlocking`, which cannot change a successful composition. `run()` turns it on only for `mail inbox`, `mail check`, `nudge drain`, `hook`, bare `events` (not `--follow`/`--watch`), `session list` and `sling`, confirmed by `root.Find` before the command runs, and off when the invocation returns; never the supervisor or any long-lived command. Pack-cache repairs invalidate it. `TestHotCommandsComposeTheCityConfigOnce` pins 1 composition per command and identical output. CPU on the-beast, median of 5: `mail check` 1.29s -> 0.77s, `sling` 1.44s -> 0.89s, `mail inbox` 1.19s -> 0.81s, `session list` 1.13s -> 0.96s. Touches upstream-owned `internal/config/compose.go` (wrapper), `internal/config/pack.go` (one `case`), `cmd/gc/main.go` (two lines), `cmd/gc/embed_builtin_packs.go` and `cmd/gc/legacy_pack_preflight.go` (bgc-58tk). |
| `2ee7410a7` | `fix(hook)`: a workflow root the session holds is served only while it is the work. While a session holds a root its work query serves the root's ready steps and otherwise the root itself, never fresh pool demand, so the existing-assignment path handed it back on every claim: bgc-wisp-82lkwea got Dispatch's `gcd-40pr2a` from 11:55Z until it closed at 15:06Z on 2026-10-04. Now the hook reads the root's live steps first: none (root-only molecule) or an unreadable read serves the root as before; a ready step routed here is claimed in its place and the root kept; otherwise, or when that step is lost, the root is released (release-if-current, `bead.claim_released` reason `workflow_root_not_work`) and the session claims normally. **How it got the root: a launch claim, not adoption (ga-tf857).** 82lkwea claimed it before the run-operator took `prepare-worktree`, while no step was held, because `a631d53e7`'s readiness check counted the root's step-spec sidecar (`gcd-rg41pd`, `gc.kind=spec`, unrouted, open from launch to finalize) as ready work for every route. Spec sidecars and scope latches no longer count, in the hook and in the controller's demand count alike. Touches upstream-owned `cmd/gc/cmd_hook_claim.go` and `cmd/gc/workflow_root_held.go` (bgc-3q3y). |
| `c113750db` | `fix(prompts)`: a blocked worker escalates to `GC_ESCALATION_RECIPIENT`, not a hard-coded human. The core `pool-worker.template.md` and `graph-worker.md` prompts said `gc mail send human`, so a GasCityDispatch implementation worker's BLOCKED mail (`gcd-40pr2a`, 2026-10-04) reached the operator's inbox instead of alice, who triages. Both now send to `"${GC_ESCALATION_RECIPIENT:-human}"` (a shell default; the Go template passes it through untouched), so a city with no recipient set behaves as before. Every host already exports the variable to its sessions (alice on the-beast, jane on the-ansible, locke on the-studio). `TestCoreWorkerPromptsEscalateToConfiguredRecipient` renders both prompts and pins the form. **Left alone on purpose:** the bundled gascity-packs `gc mail send human` lines (superpowers design/spec approval, github-issue-fix/-triage/pr-review human gates), which ask a human to DECIDE; `cmd_mail.go` help and test fixtures; the core formulas' `escalation_target` var, which defaults to `human` under its own tested contract (`TestCoreEscalationTargetDefaultsToHuman`) and is set per sling through `formula_vars`; and `orphan-sweep.sh` / `spawn-storm-detect.sh`, which read `GC_ESCALATION_TARGET` under `TestCoreEscalationScriptContract`. Touches upstream-owned prompt assets only (bgc-2e06). |
| `21fcfc500` | Regenerates the embedded dashboard bundle and re-baselines the resource census (+1 call in +1 file over upstream) for upstream `e38ce9cc5`. Build output only; redo it on every rebase (see below). |

**`e789fd70a` carries committed conflict markers, and `38efa4272` removes them
again.** That is not tidy and it is deliberate for now: the markers are inside
the patch as committed, so every rebase replays them and then repairs them. The
net tree is correct and the gate is green. Squashing the pair would be cleaner
but rewrites a patch we may offer upstream, so it is left explicit rather than
hidden. If you ever see markers in a working tree mid-rebase, this is why; check
the FINAL tree, not an intermediate one.

## Rebase log

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
run the Go suite. Regenerating is NOT optional. Our patches touch the SPA source (`SessionPeek.tsx`
and the generated supervisor client), so keeping upstream's bundle ships a
dashboard built without them, and nothing fails to say so.

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
