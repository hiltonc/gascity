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


The last two rows are OURS, not upstream cherry-picks, which makes them a
different kind of patch from everything above them. They will NOT turn into
empty commits on a rebase and drop out by themselves. Offer them upstream (both
are small and self-contained); until one is merged, expect to carry it and to
resolve real conflicts rather than watching it disappear. The reasoning and the
measurements behind them are in the town as `hgc-di92ml`.

Both regenerate `internal/api/openapi.json`, the `docs/reference/schema`
mirrors, and `internal/api/genclient/client_gen.go`, because `OutputTurn` is
`additionalProperties: false` and Huma derives the schema from the Go struct. A
hand-edited schema would make the response invalid against its own spec. Run:

    make install-oapi-codegen
    go run ./cmd/genspec
    PATH="$(go env GOPATH)/bin:$PATH" go generate ./internal/api/genclient

## Removed: the agent-liveness fix (PR #4721, issue #4703)

Carried from 2026-09-09 and dropped 2026-09-10, deliberately, not because it was
wrong. `GET /v0/city/{city}/agents` still reports every running agent as
`state=stopped, running=false, session=null` — verified live that day: 0 of 14
running while a reviewer session was executing.

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

Check the API, not the filesystem:

    curl -s "http://127.0.0.1:8372/v0/city/<city>/agent/<agent>/output?tail=1"

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

Found by Jane on the-ansible, 2026-09-10, where `~/.local/bin/gc` had pointed
into brew since 2026-07-31. The workshop has the same shape after installing.
This is the sentence someone reaches for while something is already broken, so
it is worth being exactly right.

The ICU flags matter for `go test` and not for `make build`, because the
Makefile already sets them and a bare `go test` does not inherit them. Without
them the test build fails on `unicode/regex.h` from the Dolt go-icu-regex cgo
dependency.

## Verifying a patch before installing

    go test ./internal/api/ -run 'AgentSession|ResolveAgentRuntime' -count=1

Do not swap the binary while workflows are in flight. The supervisor and every
running worker are executing the binary being replaced.
