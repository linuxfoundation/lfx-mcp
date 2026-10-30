// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"bytes"
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
