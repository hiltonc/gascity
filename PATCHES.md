# Local patch queue

This branch is upstream Gas City at a release tag with a small set of patches on
top. Every code patch it carries today is OURS: new work that will not disappear
by itself and that we should offer upstream (see the table). There are no
upstream cherry-picks left waiting to become empty commits.

It lives on a fork, because we have READ only on `gastownhall/gascity`.

## Branch shape

    patches/on-v<version>     branched from tag v<version>, patches on top, newest last

One branch per upstream release. `patches/on-v1.4.1` is branched from `v1.4.1`. The name carries the BASE TAG, not
what the build calls itself, because the rebase recipe below is read straight off it.

## Patches on patches/on-v1.4.1

| Commit | Upstream | What it fixes |
| --- | --- | --- |
| `8cba0c2be` | none | `mise.toml` pinning go 1.26.5, which `go.mod` requires and upstream does not pin. Build-environment only, no product change. |
| `25ace43` | none yet, OURS | `OutputTurn` carried only `{role, text, timestamp}`. The agent output endpoints accept `before`/`after` entry-ID cursors and report `has_older_messages`, so a client could see that older messages exist and have nothing to send as a cursor; an invented ID answers 500. Adds the entry ID to every turn, on the paged read, the live stream, and the history path. |
| `21f91a8` | none yet, OURS | A `tool_use` block rendered as its bare name, so a transcript read as `[Bash]` then a result: answers to questions that are never shown. Over one agent's 733 turns, 225 tool labels against 225 results. The input was already parsed and only the name serialized; appends the field a human reads first, flattened to one line and bounded at 500 like `tool_result` beside it. |
| `404de06` | none yet, OURS | `gc` had no extension point: a subcommand it does not implement could only be a typo. Adds the git-style convention, `gc foo` running `gc-foo` on PATH, resolved before Cobra parses so the extension's own flags and `--help` survive. Built-ins and pack commands still win the name, so a future upstream `gc foo` takes it back rather than being masked. The Unix handover replaces the process image, which is what makes argv, the environment, the working directory, unbuffered streaming, Ctrl-C and exit status (signal death included) correct without forwarding code. |
| `a05e4c71b` | none yet, OURS | `GET /v0/city/{city}/agents` forked tmux once per declared agent per request (about 130 on this city, most of them failing show-environment reads for agents that are not running), coalesced nothing, and keyed its response cache on the event index, so on a loaded host it took 15 to 55 seconds and six copies ran at once. Reads the suspended flag only for running sessions, keys the list on the /status time bucket with a singleflight per key, reads an entry back through an age floor as well as its exact bucket, and folds attach state and window activity into the tmux state cache's refresh (one `list-windows -a`) behind a new optional `runtime.SessionSnapshotProvider` that only the agent handlers use. The age floor is what makes the cache able to hit at all: a build that outlives the bucket it started in — which is every build on a loaded host — is unreadable without it. Landed as one squashed commit through PR #2, which is what removes the partial-carry hazard: the six-commit series it was squashed from ended in three review repairs, and stopping a rebase before them reinstated a cache that cannot hit, an unproven snapshot path, dead forwarding, a shared fetch budget, and an aliasing hazard. `TestFetchStateFoldsTheWindowListingOntoTheSessionsItFound` is in the tree, so the repairs are inside the squash. Of the six SHAs this table used to name, five (`8fd253b`, `fb4b514`, `db8fd34`, `1526a72`, `8b9f79e`) survive only on `origin/wip/gsc-r8s-repair-preserve`, and `389e868` does not resolve in this clone at all. Measured on high-gas-city 2026-09-12. |
| `94c52b19e` | superseded upstream in part, OURS | A backward walk of the active `events.jsonl` had no lower bound, so `GET /v0/city/{city}/events` with a selective `--type` filter read the whole log and `json.Unmarshal`'d every line to discover the page it could not fill. `belowFilterFloor` stops the walk at `Filter.AfterSeq`, which is a floor the forward path already relies on: `FileRecorder` assigns `e.Seq = r.seq++` inside one mutex-and-flock section, so the active log is strictly seq-ordered, and `activeScanStart` already skips the log's head on that basis. `Filter.Since` deliberately does NOT stop the walk, because `writeRecordLocked` preserves a caller-supplied `Ts` and timestamps are therefore not monotonic. `readFilteredTailFromFile` becomes `readFilteredTailFrom` over an `io.ReaderAt` so a test can count the bytes the walk actually reads, which is what distinguishes an early stop from a full traversal — that rename is ours and is NOT in upstream, which still takes an `*os.File`. Measured on a 58,308,326-byte live log (161,503 events): an `after_seq`-shaped read goes from 1.959s over the whole file to <1ms over 64 KiB, and a tail read that fills its page is unchanged at 4ms. |
| `8dca11199` | none yet, OURS | The api request log carried method, path, status and duration but nothing about who asked, so an endpoint being hammered by short-lived `gc` processes had no identifiable caller. Every CLI request now carries `User-Agent: gc/<version> (<subcommand>)`, surfaced as `client="..."` on the api log line — only the resolved cobra command path, never flag values or positionals, which routinely carry paths, bead ids and message text. The identity is process-wide because one `gc` invocation runs exactly one subcommand; `gc events` builds its own generated client rather than reusing `api.Client`, so it opts into the shared editor explicitly, without which the hot caller of the event list would have stayed anonymous. The event-list fallback gunzips and decodes every retained archive and did so silently; it now logs the query shape that asked for it and what the read cost, with `since=` measured from the instant the read began so a slow scan does not inflate the window it reports. Both the User-Agent components and the logged value go through `sanitizeAuditString`, so neither an argv-derived subcommand nor a hostile User-Agent can forge a header or a second log line. |
| `d26f496b` | none yet, OURS | The per-city dashboard run tailer rebuilt the WHOLE run projection on every poll second that carried a bead event, whether or not anything was reading it. On high-gas-city that made `cityRunTailer.build` the hottest gascity frame in a 10s sample of a supervisor sitting at 258% CPU for four hours, while no browser was attached to the dashboard port at all. Splits the tail's two halves by cost. Folding stays on the poll — a stat, a delta read, an in-memory `Apply` — because keeping the cursor current is what keeps the deferred build incremental instead of a cold replay. Projecting (filter every folded bead, rebuild every lane, advance the marks) now runs only on demand: a live detail-stream subscriber IS demand and keeps pushing exactly as before, and with no subscriber the loop records the debt and every warm reader (summary, detail, census, projection snapshot) collects it with one build that concurrent readers collapse onto. Builds are bounded by reader demand and are zero with no readers, where they were bounded only by the event rate. Measured here on a 300-run city with nothing attached, one bead event every 40ms for four seconds: 97 whole-city projections and 908ms of process CPU before, 0 and 51ms after. The one observable consequence is that lane progress marks advance once per published projection rather than once per folded second, so derived thrash detection samples at the polling client's cadence — a sustained thrash still trips, a burst that resolves between polls is no longer seen. Re-implemented against this base rather than cherry-picked, because the base predates over a hundred commits of upstream drift in `internal/api`. The earlier attempt it was re-implemented from, `10f722c` on `gsc-fbe-runtailer-on-demand`, resolves nowhere in this clone; `d26f496b` and its docs row are the only surviving copy, on this branch and on `origin/gsc-lnc-runtailer-on-demand`, merged here through PR #5. Touches no schema and regenerates nothing. |


