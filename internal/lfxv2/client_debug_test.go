// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"bytes"
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

// newDebugTransportForTest wraps rt in a debugTransport whose DEBUG-level
// log lines are captured in the returned buffer.
func newDebugTransportForTest(rt http.RoundTripper) (*debugTransport, *bytes.Buffer) {
	var logs bytes.Buffer
	return &debugTransport{
		transport: rt,
		logger:    slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, &logs
}

func TestDebugTransport_BodyReadErrorIsReturned(t *testing.T) {
	readErr := errors.New("connection reset by peer")
	body := &failingBody{prefix: strings.NewReader(`{"count": 1`), err: readErr}
	dt, _ := newDebugTransportForTest(&stubRoundTripper{resp: &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
	}})

	req, err := http.NewRequest(http.MethodGet, "https://api.example.test/query/resources/count?v=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := dt.RoundTrip(req)
	if err == nil {
		t.Fatal("a body read error must fail the round trip, not return a response with a drained body")
	}
	if resp != nil {
		t.Errorf("no response must be returned alongside the error, got %+v", resp)
	}
	if !errors.Is(err, readErr) {
		t.Errorf("the transport error must be wrapped, got %v", err)
	}
	if !strings.Contains(err.Error(), "/query/resources/count") {
		t.Errorf("the error must name the request path, got %v", err)
	}
	if !body.closed {
		t.Error("the upstream body must be closed after the failed read")
	}
}

func TestDebugTransport_HealthyBodyReachesCallerIntact(t *testing.T) {
	const payload = `{"count": 42, "has_more": false}`
	body := &failingBody{prefix: strings.NewReader(payload), err: io.EOF}
	dt, logs := newDebugTransportForTest(&stubRoundTripper{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		ContentLength: int64(len(payload)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          body,
	}})

	req, err := http.NewRequest(http.MethodGet, "https://api.example.test/query/resources/count?v=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := dt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the restored body: %v", err)
	}
	if string(got) != payload {
		t.Errorf("caller must read the complete body: want %q got %q", payload, got)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" || resp.ContentLength != int64(len(payload)) {
		t.Errorf("status, headers and content length must pass through unchanged: %d %v %d", resp.StatusCode, resp.Header, resp.ContentLength)
	}
	if !body.closed {
		t.Error("the upstream body must be closed once it has been read")
	}
	if !strings.Contains(logs.String(), "lfxv2 inbound response") || !strings.Contains(logs.String(), `\"count\": 42`) {
		t.Errorf("the debug dump must still carry the response body, got logs:\n%s", logs.String())
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

// Test values only: none of these is a real credential.
const (
	debugTestBearer    = "test-bearer-value"
	debugTestSignature = "test-signature-value"
	debugTestSession   = "test-session-value"
	debugTestCookie    = "test-cookie-value"
)

func TestDebugTransport_MasksCredentialsAndSignedLinksInLogOnly(t *testing.T) {
	// A pre-signed download link as the meeting service returns it; Go's
	// JSON encoder writes "&" as \u0026.
	payload := `{"uid":"att-7","name":"agenda.pdf","download_url":"https://files.example.test/att-7/agenda.pdf` +
		`?X-Amz-Algorithm=AWS4-HMAC-SHA256\u0026X-Amz-Date=20260101T000000Z\u0026X-Amz-Expires=3600` +
		`\u0026X-Amz-Security-Token=` + debugTestSession + `\u0026X-Amz-Signature=` + debugTestSignature + `"}`
	respHeader := http.Header{
		"Content-Type": []string{"application/json"},
		"Set-Cookie":   []string{"sid=" + debugTestCookie + "; Path=/; HttpOnly"},
	}
	rt := &recordingRoundTripper{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		ContentLength: int64(len(payload)),
		Header:        respHeader,
		Body:          io.NopCloser(strings.NewReader(payload)),
	}}
	dt, logs := newDebugTransportForTest(rt)

	const reqBody = `{"name":"agenda.pdf"}`
	req, err := http.NewRequest(http.MethodPost,
		"https://api.example.test/meetings/m-1/attachments?v=1&X-Amz-Signature="+debugTestSignature,
		strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+debugTestBearer)
	req.Header.Set("Content-Type", "application/json")

	resp, err := dt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The request reaches the upstream unchanged.
	if got := rt.gotReq.Header.Get("Authorization"); got != "Bearer "+debugTestBearer {
		t.Errorf("the upstream must receive the original Authorization header, got %q", got)
	}
	if got := rt.gotReq.URL.Query().Get("X-Amz-Signature"); got != debugTestSignature {
		t.Errorf("the upstream must receive the original URL, got signature %q", got)
	}
	if string(rt.gotBody) != reqBody {
		t.Errorf("the upstream must receive the original body: want %q got %q", reqBody, rt.gotBody)
	}

	// The response reaches the caller unchanged.
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the restored body: %v", err)
	}
	if string(got) != payload {
		t.Errorf("caller must read the original body byte for byte: want %q got %q", payload, got)
	}
	if resp.Header.Get("Set-Cookie") != "sid="+debugTestCookie+"; Path=/; HttpOnly" {
		t.Errorf("response headers must pass through unchanged, got %v", resp.Header)
	}

	// Only the logged copy is masked.
	out := logs.String()
	for _, secret := range []string{debugTestBearer, debugTestSignature, debugTestSession, debugTestCookie} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q must not appear in the debug log:\n%s", secret, out)
		}
	}
	for _, kept := range []string{
		"lfxv2 outbound request", "lfxv2 inbound response",
		"Authorization: Bearer [REDACTED]", "Set-Cookie: [REDACTED]",
		"/meetings/m-1/attachments?v=1", `\"name\":\"agenda.pdf\"`, `\"uid\":\"att-7\"`,
		"X-Amz-Date=20260101T000000Z", "X-Amz-Signature=[REDACTED]",
	} {
		if !strings.Contains(out, kept) {
			t.Errorf("the debug log must keep %q, got:\n%s", kept, out)
		}
	}
}

