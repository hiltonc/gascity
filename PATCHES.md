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
| `ecf762c` | PR #4721, issue #4703 | `GET /v0/city/{city}/agents` reports every bounded-pool worker as `state=stopped, running=false` while it is executing. The handler reconstructs a deterministic tmux name from the agent slot and probes it, but the runtime names ephemeral pool sessions after the session id and without the rig prefix, so the probe never matches. Everything gated on `running` is skipped too: `Session`, `LastActivity`, `Attached`, peek, `enrichSessionMeta`. The fix indexes live sessions by qualified identity and consults that index when the probe misses. |
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

Upstream PR #4721 was open and `CONFLICTING` against `main` as of 2026-09-09,
last touched 2026-08-14, with no review. It cherry-picks cleanly onto `v1.4.1`,
because the conflict is with main having moved on rather than with the release.

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
