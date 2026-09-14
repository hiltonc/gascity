# Local patch queue

This branch is upstream Gas City at a release tag with a small set of patches on
top. Most are upstream cherry-picks that are meant to disappear when upstream
merges them. Two are OURS: new work that will not disappear by itself and that
we should offer upstream (see the table).

It lives on a fork, because we have READ only on `gastownhall/gascity`.

## Branch shape

    patches/on-v<version>     branched from tag v<version>, patches on top, newest last

One branch per upstream release. `patches/on-v1.4.1` is branched from `v1.4.1`. The name carries the BASE TAG, not
what the build calls itself, because the rebase recipe below is read straight off it.

## Patches on patches/on-v1.4.1

| Commit | Upstream | What it fixes |
| --- | --- | --- |
| local | none | `mise.toml` pinning go 1.26.5, which `go.mod` requires and upstream does not pin. Build-environment only, no product change. |
| `25ace43` | none yet, OURS | `OutputTurn` carried only `{role, text, timestamp}`. The agent output endpoints accept `before`/`after` entry-ID cursors and report `has_older_messages`, so a client could see that older messages exist and have nothing to send as a cursor; an invented ID answers 500. Adds the entry ID to every turn, on the paged read, the live stream, and the history path. |
| `21f91a8` | none yet, OURS | A `tool_use` block rendered as its bare name, so a transcript read as `[Bash]` then a result: answers to questions that are never shown. Over one agent's 733 turns, 225 tool labels against 225 results. The input was already parsed and only the name serialized; appends the field a human reads first, flattened to one line and bounded at 500 like `tool_result` beside it. |
| `404de06` | none yet, OURS | `gc` had no extension point: a subcommand it does not implement could only be a typo. Adds the git-style convention, `gc foo` running `gc-foo` on PATH, resolved before Cobra parses so the extension's own flags and `--help` survive. Built-ins and pack commands still win the name, so a future upstream `gc foo` takes it back rather than being masked. The Unix handover replaces the process image, which is what makes argv, the environment, the working directory, unbuffered streaming, Ctrl-C and exit status (signal death included) correct without forwarding code. |
| `8fd253b`, `fb4b514`, `db8fd34`, `1526a72`, `8b9f79e`, `389e868` | none yet, OURS | `GET /v0/city/{city}/agents` forked tmux once per declared agent per request (about 130 on this city, most of them failing show-environment reads for agents that are not running), coalesced nothing, and keyed its response cache on the event index, so on a loaded host it took 15 to 55 seconds and six copies ran at once. Reads the suspended flag only for running sessions, keys the list on the /status time bucket with a singleflight per key, reads an entry back through an age floor as well as its exact bucket, and folds attach state and window activity into the tmux state cache's refresh (one `list-windows -a`) behind a new optional `runtime.SessionSnapshotProvider` that only the agent handlers use. The age floor is what makes the cache able to hit at all: a build that outlives the bucket it started in — which is every build on a loaded host — is unreadable without it. Carry all six commits together; `8fd253b` alone is the version with the cache that cannot hit, an unproven snapshot path, dead forwarding, a shared fetch budget, and an aliasing hazard. The last three are review repairs, and each one closes a defect that comes straight back if a rebase stops before it: `1526a72` removes the two fixed sleeps the cache tests waited on, `8b9f79e` renames a test that claimed coverage it did not have, and `389e868` adds the `FetchState` test that fails when the window fold is deleted — without it, nothing in the tree fails when the feature this patch exists to add is removed. Re-check this list on every push to the branch. Measured on high-gas-city 2026-09-12. |
| `7d53d19` | none yet, OURS | `GET /v0/city/{city}/events` took 40s to 2m34s per call and was issued continuously by two core-pack orders, burning two to three supervisor cores. The handler probes the active `events.jsonl` with a backward walk; a selective `--type` filter never fills the probe, so it walked the whole log `json.Unmarshal`-ing every line, and the handler then discarded that work and paid a second full pass in the archive-aware fallback. Bounds both legs on invariants already relied on elsewhere: the walk stops at `Filter.AfterSeq` (the active log is strictly seq-ordered, which `activeScanStart` already depends on), and the probe carries an 8 MiB `MaxScanBytes` budget, which cannot change an answer because a probe short of `limit+1` rows was already going to fall through. `Filter.Since` deliberately does NOT stop the walk: `writeRecordLocked` preserves a caller-supplied `Ts`, so timestamps are not monotonic. A read that still needs the archive now logs what it cost, and every CLI request carries `User-Agent: gc/<version> (<subcommand>)`, surfaced as `client="..."` on the api log line, so a hot endpoint's caller is identifiable at all — only the resolved cobra path, never flag values or positionals. This is a re-land, not a transplant: the original sits on `origin/main` (`4e7e9f71d`, `99563ee61`), 844 commits ahead, and cherry-picking conflicts in three files. `Filter.MaxScanBytes` is an upstream field (#4418) absent from this branch and re-landed here with its mid-chunk clamp; this branch has a `Filter.BeforeSeq` the origin/main version lacked, and its `NewRemoteEventsClient` sets no SSE `Accept` header. Measured on a 58,308,326-byte live log (161,503 events, none matching the caller's filter): probe 1.482s/whole file to 255ms/8 MiB; `after_seq` read 1.959s/whole file to <1ms/64 KiB; a tail read that fills its page unchanged at 4ms. The probe leg is now O(budget), not O(log size); the fallback still runs for that caller, so this makes the doomed leg cheap rather than removing it. Measured on high-gas-city 2026-09-13. |

