# Local patch queue

This branch is upstream Gas City at a release tag, with a small set of upstream
fixes we care about cherry-picked on top. It is not a fork: nothing here is
ours to maintain, and every patch is meant to disappear when upstream merges it.

## Branch shape

    high/v<version>     branched from tag v<version>, patches on top, newest last

One branch per upstream release. `high/v1.4.1` is branched from `v1.4.1`.

## Patches on high/v1.4.1

| Commit | Upstream | What it fixes |
| --- | --- | --- |
| local | none | `mise.toml` pinning go 1.26.5, which `go.mod` requires and upstream does not pin. Build-environment only, no product change. |
| `850d560` | none yet, OURS | `OutputTurn` carried only `{role, text, timestamp}`. The agent output endpoints accept `before`/`after` entry-ID cursors and report `has_older_messages`, so a client could see that older messages exist and have nothing to send as a cursor; an invented ID answers 500. Adds the entry ID to every turn, on the paged read, the live stream, and the history path. |
| `3d55dc0` | none yet, OURS | A `tool_use` block rendered as its bare name, so a transcript read as `[Bash]` then a result: answers to questions that are never shown. Over one agent's 733 turns, 225 tool labels against 225 results. The input was already parsed and only the name serialized; appends the field a human reads first, flattened to one line and bounded at 500 like `tool_result` beside it. |


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

To bring it back: `git cherry-pick ecf762c76`, still on
`backup/high-v1.4.1-with-liveness`.

## A patch is not live until the supervisor restarts

`make install` writes `~/go/bin/gc` and symlinks `~/.local/bin/gc`, which is
ahead of brew on PATH, so a shell's `gc` is patched immediately. The long-lived
`gc supervisor` process keeps executing whatever binary launched it. On
2026-09-10 that process had been up 2d14h and was still `/opt/homebrew/bin/gc`,
so the liveness fix installed on 2026-09-09 had never once run.

Check the API, not the filesystem:

    curl -s "http://127.0.0.1:8372/v0/city/<city>/agent/<agent>/output?tail=1"

Installing is safe at any time; restarting drops whatever workflow is mid-flight.

## Rebasing onto a new release

    git fetch origin --tags
    git checkout -b high/v1.4.2 high/v1.4.1
    git rebase --onto v1.4.2 v1.4.1 high/v1.4.2

A patch upstream has merged becomes an empty commit and drops out, which is the
signal to delete its row from the table above. Resolve anything else by hand,
then rebuild and re-run the patch's own tests before installing.

## Build and install

    ICU=$(brew --prefix icu4c)
    export CGO_CPPFLAGS="-I$ICU/include" CGO_LDFLAGS="-L$ICU/lib"
    make build           # -> bin/gc, signed for macOS
    make install         # -> $GOPATH/bin/gc, NOT brew's prefix

`make install` does not touch `/opt/homebrew/bin/gc`, so the brew install stays
intact and PATH order decides which binary runs. To go back to stock, remove the
GOPATH copy or put brew first on PATH; `brew upgrade gascity` is unaffected
either way.

The ICU flags matter for `go test` and not for `make build`, because the
Makefile already sets them and a bare `go test` does not inherit them. Without
them the test build fails on `unicode/regex.h` from the Dolt go-icu-regex cgo
dependency.

## Verifying a patch before installing

    go test ./internal/api/ -run 'AgentSession|ResolveAgentRuntime' -count=1

Do not swap the binary while workflows are in flight. The supervisor and every
running worker are executing the binary being replaced.
