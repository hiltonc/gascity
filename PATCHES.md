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
upstream takes it. As of 2026-10-01, rebased onto upstream/main `4b26deb7c`
(the per-patch outcome of that rebase is under "Rebase log" below):

| commit | what |
|---|---|
| `d1dd950b8` | `mise.toml` pins go 1.26.6, which `go.mod` requires and upstream does not. Build environment only. |
| `81b503e84` | Sends the heavy sweeps to the build host instead of this laptop. |
| `ee3ca443f` | `perf(events)`: stops a backward tail walk at the AfterSeq floor. |
| `75bfd9a90` | `perf(dashboardbff)`: projects run lanes on demand rather than on a timer. |
| `fd2bbc4bc` | `fix(api)`: an output turn carries the entry id its cursors need. |
| `f22607fb1` | `perf(api)`: keys the agent list cache on time, not the event sequence. |
| `f7268f446` | `feat(api)`: shows what a tool was asked to do, not just its name. |
| `a10abb766` | `feat(cli)`: delegates an unknown command to `gc-<name>` on PATH. |
| `8bfc82299` | `test(cli)`: owns the external-command process handover. |
| `3fa6b051c` | `feat(api)`: names the client on every request, and reports a full-history scan. |
| `b4f2cbd2a` | `test(docsync)`: lets the gitignored `scratchpad/` hold notes without failing the gate (gsc-e2nr). |
| `428675b23` | `fix(api)`: resolves a committed conflict in `client_remote.go` that broke every repository scan. See the warning below. |
| `8e14c9258` | The bead API serves a close time and attribution (gsc-88ni, PR #9). |
| `1be36772f` | `fix(test)`: `scripts/test-local-parallel` names the failing jobs when a sharded run fails (PR #10). Its preflight-PID test fix is upstream now (67e786ee7); only the runner half is carried. |
| `79ffa1441` | `fix(runs)`: the beads cache announces a close a read path absorbed silently, and `/runs` confirms a stale non-terminal workflow root against its store, so a root whose close never reached the event log stops reading active (gsc-bilu). |
| `ae5f1014f` | `fix(runs)`: the dashboard's `runs/summary` and run census apply the same per-city run-root reconcile `/runs` uses (handed to the plane by `WithRunCensusSource`), so a root the store reports closed leaves `lanes` and the counts agree (gsc-vw6t). |
| `e6a3942f2` | `perf(dispatch)`: the control-dispatcher follower skips its control-ready re-list while the scope's Dolt database hash is unchanged (gsc-dtay). `perf(dispatch)`: the control-ready re-prime reads one brief issue-tier list (`ListQuery.Brief`, `CachingStore.PrimeActiveReadiness`) and shares one `bd version` probe across control stores (`BdVersionMemo`). Touches upstream-owned `internal/beads` (`query.go`, `bdstore.go`, `bdstore_ready_projection.go`, `caching_store.go`, `caching_store_reads.go`) and `cmd/gc/bd_env.go`, all additive (gsc-dtay). |
| `153dfebe3` | `feat(mail)`: closed mail is served under `GET /mail?status=closed\|any` with `status`/`closed_at` on every mail response; archive closes, delete deletes, `POST /mail/{id}/unarchive` reopens, and close, archive, unarchive, sweep and purge emit `mail.archived` / `mail.unarchived` / `mail.deleted`. Touches upstream-owned `internal/mail` (`mail.go`, `beadmail`, `exec`, `fake.go`, `mailtest`), `internal/beads/memstore.go`, `internal/api` (mail handlers and types, event payloads, routes, regenerated OpenAPI and clients), `internal/events/events.go` and `cmd/gc` (mail lifecycle events, nudge-mail sweep, wisp GC) (gsc-85n6, PR #16). |
| `d8b0666df` | `feat(cli)`: `gc session pending`, `gc status readiness` and `gc session stop-turn <session>`, each with `--json` emitting its route's body, so dispatch can run `gc` on the host instead of calling the supervisor API. Each calls the function its route does: `session.Manager.CityPending` (extracted from the pending handler), `api.ProbeReadiness` (shared by both readiness routes) and the stop route / `Manager.StopTurn`. Touches upstream-owned `internal/session`, `internal/worker`, `internal/api` (pending and readiness handlers, plus `NewCityScopedClientWithHTTPClient` so client tests use an in-process transport) and `cmd/gc` (gsc-6jam). |
| `1b6091994` | `feat(mail)`: read-mail retention is configurable per recipient. `[mail] archive_read_after` (default `"1h"`, `"0"` = never) replaces the watchdog's hard-coded hour, and `[[mail.recipient]]` entries (exact address or `path.Match` glob, first match wins) override it and `retention_ttl`. `config.MailRetentionPolicy` answers both windows per address for the archive sweep, its dry-run count and the wisp-GC purge; `gc order sweep-nudge-mail --mail-ttl` replaces the policy only when given. With `archive_read_after` unset the archive window is `retention_ttl` when set, else 1h, which is upstream's sweep behaviour since 06b47b6af; with no new config the behaviour is upstream's. Touches upstream-owned `internal/config` (`MailConfig`, load validation), `internal/mail/beadmail` (`SweepReadMessages`, `CountReadMessages`, `PurgeReadMessageWisps`) and `cmd/gc` (nudge-mail sweep and watchdog, `sweep-nudge-mail`, wisp GC), plus regenerated reference docs (gsc-w27v3). |
| `4df2f23b2` | `fix(hook,controller)`: a running workflow root is neither claimed nor counted as demand. `gc hook --claim` skips a routed root while one of its steps is in progress under a session, and a session that claims a released step adopts that step's open, unassigned root (compare-and-swap). The controller's default scale check stops counting such a root as pool demand. A failed step read fails OPEN in both places. New `cmd/gc/workflow_root_held.go`; touches upstream-owned `cmd/gc/cmd_hook_claim.go`, `claim_class_route.go` and `build_desired_state.go`. Makes the city's `orders/root-adoption-sweep` (bgc-lck) unnecessary (gsc-tf857, bgc-9yh). |
| `6dd0c3e12` | `feat(cli)`: `gc formula list --rig <rig>` lists that rig's scope (city layers beneath the rig's own, `FormulaLayers.SearchPaths(rig)`), the paths `gc formula show --rig` compiles against and `GET /formulas?scope_kind=rig` serves. It took the global `--rig` and ignored it. Without `--rig` the list is unchanged (every layer); an unbound `--rig` errors. Touches upstream-owned `cmd/gc/cmd_formula.go` (bgc-cya). |
| `de796c2f0` | `fix(controller)`: an answered nudge restarts the execution-claim backstop's window. Runtime activity later than the last nudge (or the observe marker, before any nudge) resets `execution_claim_nudge_count` to 0 and restarts the grace clock, logged as "showed activity since its last nudge"; a session that never answers still drains after 3, a no-nudge template still drains straight after grace unless it worked after observation, and an escalation latch is not unwound. Optional `activityRenewingBackstop` hook in upstream-owned `cmd/gc/nudge_backstop.go`, implemented in `execution_backstop.go`. Makes the city's `orders/claim-nudge-reset` unnecessary (bgc-u92). |
| `131ed2681` | Regenerates the embedded dashboard bundle and re-baselines the resource census (+1 call in +1 file over upstream) for upstream `4b26deb7c`. Build output only; redo it on every rebase (see below). |

**`e789fd70a` carries committed conflict markers, and `38efa4272` removes them
again.** That is not tidy and it is deliberate for now: the markers are inside
the patch as committed, so every rebase replays them and then repairs them. The
net tree is correct and the gate is green. Squashing the pair would be cleaner
but rewrites a patch we may offer upstream, so it is left explicit rather than
hidden. If you ever see markers in a working tree mid-rebase, this is why; check
the FINAL tree, not an intermediate one.

## Rebase log

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
far (665 against 664, 670 against 669, and 720 against 719 at `4b26deb7c`). A number
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

    ICU=$(brew --prefix icu4c)
    export CGO_CPPFLAGS="-I$ICU/include" CGO_LDFLAGS="-L$ICU/lib"
    make build           # -> bin/gc, signed for macOS
    make install         # -> $GOPATH/bin/gc, NOT brew's prefix

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

The ICU flags matter for `go test` and not for `make build`, because the
Makefile already sets them and a bare `go test` does not inherit them. Without
them the test build fails on `unicode/regex.h` from the Dolt go-icu-regex cgo
dependency.

## Verifying a patch before installing

    go test ./internal/api/ -run 'AgentSession|ResolveAgentRuntime' -count=1

Do not swap the binary while workflows are in flight. The supervisor and every
running worker are executing the binary being replaced.