Every row here is OURS, not an upstream cherry-pick. They will NOT turn into
empty commits on a rebase and drop out by themselves. Offer them upstream (each
is small and self-contained); until one is merged, expect to carry it and to
resolve real conflicts rather than watching it disappear. The reasoning and the
measurements behind the two output-turn patches are in the town as `hgc-di92ml`.

`94c52b19e` and `8dca11199` were one commit, `7b078cc83`, which is what the
table used to point at as `7d53d19` — a SHA that resolves nowhere. Splitting it
dropped a third change it also carried: `Filter.MaxScanBytes`, its loop guard
and its mid-chunk clamp, plus the 8 MiB budget the event-list tail probe passed
in. Upstream landed the reader half independently as #4418, character for
character including the doc comment, so a rebase onto any tag carrying it would
conflict in `reader.go` for a resolution that is simply "take upstream's copy".

**The budget's consumer does not come back with it.** Upstream applies
`MaxScanBytes` only in `internal/storehealth`, never on the `/events` tail
probe, so a rebase onto a #4418 tag restores the field and leaves the endpoint
unbounded. The probe walks the active log until it fills `limit+1` rows, and a
selective filter never does — the wasted leg this branch measured at 1.482s
over the whole file, against 255ms inside an 8 MiB budget. Re-add the two lines
in `fetchEventPageAscending` after that rebase; the reader side will already be
there. Copy them from `4e7e9f71d` and `99563ee61` on
`origin/gsc-qwy-events-tail-read`, the original re-land of this work with the
budget intact, rather than reconstructing them.

