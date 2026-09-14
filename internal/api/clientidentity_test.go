package api

import (
	"bytes"
	"context"
	"io"
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

	if strings.Contains(logged, "\napi: GET /forged") {
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

// capturingDoer stands in for the network on both client shapes: it satisfies
// genclient.HttpRequestDoer for the generated clients and http.RoundTripper for
// the SSE request builder. Using it instead of a loopback httptest server keeps
// this test free of a listener — the assertion is about the header a request
// carries when it goes out, which is settled by the editor chain long before a
// socket would be involved.
type capturingDoer struct{ seen chan string }

func newCapturingDoer() *capturingDoer {
	return &capturingDoer{seen: make(chan string, 4)}
}

func (d *capturingDoer) Do(req *http.Request) (*http.Response, error) {
	d.seen <- req.Header.Get("User-Agent")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"items":[]}`)),
		Request:    req,
	}, nil
}

func (d *capturingDoer) RoundTrip(req *http.Request) (*http.Response, error) { return d.Do(req) }

// userAgent reports the header of the next request the doer saw.
func (d *capturingDoer) userAgent(t *testing.T) string {
	t.Helper()
	select {
	case ua := <-d.seen:
		return ua
	default:
		t.Fatal("no request reached the transport")
		return ""
	}
}

// interceptGenerated swaps a generated client's transport for d. It fails the
// test rather than skipping if the generated client is not the concrete type,
// so a codegen change cannot silently turn this guard into a no-op.
func interceptGenerated(t *testing.T, cw *genclient.ClientWithResponses, d *capturingDoer) {
	t.Helper()
	inner, ok := cw.ClientInterface.(*genclient.Client)
	if !ok {
		t.Fatalf("generated client is %T, want *genclient.Client", cw.ClientInterface)
	}
	inner.Client = d
}

// TestEveryClientConstructorStampsIdentity guards the gap that made the hot
// event-list caller unidentifiable: a construction site that builds its own
// generated client and forgets the identity editor sends unattributed
// requests. Every stamping site is exercised by issuing a real request through
// the client's own editor chain and reading the User-Agent that request
// carried, so a constructor that drops its editor fails here even though the
// editor itself still works.
func TestEveryClientConstructorStampsIdentity(t *testing.T) {
	restoreClientIdentity(t)
	SetClientIdentity("gc events", "test")
	const want = "gc/test (gc events)"
	const baseURL = "https://gc.invalid"

	// Transport errors are not asserted on: the subject is what went out, not
	// the canned response coming back.
	t.Run("shared constructor", func(t *testing.T) {
		d := newCapturingDoer()
		c := NewClient(baseURL)
		interceptGenerated(t, c.cw, d)
		_, _ = c.ListCities()
		if got := d.userAgent(t); got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
	})

	t.Run("remote city scoped client", func(t *testing.T) {
		d := newCapturingDoer()
		c, err := NewRemoteCityScopedClient(baseURL, "", RemoteOptions{})
		if err != nil {
			t.Fatalf("NewRemoteCityScopedClient: %v", err)
		}
		interceptGenerated(t, c.cw, d)
		_, _ = c.ListCities()
		if got := d.userAgent(t); got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
	})

	t.Run("remote events client", func(t *testing.T) {
		d := newCapturingDoer()
		cw, err := NewRemoteEventsClient(baseURL, RemoteOptions{})
		if err != nil {
			t.Fatalf("NewRemoteEventsClient: %v", err)
		}
		interceptGenerated(t, cw, d)
		_, _ = cw.GetV0EventsWithResponse(context.Background(), &genclient.GetV0EventsParams{})
		if got := d.userAgent(t); got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
	})

	t.Run("stream request builder", func(t *testing.T) {
		d := newCapturingDoer()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c := NewClient(baseURL)
		c.streamClient = &http.Client{Transport: d}
		_, _, _, _ = c.waitForEventOnce(ctx, "", "", "", "", nil)
		if got := d.userAgent(t); got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
	})
}
