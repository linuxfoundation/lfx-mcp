// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// capturingRoundTripper records the request it receives (as seen by the
// innermost transport, i.e. after all wrapping RoundTrippers have run) and
// answers with an empty 200 response.
type capturingRoundTripper struct {
	gotAuthHeader string
}

func (c *capturingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.gotAuthHeader = req.Header.Get("Authorization")
	return &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Body:       http.NoBody,
	}, nil
}

// TestTransportWrapOrder_DebugDumpDoesNotLeakStaticToken verifies that the
// debug transport wraps outside the auth interceptor: the wire dump it logs
// must reflect the request as it existed *before* the auth interceptor
// injected the Authorization header, while the request actually sent
// upstream must carry that header. This mirrors the wrap order applied by
// NewClients.
func TestTransportWrapOrder_DebugDumpDoesNotLeakStaticToken(t *testing.T) {
	upstream := &capturingRoundTripper{}
	clients := &Clients{staticLFXToken: "super-secret-token"}

	httpClient := &http.Client{Transport: upstream}
	httpClient = clients.wrapWithAuthInterceptor(httpClient)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	httpClient = newDebugTransportClient(httpClient, logger)

	req, err := http.NewRequest(http.MethodGet, "https://api.example.test/resource", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := httpClient.Do(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if upstream.gotAuthHeader != "Bearer super-secret-token" {
		t.Errorf("the request actually sent upstream must carry the static token: got Authorization=%q", upstream.gotAuthHeader)
	}
	if strings.Contains(logs.String(), "super-secret-token") || strings.Contains(logs.String(), "Authorization") {
		t.Errorf("the debug wire dump must not include the Authorization header or the bearer token, got logs:\n%s", logs.String())
	}
}
