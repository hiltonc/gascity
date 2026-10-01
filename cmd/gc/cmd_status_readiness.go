package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/gastownhall/gascity/internal/api"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/spf13/cobra"
)

// statusReadinessProbe is the readiness entry point, indirected so tests can
// substitute canned probe results without touching the host's CLI logins.
var statusReadinessProbe = api.ProbeCityReadiness

// newStatusReadinessCmd creates the "gc status readiness" command.
func newStatusReadinessCmd(stdout, stderr io.Writer) *cobra.Command {
	var items string
	var fresh bool
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "readiness",
		Short: "Show whether each provider CLI is installed and logged in",
		Long: `Probe each provider CLI (and the GitHub CLI) on this host and report
whether it is installed and logged in.

This is the same probe the supervisor serves at GET /v0/city/{cityName}/readiness,
and --json emits that route's response body plus the CLI's "ok": true field.
It probes the host's CLI logins directly, so it needs neither a city nor a
running supervisor. Inside a city, each provider's probe also sees the env the
city configures for it ([providers.<name>.env]), as its sessions do.

Statuses: configured, needs_auth, not_installed, invalid_configuration,
probe_error.`,
		Example: `  gc status readiness
  gc status readiness --items claude,codex --json
  gc status readiness --fresh`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmdStatusReadiness(cmd.Context(), items, fresh, jsonOutput, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&items, "items", "", "comma-separated items to probe (default: claude,codex,gemini,github_cli)")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "bypass the short-lived probe cache")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit the GET /v0/city/{cityName}/readiness response body")
	return cmd
}

// cmdStatusReadiness is the CLI entry point for "gc status readiness".
func cmdStatusReadiness(ctx context.Context, items string, fresh, jsonOutput bool, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := statusReadinessProbe(ctx, readinessCityConfig(), items, fresh)
	if err != nil {
		code := "readiness_probe_failed"
		var invalid *api.InvalidReadinessItemsError
		if errors.As(err, &invalid) {
			code = "invalid_readiness_items"
		}
		if jsonOutput {
			return writeJSONError(stdout, stderr, code, fmt.Sprintf("gc status readiness: %v", err), 1)
		}
		fmt.Fprintf(stderr, "gc status readiness: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	if jsonOutput {
		return writeCLIJSONLineOrExit(stdout, stderr, "gc status readiness", resp)
	}

	names := make([]string, 0, len(resp.Items))
	for name := range resp.Items {
		names = append(names, name)
	}
	sort.Strings(names)
	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ITEM\tKIND\tSTATUS\tDETAIL") //nolint:errcheck // best-effort stdout
	for _, name := range names {
		item := resp.Items[name]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.DisplayName, item.Kind, item.Status, sessionListDisplayValue(item.Detail)) //nolint:errcheck // best-effort stdout
	}
	_ = w.Flush() //nolint:errcheck // best-effort stdout
	return 0
}

// readinessCityConfig loads the enclosing city's config, best-effort. Readiness
// needs no city, but inside one a provider's env (say, a gateway's
// ANTHROPIC_BASE_URL) lives in city.toml, not in this process's env.
func readinessCityConfig() *config.City {
	cityPath, err := resolveCity()
	if err != nil {
		return nil
	}
	cfg, err := loadCityConfig(cityPath, io.Discard)
	if err != nil {
		return nil
	}
	return cfg
}
