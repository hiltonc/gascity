package main

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/gastownhall/gascity/internal/api"
	"github.com/spf13/cobra"
)

// newSessionPendingCmd creates the "gc session pending" command.
func newSessionPendingCmd(stdout, stderr io.Writer) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "pending",
		Short: "List sessions awaiting a human decision",
		Long: `List the active sessions whose runtime is waiting on a human decision,
such as a tool approval or a prompt for input.

Each active session's runtime is asked directly (for tmux, by matching the
approval prompt in the pane). This is the same aggregate the supervisor serves
at GET /v0/city/{cityName}/pending, and --json emits that route's response body
with one addition: the CLI's "ok": true field. The route's X-GC-Index and
cache-age response headers have no CLI equivalent. Sessions whose probe fails
are listed in partial_errors rather than failing the command.`,
		Example: `  gc session pending
  gc session pending --json`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if cmdSessionPending(jsonOutput, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit the GET /v0/city/{cityName}/pending response body")
	return cmd
}

// sessionPendingAPIClient returns (client, "") when the API path is available,
// or (nil, reason) when the caller should fall back. Indirected through a var
// so tests can inject one.
var sessionPendingAPIClient = func(cityPath string) (*api.Client, string) {
	if c := apiClient(cityPath); c != nil {
		return c, ""
	}
	return nil, apiClientFallbackReason(cityPath)
}

// cmdSessionPending is the CLI entry point for "gc session pending". It routes
// through the supervisor API when a controller is up, because in-process
// runtimes (e.g. ACP) are reachable only from the supervisor, and falls back
// to probing the local runtime provider otherwise.
func cmdSessionPending(jsonOutput bool, stdout, stderr io.Writer) int {
	return routeReadCmd("session pending", stderr, sessionPendingAPIClient, func(_ string, c *api.Client, nilReason string) int {
		return routeSessionPending(c, nilReason, jsonOutput, stdout, stderr)
	})
}

// routeSessionPending dispatches to the pending route when c is non-nil and
// falls back to the local aggregate when the API is unavailable.
func routeSessionPending(c *api.Client, nilReason string, jsonOutput bool, stdout, stderr io.Writer) int {
	const cmdName = "session pending"
	if c != nil {
		cr, err := c.CityPending()
		if err == nil {
			logRoute(stderr, cmdName, "api", "")
			return renderSessionPending(cr.Body, jsonOutput, stdout, stderr)
		}
		if !api.ShouldFallbackForRead(c, err) {
			logRoute(stderr, cmdName, "api", "error")
			fmt.Fprintf(stderr, "gc session pending: %v\n", err) //nolint:errcheck // best-effort stderr
			return 1
		}
		logRoute(stderr, cmdName, "fallback", api.FallbackReason(c, err))
	} else {
		logRoute(stderr, cmdName, "fallback", nilReason)
	}
	return doSessionPendingFallback(jsonOutput, stdout, stderr)
}

// doSessionPendingFallback computes the pending aggregate directly against the
// city store and local runtime provider, through the same
// session.Manager.CityPending the route calls.
func doSessionPendingFallback(jsonOutput bool, stdout, stderr io.Writer) int {
	store, code := openCityStore(stderr, "gc session pending")
	if store == nil {
		return code
	}
	providerCtx := loadSessionProviderContext()
	sessStore := cliSessionStore(store, providerCtx.cfg, providerCtx.cityPath)

	snapshot, err := loadSessionBeadSnapshot(sessStore)
	if err != nil {
		fmt.Fprintf(stderr, "gc session pending: listing sessions: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	sp, err := newSessionProvider()
	if err != nil {
		fmt.Fprintf(stderr, "gc session pending: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	catalog, err := workerSessionCatalogWithConfig("", sessStore, sp, providerCtx.cfg)
	if err != nil {
		fmt.Fprintf(stderr, "gc session pending: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	pending, probeErrors := catalog.CityPending(snapshot.OpenInfos())
	return renderSessionPending(api.CityPendingBody(pending, probeErrors), jsonOutput, stdout, stderr)
}

// renderSessionPending writes the pending aggregate as the route's JSON body
// or as a table. Probe failures are warnings on stderr in text mode.
func renderSessionPending(body api.ListBody[api.CityPendingEntry], jsonOutput bool, stdout, stderr io.Writer) int {
	if jsonOutput {
		return writeCLIJSONLineOrExit(stdout, stderr, "gc session pending", body)
	}
	for _, msg := range body.PartialErrors {
		fmt.Fprintf(stderr, "gc session pending: warning: %s\n", msg) //nolint:errcheck // best-effort stderr
	}
	if len(body.Items) == 0 {
		fmt.Fprintln(stdout, "No sessions awaiting a decision.") //nolint:errcheck // best-effort stdout
		return 0
	}
	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION\tKIND\tREQUEST") //nolint:errcheck // best-effort stdout
	for _, item := range body.Items {
		fmt.Fprintf(w, "%s\t%s\t%s\n", item.SessionID, sessionListDisplayValue(item.Kind), sessionListDisplayValue(item.RequestID)) //nolint:errcheck // best-effort stdout
	}
	_ = w.Flush() //nolint:errcheck // best-effort stdout
	return 0
}