The pre-split stack, with `7b078cc83` and the dropped hunks intact, is tagged
`backup/on-v1.4.1-presplit` on `origin`. That is the only copy of those hunks;
`7b078cc83` is unreachable from the branch now, so deleting the tag garbage
collects it.

The two output-turn patches regenerate `internal/api/openapi.json`, the
`docs/reference/schema` mirrors, and `internal/api/genclient/client_gen.go`,
because `OutputTurn` is `additionalProperties: false` and Huma derives the
schema from the Go struct. A hand-edited schema would make the response
invalid against its own spec. Run:

    make install-oapi-codegen
    go run ./cmd/genspec
    PATH="$(go env GOPATH)/bin:$PATH" go generate ./internal/api/genclient

Every other patch touches no schema and regenerates nothing. Each one's commit is
worth keeping clear of this file: PATCHES.md is the fork's own register and has no
place in what gets offered upstream, so a patch and the row that records it are
separate commits, as `25ace43` and `21f91a8` already are.

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

**The change itself is gone, and there is no recipe to bring it back.** This
section used to say `git cherry-pick d5c4a1898` off a local `pr-4721` branch,
with `ecf762c76` on `backup/on-v1.4.1-with-liveness` as a disposable second
copy. Neither branch exists in this clone and neither object resolves, on any
ref or in either remote; nothing on `upstream/main` mentions #4721 or #4703
either. Reconstructing it means reading the upstream PR, not cherry-picking.
The description above is what remains, and it is enough to decide whether
reconstruction is worth it — which, per the paragraph before it, it currently
is not for the app, only for the built-in SPA.

## `gc version` on this branch lies, and says 1.4.2

A build of this branch self-reports **1.4.2**, which is not a release: upstream's
latest tag is v1.4.1 and there is no v1.4.2 anywhere. The binary is honest; the
version string is not.

The chain, worth knowing before anyone treats it as evidence of a bad build:

1. The Makefile stamps the version from `git describe --tags --exact-match`,
   which fails on this branch (no tag points at any commit past `v1.4.1`), so it
   falls back to `-X main.version=dev`. Correct so far.
2. `cmd/gc/cmd_version.go` then treats `dev` as "unknown" and falls back to Go's
   build info: `info.Main.Version`.
3. Go stamps the main module with a **pseudo-version**, which by convention names
   the NEXT patch after the last tag, so it reads
   `v1.4.2-0.<utc timestamp>-<12 hex of the commit>`.
4. `normalizeVersion` strips the pseudo-version suffix with
   `^(.*)-0\.\d{14}-[0-9a-f]{12,}$`, leaving a bare `1.4.2`.

So a pseudo-version meaning "somewhere after v1.4.1" is rewritten into a claim to
be a release that does not exist. `git describe --tags` says the honest thing —
`v1.4.1-17-g8dca11199` for the last code patch in the table — and it stays
honest as the branch grows, which the version string does not.

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
ceremony: it is the staging ground for offering these patches upstream, and it
means the stack survives this laptop. The branch has already lost work to local
churn twice — see the missing objects noted in the table and in the liveness
section — so an unpushed commit here should be treated as one that does not
exist yet.

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
