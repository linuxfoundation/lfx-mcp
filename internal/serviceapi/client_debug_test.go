// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package serviceapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// stubRoundTripper answers every request with one canned response.
type stubRoundTripper struct {
	resp *http.Response
}

func (s *stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return s.resp, nil
}

// failingBody yields prefix and then fails with err, the way a transport
// read error surfaces part-way through a response body.
type failingBody struct {
	prefix io.Reader
	err    error
	closed bool
}

func (f *failingBody) Read(p []byte) (int, error) {
	n, err := f.prefix.Read(p)
	if err == io.EOF {
		return n, f.err
	}
	return n, err
}

func (f *failingBody) Close() error {
	f.closed = true
	return nil
}

// newDebugClientForTest builds a Client whose HTTP transport is rt, with the
// debug transport enabled and its DEBUG-level log lines captured in the
// returned buffer.
func newDebugClientForTest(t *testing.T, rt http.RoundTripper) (*Client, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	client, err := NewClient(Config{
		BaseURL:     "https://service.example.test",
		TokenSource: staticToken("token"),
		HTTPClient:  &http.Client{Transport: rt},
		DebugLogger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, &logs
}

func TestServiceDebugTransport_BodyReadErrorIsReturned(t *testing.T) {
	readErr := errors.New("connection reset by peer")
	body := &failingBody{prefix: strings.NewReader(`{"items": [`), err: readErr}
	client, _ := newDebugClientForTest(t, &stubRoundTripper{resp: &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
	}})

	got, status, err := client.Get(context.Background(), "/v1/onboarding/status", nil)
	if err == nil {
		t.Fatalf("a body read error must fail the call as this request's error; got status %d body %q", status, got)
	}
	if got != nil {
		t.Errorf("no body must be returned alongside the error, got %q", got)
	}
	if !errors.Is(err, readErr) {
		t.Errorf("the transport error must be wrapped, got %v", err)
	}
	if !strings.Contains(err.Error(), "/v1/onboarding/status") {
		t.Errorf("the error must name the request path, got %v", err)
	}
	if !body.closed {
		t.Error("the upstream body must be closed after the failed read")
	}
}

func TestServiceDebugTransport_HealthyBodyReachesCallerIntact(t *testing.T) {
	const payload = `{"items": [{"uid": "m-1"}], "total": 1}`
	body := &failingBody{prefix: strings.NewReader(payload), err: io.EOF}
	client, logs := newDebugClientForTest(t, &stubRoundTripper{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		ContentLength: int64(len(payload)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          body,
	}})

	got, status, err := client.Get(context.Background(), "/v1/onboarding/status", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status must pass through unchanged, got %d", status)
	}
	if string(got) != payload {
		t.Errorf("caller must read the complete body: want %q got %q", payload, got)
	}
	if !body.closed {
		t.Error("the upstream body must be closed once it has been read")
	}
	if !strings.Contains(logs.String(), "serviceapi inbound response") || !strings.Contains(logs.String(), `\"total\": 1`) {
		t.Errorf("the debug dump must still carry the response body, got logs:\n%s", logs.String())
	}
}

func TestServiceDebugTransport_NilBodyIsTolerated(t *testing.T) {
	client, logs := newDebugClientForTest(t, &stubRoundTripper{resp: &http.Response{
		StatusCode: http.StatusNoContent,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Body:       nil,
	}})

	got, status, err := client.Get(context.Background(), "/v1/onboarding/ping", nil)
	if err != nil {
		t.Fatalf("a nil body from a non-standard transport must not fail the call: %v", err)
	}
	if status != http.StatusNoContent || len(got) != 0 {
		t.Errorf("want 204 with an empty body, got %d %q", status, got)
	}
	if !strings.Contains(logs.String(), "serviceapi inbound response") {
		t.Errorf("the response must still be dumped, got logs:\n%s", logs.String())
	}
}

// recordingRoundTripper answers with one canned response and keeps what it
// was sent: the request and the body bytes it read.
type recordingRoundTripper struct {
	resp    *http.Response
	gotReq  *http.Request
	gotBody []byte
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.gotReq = req
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		r.gotBody = b
	}
	return r.resp, nil
}

func TestServiceDebugTransport_MasksCredentialsAndSignedLinksInLogOnly(t *testing.T) {
	// Test values only: none of these is a real credential.
	const (
		bearer    = "test-service-bearer-value"
		signature = "test-signature-value"
		cookie    = "test-cookie-value"
	)
	payload := `{"status":"sent","report_url":"https://files.example.test/r.csv?X-Amz-Expires=3600&X-Amz-Signature=` + signature + `"}`
	rt := &recordingRoundTripper{resp: &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"Set-Cookie":   []string{"sid=" + cookie},
		},
		Body: io.NopCloser(strings.NewReader(payload)),
	}}
	var logs bytes.Buffer
	client, err := NewClient(Config{
		BaseURL:     "https://service.example.test",
		TokenSource: staticToken(bearer),
		HTTPClient:  &http.Client{Transport: rt},
		DebugLogger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	reqBody := map[string]string{"template": "welcome"}
	got, status, err := client.PostJSON(context.Background(), "/v1/onboarding/email", reqBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The request reaches the upstream unchanged.
	if h := rt.gotReq.Header.Get("Authorization"); h != "Bearer "+bearer {
		t.Errorf("the upstream must receive the original Authorization header, got %q", h)
	}
	if string(rt.gotBody) != `{"template":"welcome"}` {
		t.Errorf("the upstream must receive the original body, got %q", rt.gotBody)
	}

	// The response reaches the caller unchanged.
	if status != http.StatusOK || string(got) != payload {
		t.Errorf("caller must read the original response byte for byte: status %d body %q", status, got)
	}

	// Only the logged copy is masked.
	out := logs.String()
	for _, secret := range []string{bearer, signature, cookie} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q must not appear in the debug log:\n%s", secret, out)
		}
	}
	for _, kept := range []string{
		"serviceapi outbound request", "serviceapi inbound response",
		"Authorization: Bearer [REDACTED]", "Set-Cookie: [REDACTED]",
		"/v1/onboarding/email", `\"template\":\"welcome\"`, `\"status\":\"sent\"`,
		"X-Amz-Expires=3600", "X-Amz-Signature=[REDACTED]",
	} {
		if !strings.Contains(out, kept) {
			t.Errorf("the debug log must keep %q, got:\n%s", kept, out)
		}
	}
}

