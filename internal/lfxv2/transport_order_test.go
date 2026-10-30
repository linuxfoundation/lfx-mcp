// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewClients_DebugDumpDoesNotLeakStaticToken verifies that NewClients
// wraps the debug transport outside the auth interceptor: the wire dump it
// logs must reflect the request as it existed *before* the auth interceptor
// injected the Authorization header, while the request actually sent
// upstream must carry that header. Unlike a hand-assembled wrapper chain,
// this drives a real request through the clients returned by NewClients, so
// it fails if the constructor's wrap order regresses.
func TestNewClients_DebugDumpDoesNotLeakStaticToken(t *testing.T) {
	var gotAuthHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`"b2s="`))
	}))
	defer upstream.Close()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	clients, err := NewClients(context.Background(), ClientConfig{
		APIDomain:      upstream.URL,
		StaticLFXToken: "super-secret-token",
		DebugLogger:    logger,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := clients.Committee.Readyz(context.Background()); err != nil {
		t.Fatalf("unexpected error calling Readyz: %v", err)
	}

	if gotAuthHeader != "Bearer super-secret-token" {
		t.Errorf("the request actually sent upstream must carry the static token: got Authorization=%q", gotAuthHeader)
	}
	if strings.Contains(logs.String(), "super-secret-token") || strings.Contains(logs.String(), "Authorization") {
		t.Errorf("the debug wire dump must not include the Authorization header or the bearer token, got logs:\n%s", logs.String())
	}
}
