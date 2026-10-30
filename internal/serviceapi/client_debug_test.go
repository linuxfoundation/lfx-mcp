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
