package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
)

// clientIdentity is the process-wide identity every API client this process
// builds reports in its User-Agent.
//
// Process-wide is the honest scope: one `gc` invocation runs exactly one
// subcommand, and every client it constructs — for any city, scoped or
// supervisor — is that subcommand talking. Threading it through each
// constructor would say the same thing in more places and still be set once,
// at startup, from the same argv.
var clientIdentity struct {
	mu         sync.RWMutex
	subcommand string
	version    string
}

// SetClientIdentity records which subcommand this process is running, so every
// API request it makes names its own source. The CLI calls it once during
// startup, after cobra has resolved the command and before any request.
//
// Attribution is the whole point: the api request log carries method, path,
// status and duration, but nothing about who asked. On a city whose event list
// was being hammered by short-lived gc processes, that made the caller
// impossible to identify from the log alone.
func SetClientIdentity(subcommand, version string) {
	clientIdentity.mu.Lock()
	defer clientIdentity.mu.Unlock()
	clientIdentity.subcommand = sanitizeHeaderValue(subcommand)
	clientIdentity.version = sanitizeHeaderValue(version)
}

// currentClientIdentity returns the recorded subcommand and version.
func currentClientIdentity() (subcommand, version string) {
	clientIdentity.mu.RLock()
	defer clientIdentity.mu.RUnlock()
	return clientIdentity.subcommand, clientIdentity.version
}

// clientUserAgent renders the User-Agent this process sends. It degrades to
// the bare binary name when startup has not named a subcommand — a library
// embedding, or a request issued before the CLI resolved its command.
func clientUserAgent() string {
	subcommand, version := currentClientIdentity()
	ua := "gc"
	if version != "" {
		ua += "/" + version
	}
	if subcommand != "" {
		ua += " (" + subcommand + ")"
	}
	return ua
}

// SetClientUserAgentHeader stamps this process's client identity on req. Every
// path that builds an API client goes through it — the shared constructor, the
// remote constructors, the SSE request builder, and the events command's own
// client — so no request can reach a server unattributed.
func SetClientUserAgentHeader(req *http.Request) {
	if req == nil {
		return
	}
	req.Header.Set("User-Agent", clientUserAgent())
}

// ClientIdentityRequestEditor returns a generated-client request editor that
// applies SetClientUserAgentHeader. It reads the identity per request rather
// than per client, because a client may be constructed either side of the
// CLI naming its subcommand at startup.
func ClientIdentityRequestEditor() func(context.Context, *http.Request) error {
	return func(_ context.Context, req *http.Request) error {
		SetClientUserAgentHeader(req)
		return nil
	}
}

// sanitizeHeaderValue strips anything that could terminate or forge a header
// line, and bounds the length so a long argv cannot bloat every request.
// The subcommand is derived from argv, so it is untrusted input.
func sanitizeHeaderValue(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	return sanitizeAuditString(strings.TrimSpace(value), maxClientIdentityRunes)
}

// maxClientIdentityRunes bounds each component of the User-Agent.
const maxClientIdentityRunes = 64
