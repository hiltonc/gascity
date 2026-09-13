package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/api/genclient"
)

// restoreClientIdentity returns the process-wide client identity to its
// pre-test value so tests do not leak into each other.
func restoreClientIdentity(t *testing.T) {
	t.Helper()
	prevSub, prevVer := currentClientIdentity()
	t.Cleanup(func() { SetClientIdentity(prevSub, prevVer) })
}

// captureWithLoggingOutput runs fn with the standard logger redirected and
// returns everything it wrote.
func captureWithLoggingOutput(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	defer captureLog(t, &buf)()
	fn()
	return buf.String()
}

func TestClientUserAgentDefaultsToBareBinary(t *testing.T) {
	restoreClientIdentity(t)
	SetClientIdentity("", "")
	if got, want := clientUserAgent(), "gc"; got != want {
		t.Fatalf("clientUserAgent() = %q, want %q", got, want)
	}
}

func TestClientUserAgentNamesSubcommandAndVersion(t *testing.T) {
	restoreClientIdentity(t)
	SetClientIdentity("gc events", "1.2.3")
	if got, want := clientUserAgent(), "gc/1.2.3 (gc events)"; got != want {
		t.Fatalf("clientUserAgent() = %q, want %q", got, want)
	}
}

// TestClientUserAgentIsHeaderSafe pins that nothing reaching the header can
// break the request or forge a second header line. The subcommand is derived
// from argv, so it is attacker-adjacent input on a shared host.
func TestClientUserAgentIsHeaderSafe(t *testing.T) {
	restoreClientIdentity(t)
	SetClientIdentity("gc events\r\nX-Forged: 1", "9\n9")
	ua := clientUserAgent()
	if strings.ContainsAny(ua, "\r\n") {
		t.Fatalf("user agent %q carries a line break", ua)
	}
}

// TestClientSendsUserAgent pins the wire behavior: every request the generated
// client makes identifies the subcommand that made it, which is what lets an
// operator read a hot endpoint's caller off the server log.
func TestClientSendsUserAgent(t *testing.T) {
	restoreClientIdentity(t)
	SetClientIdentity("gc events", "test")

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, _ = c.ListCities()

	if got != "gc/test (gc events)" {
		t.Fatalf("server saw User-Agent %q, want %q", got, "gc/test (gc events)")
	}
}

// TestRequestLogNamesTheClient pins ask 1 of the bead: the api log line must
// carry the request source. Without it a hot endpoint's caller cannot be told
// from the log at all, which is exactly why the 40-to-77-second /events reads
// went unattributed.
func TestRequestLogNamesTheClient(t *testing.T) {
	h := withLogging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), requestAuditConfig{})

	logged := captureWithLoggingOutput(t, func() {
		req := httptest.NewRequest(http.MethodGet, "/v0/city/c/events", nil)
		req.Header.Set("User-Agent", "gc/1.0 (gc events)")
		h.ServeHTTP(httptest.NewRecorder(), req)
	})

	if !strings.Contains(logged, `client="gc/1.0 (gc events)"`) {
		t.Fatalf("request log does not name the client; log = %q", logged)
	}
}

// TestRequestLogClientIsSanitized pins that a hostile User-Agent cannot inject
// extra lines into the server log.
func TestRequestLogClientIsSanitized(t *testing.T) {
	h := withLogging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), requestAuditConfig{})

	logged := captureWithLoggingOutput(t, func() {
		req := httptest.NewRequest(http.MethodGet, "/v0/city/c/events", nil)
		req.Header.Set("User-Agent", "evil\napi: GET /forged 200 1s [memory]")
		h.ServeHTTP(httptest.NewRecorder(), req)
	})

	if strings.Contains(logged, "/forged") && strings.Contains(logged, "\napi: GET /forged") {
		t.Fatalf("hostile User-Agent forged a log line; log = %q", logged)
	}
}

// TestRequestLogOmitsClientWhenAbsent keeps the line unchanged for callers
// that send no User-Agent at all.
func TestRequestLogOmitsClientWhenAbsent(t *testing.T) {
	h := withLogging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), requestAuditConfig{})

	logged := captureWithLoggingOutput(t, func() {
		req := httptest.NewRequest(http.MethodGet, "/v0/city/c/events", nil)
		req.Header.Del("User-Agent")
		h.ServeHTTP(httptest.NewRecorder(), req)
	})

	if strings.Contains(logged, "client=") {
		t.Fatalf("log should omit client= when the caller sent no User-Agent; log = %q", logged)
	}
}

// TestEveryClientConstructorStampsIdentity guards the gap that made the hot
// event-list caller unidentifiable: a construction site that builds its own
// generated client and forgets the identity editor sends unattributed
// requests. Every stamping site is exercised by issuing a real request and
// reading the User-Agent the server actually received, so a constructor that
// drops its editor fails here even though the editor itself still works.
func TestEveryClientConstructorStampsIdentity(t *testing.T) {
	restoreClientIdentity(t)
	SetClientIdentity("gc events", "test")
	const want = "gc/test (gc events)"

	seen := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	// Each subtest issues one request and asserts on the header the server
	// saw. Transport-level errors are ignored: the assertion is about what
	// went out, not about the canned response coming back.
	t.Run("shared constructor", func(t *testing.T) {
		_, _ = NewClient(srv.URL).ListCities()
		if got := <-seen; got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
	})

	t.Run("remote city scoped client", func(t *testing.T) {
		c, err := NewRemoteCityScopedClient(srv.URL, "", RemoteOptions{})
		if err != nil {
			t.Fatalf("NewRemoteCityScopedClient: %v", err)
		}
		_, _ = c.ListCities()
		if got := <-seen; got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
	})

	t.Run("remote events client", func(t *testing.T) {
		cw, err := NewRemoteEventsClient(srv.URL, RemoteOptions{})
		if err != nil {
			t.Fatalf("NewRemoteEventsClient: %v", err)
		}
		_, _ = cw.GetV0EventsWithResponse(context.Background(), &genclient.GetV0EventsParams{})
		if got := <-seen; got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
	})

	t.Run("stream request builder", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c := NewClient(srv.URL)
		_, _, _, _ = c.waitForEventOnce(ctx, "", "", "", "", nil)
		if got := <-seen; got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
	})
}