The last five rows are OURS, not upstream cherry-picks, which makes them a
different kind of patch from everything above them. They will NOT turn into
empty commits on a rebase and drop out by themselves. Offer them upstream (each
is small and self-contained); until one is merged, expect to carry it and to
resolve real conflicts rather than watching it disappear. The reasoning and the
measurements behind the two output-turn patches are in the town as `hgc-di92ml`.

The two output-turn patches regenerate `internal/api/openapi.json`, the
`docs/reference/schema` mirrors, and `internal/api/genclient/client_gen.go`,
because `OutputTurn` is `additionalProperties: false` and Huma derives the
schema from the Go struct. A hand-edited schema would make the response
invalid against its own spec. Run:

    make install-oapi-codegen
    go run ./cmd/genspec
    PATH="$(go env GOPATH)/bin:$PATH" go generate ./internal/api/genclient

The external-command patch touches no schema and regenerates nothing. It is the
one row here whose commit is worth keeping clear of this file: PATCHES.md is the
fork's own register and has no place in what gets offered upstream, so the patch
and the row that records it are separate commits, as `25ace43` and `21f91a8`
already are.

## Removed: the agent-liveness fix (PR #4721, issue #4703)

Carried from 2026-09-09 and dropped 2026-09-10, deliberately, not because it was
wrong. `GET /v0/city/{city}/agents` still under-reports liveness: on this town,
0 of 14 agents reported running while four sessions were active.

**How badly it lies is host-shaped, so do not generalize from one town.**
personal-gas-city on the-ansible measured its own roster against sessions and
found its three named agents joining correctly while only pool slots
(`claude-1`) read as stopped, and concluded the gap was confined to pool slots.
That is not what this town shows. Here `High/core.control-dispatcher` and
`GasCityDispatch/core.control-dispatcher` are named agents, not pool members,
each with a live session, and the roster still calls both stopped. This town's
`mayor` is missing from the roster entirely, which is a third defect again.

So the useful summary is: named agents join on some hosts and not others, pool
slots seem to fail everywhere, and an agent can be absent rather than merely
misreported. Measure your own roster against `/sessions` before deciding the
patch is or is not worth carrying.

We dropped it because our reason for carrying it went away. Dispatch does not
read the roster's liveness at all: `Core/GasCityRepository/Sources/AgentRoster.swift`
derives liveness by joining the sessions list itself, and says so in a comment at
the top of the file. That workaround shipped in PR #31 and was extended to
Overview in PR #34, which fixed a real `0/14 Agents` on a busy city. So the
server-side fix would change nothing the app shows.

What still pays the cost: the built-in dashboard SPA, which reads `/agents` in
`App.tsx`, `CityBootstrap.tsx` and `attention/registry.ts`, so it shows an idle
city and its attention logic sees nothing to attend to. That was already true —
the patch had never actually run in the supervisor (see below) — so nothing
regressed by removing it.

To bring it back: `git cherry-pick d5c4a1898`, which is the tip of the local
`pr-4721` branch. (An earlier `backup/on-v1.4.1-with-liveness` held the same
change as `ecf762c76`; `pr-4721` is the durable copy, so the backup is
disposable.)

## `gc version` on this branch lies, and says 1.4.2

A build of this branch self-reports **1.4.2**, which is not a release: upstream's
latest tag is v1.4.1 and there is no v1.4.2 anywhere. The binary is honest; the
version string is not.

The chain, worth knowing before anyone treats it as evidence of a bad build:

1. The Makefile stamps the version from `git describe --tags --exact-match`,
   which fails on this branch (we are five commits past the tag), so it falls
   back to `-X main.version=dev`. Correct so far.
2. `cmd/gc/cmd_version.go` then treats `dev` as "unknown" and falls back to Go's
   build info: `info.Main.Version`.
3. Go stamps the main module with a **pseudo-version**, which by convention names
   the NEXT patch after the last tag:
   `v1.4.2-0.20260910144616-1a99c077eb81`.
4. `normalizeVersion` strips the pseudo-version suffix with
   `^(.*)-0\.\d{14}-[0-9a-f]{12,}$`, leaving a bare `1.4.2`.

So a pseudo-version meaning "somewhere after v1.4.1" is rewritten into a claim to
be a release that does not exist. `git describe --tags` says the honest thing:
`v1.4.1-5-g38b02e173`.

Verify provenance from the build info rather than the version string:

    go version -m ~/go/bin/gc | grep -E 'mod|vcs.revision|vcs.modified'

`vcs.revision` is the real commit and `vcs.modified=false` means a clean tree.
This is an upstream bug in `normalizeVersion` and worth reporting: stripping the
suffix is right for a real tagged build and wrong for a pseudo-version, which
should keep enough of itself to stay distinguishable from a release.

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
<https://github.com/hiltonc/gascity/tree/patches/on-v1.4.1>. Pushing it is not
ceremony: it is the staging ground for offering the two OURS patches upstream,
and it means the stack survives this laptop.

## Rebasing onto a new release

    git fetch upstream --tags
    git checkout -b patches/on-v1.4.2 patches/on-v1.4.1
    git rebase --onto v1.4.2 v1.4.1 patches/on-v1.4.2
    git push -u origin patches/on-v1.4.2

Tags come from `upstream`; the branch goes to `origin`. Both halves of the
`--onto` are read straight off the branch names, which is why the name carries
the base tag rather than what the build calls itself.

A patch upstream has merged becomes an empty commit and drops out, which is the
signal to delete its row from the table above. Resolve anything else by hand,
then rebuild and re-run the patch's own tests before installing.

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
