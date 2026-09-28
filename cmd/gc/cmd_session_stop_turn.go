package main

import (
	"context"
	"fmt"
	"io"

	"github.com/gastownhall/gascity/internal/api"
	"github.com/gastownhall/gascity/internal/worker"
	"github.com/spf13/cobra"
)

// sessionStopTurnJSON is the POST /v0/city/{cityName}/session/{id}/stop
// response body, which `gc session stop-turn --json` emits unchanged.
type sessionStopTurnJSON struct {
	Status string `json:"status"`
	ID     string `json:"id"`
}

// newSessionStopTurnCmd creates the "gc session stop-turn <session>" command.
func newSessionStopTurnCmd(stdout, stderr io.Writer) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "stop-turn <session-id-or-alias>",
		Short: "Interrupt a session's running turn and wait for idle",
		Long: `Interrupt the turn a session is currently running and wait until the
session is back at an idle prompt.

This sends the provider's own interrupt, the same operation the supervisor
performs for POST /v0/city/{cityName}/session/{id}/stop; --json emits that
route's response body ({"status":"ok","id":...}) plus the CLI's "ok": true
field. It sends no message. To interrupt and hand the session
new instructions, use "gc session submit --intent interrupt_now".

Accepts a session ID (e.g., gc-42) or session alias (e.g., mayor).`,
		Example: `  gc session stop-turn mayor
  gc session stop-turn gc-42 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if cmdSessionStopTurn(args[0], jsonOutput, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
		ValidArgsFunction: completeSessionIDs,
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit the POST /v0/city/{cityName}/session/{id}/stop response body")
	return cmd
}

// sessionStopTurnAPIClient returns the supervisor API client, or nil when the
// command should interrupt locally. Indirected through a var so tests can
// inject one.
var sessionStopTurnAPIClient = apiClient

// cmdSessionStopTurn is the CLI entry point for "gc session stop-turn". With a
// controller up it routes through the stop route so the supervisor interrupts
// under its own session locks; otherwise it interrupts through the local
// worker handle, which calls the same session.Manager.StopTurn.
func cmdSessionStopTurn(target string, jsonOutput bool, stdout, stderr io.Writer) int {
	cityPath, err := resolveCity()
	if err != nil {
		fmt.Fprintf(stderr, "gc session stop-turn: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	if c := sessionStopTurnAPIClient(cityPath); c != nil {
		id, err := c.StopSessionTurn(target)
		if err == nil {
			return emitSessionStopTurnResult(id, jsonOutput, stdout, stderr)
		}
		if !api.ShouldFallback(c, err) {
			fmt.Fprintf(stderr, "gc session stop-turn: %v\n", err) //nolint:errcheck // best-effort stderr
			return 1
		}
	}

	cfg, err := loadCityConfig(cityPath, configWarnWriter(jsonOutput, stderr))
	if err != nil {
		fmt.Fprintf(stderr, "gc session stop-turn: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	store, code := openCityStore(stderr, "gc session stop-turn")
	if store == nil {
		return code
	}
	sessStore := cliSessionStore(store, cfg, cityPath)
	sessionID, err := resolveSessionIDWithConfig(cityPath, cfg, sessStore, target)
	if err != nil {
		fmt.Fprintf(stderr, "gc session stop-turn: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	sp, err := newSessionProvider()
	if err != nil {
		fmt.Fprintf(stderr, "gc session stop-turn: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	handle, err := workerHandleForSessionWithConfig(cityPath, sessStore, sp, cfg, sessionID)
	if err != nil {
		fmt.Fprintf(stderr, "gc session stop-turn: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	if err := handle.Interrupt(context.Background(), worker.InterruptRequest{}); err != nil {
		fmt.Fprintf(stderr, "gc session stop-turn: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	return emitSessionStopTurnResult(sessionID, jsonOutput, stdout, stderr)
}

func emitSessionStopTurnResult(sessionID string, jsonOutput bool, stdout, stderr io.Writer) int {
	if jsonOutput {
		return writeCLIJSONLineOrExit(stdout, stderr, "gc session stop-turn", sessionStopTurnJSON{
			Status: "ok",
			ID:     sessionID,
		})
	}
	fmt.Fprintf(stdout, "Stopped turn for %s\n", sessionID) //nolint:errcheck // best-effort stdout
	return 0
}