func TestDebugTransport_ReadErrorLogMasksSignedLink(t *testing.T) {
	readErr := errors.New("connection reset by peer")
	dt, logs := newDebugTransportForTest(&stubRoundTripper{resp: &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Body:       &failingBody{prefix: strings.NewReader(`{"uid"`), err: readErr},
	}})

	req, err := http.NewRequest(http.MethodGet, "https://files.example.test/att-7/agenda.pdf?X-Amz-Signature="+debugTestSignature, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dt.RoundTrip(req); !errors.Is(err, readErr) {
		t.Fatalf("the read error must still fail the round trip, got %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "failed to read inbound response body") {
		t.Errorf("the read failure must be logged, got:\n%s", out)
	}
	if strings.Contains(out, debugTestSignature) {
		t.Errorf("the logged URL must have its signature masked, got:\n%s", out)
	}
}

func TestDebugTransport_MasksMeetingPasscodesInLogOnly(t *testing.T) {
	// Test values only. A meeting record as the query service returns it.
	const (
		joinPasscode = "test-join-passcode"
		passcode     = "test-passcode"
		hostKey      = "test-host-key"
	)
	payload := `{"resources":[{"type":"meeting","data":{"uid":"m-1","title":"Weekly sync",` +
		`"join_url":"https://meet.example.test/j/1234567890?pwd=` + joinPasscode + `",` +
		`"passcode":"` + passcode + `","host_key":"` + hostKey + `"}}]}`
	rt := &recordingRoundTripper{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		ContentLength: int64(len(payload)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(payload)),
	}}
	dt, logs := newDebugTransportForTest(rt)

	req, err := http.NewRequest(http.MethodGet, "https://api.example.test/query/resources?type=meeting", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := dt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the restored body: %v", err)
	}
	if string(got) != payload {
		t.Errorf("caller must read the original body byte for byte: want %q got %q", payload, got)
	}

	out := logs.String()
	for _, secret := range []string{joinPasscode, passcode, hostKey} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q must not appear in the debug log:\n%s", secret, out)
		}
	}
	for _, kept := range []string{
		`\"title\":\"Weekly sync\"`, `/j/1234567890?pwd=[REDACTED]`,
		`\"passcode\":\"[REDACTED]\"`, `\"host_key\":\"[REDACTED]\"`,
	} {
		if !strings.Contains(out, kept) {
			t.Errorf("the debug log must keep %q, got:\n%s", kept, out)
		}
	}
}

// failingRoundTripper fails every request the way net/http does: with a
// *url.Error that prints the full request URL.
type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, &url.Error{Op: req.Method, URL: req.URL.String(), Err: errors.New("dial tcp: connection refused")}
}

func TestDebugTransport_TransportErrorLogMasksSignedLink(t *testing.T) {
	dt, logs := newDebugTransportForTest(failingRoundTripper{})
	req, err := http.NewRequest(http.MethodGet, "https://files.example.test/att-7/agenda.pdf?X-Amz-Signature="+debugTestSignature, nil)
	if err != nil {
		t.Fatal(err)
	}
	var urlErr *url.Error
	if _, err := dt.RoundTrip(req); !errors.As(err, &urlErr) {
		t.Fatalf("the transport error must be returned unchanged, got %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "outbound request failed") {
		t.Errorf("the transport failure must be logged, got:\n%s", out)
	}
	if strings.Contains(out, debugTestSignature) || !strings.Contains(out, "X-Amz-Signature=[REDACTED]") {
		t.Errorf("the logged error must have its signature masked, got:\n%s", out)
	}
}

// Below DEBUG the transport skips dumping and masking. That saving cannot be
// seen in the log, since slog drops DEBUG records either way; what this test
// pins is that the round trip is unchanged and failures are still logged,
// masked, at that level.
func TestDebugTransport_InfoLevelSkipsDumpsButLogsFailures(t *testing.T) {
	var logs bytes.Buffer
	info := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ok := &debugTransport{transport: &stubRoundTripper{resp: &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"uid":"m1"}`)),
	}}, logger: info}
	req, err := http.NewRequest(http.MethodGet, "https://api.example.test/meetings/m1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ok.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if body, _ := io.ReadAll(resp.Body); string(body) != `{"uid":"m1"}` {
		t.Errorf("the caller must get the body unchanged, got %q", body)
	}
	if logs.Len() != 0 {
		t.Errorf("no dump may be logged below DEBUG, got:\n%s", logs.String())
	}

	failing := &debugTransport{transport: failingRoundTripper{}, logger: info}
	req, err = http.NewRequest(http.MethodGet, "https://files.example.test/a.pdf?X-Amz-Signature="+debugTestSignature, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.RoundTrip(req); err == nil {
		t.Fatal("the transport error must be returned")
	}
	out := logs.String()
	if !strings.Contains(out, "outbound request failed") || strings.Contains(out, debugTestSignature) {
		t.Errorf("a failure must still be logged, masked, below DEBUG, got:\n%s", out)
	}
}

// TestDebugTransport_MasksSecretsSplitAcrossChunks pins that a chunked response is masked
// even when the upstream splits a secret across chunks: the transport reads
// the whole body before dumping it, so the dump carries one chunk and no
// chunk-size line can fall inside a masked key or value.
func TestDebugTransport_MasksSecretsSplitAcrossChunks(t *testing.T) {
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
	client := newDebugTransportClient(&http.Client{}, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
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
