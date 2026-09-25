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
upstream takes it. As of 2026-09-18, rebased onto upstream/main:

| commit | what |
|---|---|
| `317e8996b` | `mise.toml` pins go 1.26.5, which `go.mod` requires and upstream does not. Build environment only. |
| `ef0df25c8` | Sends the heavy sweeps to the build host instead of this laptop. |
| `268ea8430` | `perf(events)`: stops a backward tail walk at the AfterSeq floor. |
| `5c840db79` | `perf(dashboardbff)`: projects run lanes on demand rather than on a timer. |
| `b394c28c5` | `fix(api)`: an output turn carries the entry id its cursors need. |
| `ee4a18af9` | `perf(api)`: keys the agent list cache on time, not the event sequence. |
| `049164791` | `feat(api)`: shows what a tool was asked to do, not just its name. |
| `29c3b58f1` | `feat(cli)`: delegates an unknown command to `gc-<name>` on PATH. |
| `d817d2682` | `test(cli)`: owns the external-command process handover. |
| `e789fd70a` | `feat(api)`: names the client on every request, and reports a full-history scan. |
| `d36ff8c31` | `test(docsync)`: lets the gitignored `scratchpad/` hold notes without failing the gate (gsc-e2nr). |
| `38efa4272` | `fix(api)`: resolves a committed conflict in `client_remote.go` that broke every repository scan. See the warning below. |
| `5866e0335` | The bead API serves a close time and attribution (gsc-88ni, PR #9). |
| `dfe5e301e` | Rebuilds the dashboard bundle and re-baselines the census for the new upstream. |
| `e171f2a3e` | `fix(runs)`: the beads cache announces a close a read path absorbed silently, and `/runs` confirms a stale non-terminal workflow root against its store, so a root whose close never reached the event log stops reading active (gsc-bilu). |
| `0ccec177c` | `fix(runs)`: the dashboard's `runs/summary` and run census apply the same per-city run-root reconcile `/runs` uses (handed to the plane by `WithRunCensusSource`), so a root the store reports closed leaves `lanes` and the counts agree (gsc-vw6t). |
| `3d32f6ce4` | `perf(dispatch)`: the control-dispatcher follower skips its control-ready re-list while the scope's Dolt database hash is unchanged (gsc-dtay). |
| `11ef202a3` | `perf(dispatch)`: the control-ready re-prime reads one brief issue-tier list (`ListQuery.Brief`, `CachingStore.PrimeActiveReadiness`) and shares one `bd version` probe across control stores (`BdVersionMemo`). Touches upstream-owned `internal/beads` (`query.go`, `bdstore.go`, `bdstore_ready_projection.go`, `caching_store.go`, `caching_store_reads.go`) and `cmd/gc/bd_env.go`, all additive (gsc-dtay). |

**`e789fd70a` carries committed conflict markers, and `38efa4272` removes them
again.** That is not tidy and it is deliberate for now: the markers are inside
the patch as committed, so every rebase replays them and then repairs them. The
net tree is correct and the gate is green. Squashing the pair would be cleaner
but rewrites a patch we may offer upstream, so it is left explicit rather than
hidden. If you ever see markers in a working tree mid-rebase, this is why; check
the FINAL tree, not an intermediate one.

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

Regenerating is NOT optional. Our patches touch the SPA source (`SessionPeek.tsx`
and the generated supervisor client), so keeping upstream's bundle ships a
dashboard built without them, and nothing fails to say so.

**The census baseline.** `test/test-resources.toml`,
`internal/testpolicy/resourcecensus/census.go` and `TESTING.md` all carry the
same subprocess count and all three conflict. Take UPSTREAM's numbers, finish
the rebase, then re-measure on the rebased tree and bank the real figure:

    go test ./internal/testpolicy/resourcecensus/ -count=1

Our stack adds exactly +1 call in +1 file over upstream, and has at each base so
far (665 against 664 on the old base, 670 against 669 on this one). A number
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