func TestServiceDebugTransport_MasksSignedLinkInRequestLineOnly(t *testing.T) {
	// Test value only: not a real credential.
	const signature = "test-signature-value"
	rt := &recordingRoundTripper{resp: &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}}
	client, logs := newDebugClientForTest(t, rt)

	query := url.Values{"X-Amz-Expires": {"3600"}, "X-Amz-Signature": {signature}}
	if _, _, err := client.Get(context.Background(), "/v1/onboarding/status", query); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The upstream receives the original URL.
	if got := rt.gotReq.URL.Query().Get("X-Amz-Signature"); got != signature {
		t.Errorf("the upstream must receive the original URL, got signature %q", got)
	}

	// Only the logged request line is masked.
	out := logs.String()
	if strings.Contains(out, signature) {
		t.Errorf("the signature must not appear in the debug log:\n%s", out)
	}
	for _, kept := range []string{"/v1/onboarding/status?", "X-Amz-Expires=3600", "X-Amz-Signature=[REDACTED]"} {
		if !strings.Contains(out, kept) {
			t.Errorf("the debug log must keep %q, got:\n%s", kept, out)
		}
	}
}

func TestServiceDebugTransport_ReadErrorLogMasksSignedLink(t *testing.T) {
	// Test value only: not a real credential.
	const signature = "test-signature-value"
	readErr := errors.New("connection reset by peer")
	client, logs := newDebugClientForTest(t, &stubRoundTripper{resp: &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Body:       &failingBody{prefix: strings.NewReader(`{"ok"`), err: readErr},
	}})

	query := url.Values{"X-Amz-Signature": {signature}}
	if _, _, err := client.Get(context.Background(), "/v1/onboarding/status", query); !errors.Is(err, readErr) {
		t.Fatalf("the read error must still fail the call, got %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "failed to read inbound response body") {
		t.Errorf("the read failure must be logged, got:\n%s", out)
	}
	if strings.Contains(out, signature) || !strings.Contains(out, "X-Amz-Signature=[REDACTED]") {
		t.Errorf("the logged URL must have its signature masked, got:\n%s", out)
	}
}

// failingRoundTripper fails every request the way net/http does: with a
// *url.Error that prints the full request URL.
type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, &url.Error{Op: req.Method, URL: req.URL.String(), Err: errors.New("dial tcp: connection refused")}
}

func TestServiceDebugTransport_TransportErrorLogMasksSignedLink(t *testing.T) {
	// Test value only: not a real credential.
	const signature = "test-signature-value"
	client, logs := newDebugClientForTest(t, failingRoundTripper{})
	query := url.Values{"X-Amz-Signature": {signature}}
	var urlErr *url.Error
	if _, _, err := client.Get(context.Background(), "/v1/onboarding/status", query); !errors.As(err, &urlErr) {
		t.Fatalf("the transport error must fail the call, got %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "outbound request failed") {
		t.Errorf("the transport failure must be logged, got:\n%s", out)
	}
	if strings.Contains(out, signature) || !strings.Contains(out, "X-Amz-Signature=[REDACTED]") {
		t.Errorf("the logged error must have its signature masked, got:\n%s", out)
	}
}

// TestServiceDebugTransport_MasksSecretsSplitAcrossChunks pins that a chunked response is masked
// even when the upstream splits a secret across chunks: the transport reads
// the whole body before dumping it, so the dump carries one chunk and no
// chunk-size line can fall inside a masked key or value.
func TestServiceDebugTransport_MasksSecretsSplitAcrossChunks(t *testing.T) {
	// Test values only. The record is padded past the 32 KiB copy buffer and
	// flushed in four parts that split the passcode key, its value and the
	// join-link parameter.
	const (
		passcode     = "test-passcode"
		joinPasscode = "test-join-passcode"
	)
	parts := []string{
		`{"title":"` + strings.Repeat("a", 40*1024) + `","pass`,
		`code":"` + passcode[:6],
		passcode[6:] + `","join_url":"https://meet.example.test/j/1?pw`,
		`d=` + joinPasscode + `"}`,
	}
	payload := strings.Join(parts, "")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		for _, part := range parts {
			_, _ = io.WriteString(w, part)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	var logs bytes.Buffer
	client := wrapWithDebugTransport(&http.Client{}, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if len(resp.TransferEncoding) == 0 || resp.TransferEncoding[0] != "chunked" {
		t.Fatalf("the upstream response must be chunked for this test, got %v", resp.TransferEncoding)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the restored body: %v", err)
	}
	if string(got) != payload {
		t.Error("caller must read the original body byte for byte")
	}

	out := logs.String()
	for _, secret := range []string{passcode, joinPasscode} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q must not appear in the debug log", secret)
		}
	}
	for _, kept := range []string{`\"passcode\":\"[REDACTED]\"`, `/j/1?pwd=[REDACTED]`} {
		if !strings.Contains(out, kept) {
			t.Errorf("the debug log must keep %q", kept)
		}
	}
}
