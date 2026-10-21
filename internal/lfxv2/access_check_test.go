// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckAccess_Allow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/access-check" {
			t.Errorf("expected /access-check, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("v") != "1" {
			t.Errorf("expected v=1 query param, got %s", r.URL.Query().Get("v"))
		}
		if r.Header.Get("Authorization") != "Bearer test-v2-token" {
			t.Errorf("expected Bearer test-v2-token, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Verify request body.
		var req accessCheckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if len(req.Requests) != 1 || req.Requests[0] != "project:abc-123#writer" {
			t.Errorf("unexpected requests: %v", req.Requests)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessCheckResponse{ //nolint:errcheck // Test handler write errors are not actionable.
			Results: []string{"project:abc-123#writer@user:testuser\ttrue"},
		})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	results, err := client.CheckAccess(context.Background(), "test-v2-token", []string{"project:abc-123#writer"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	allowed, ok := results["project:abc-123#writer"]
	if !ok {
		t.Fatal("expected result for project:abc-123#writer")
	}
	if !allowed {
		t.Error("expected allowed=true")
	}
}

func TestCheckAccess_Deny(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessCheckResponse{ //nolint:errcheck // Test handler write errors are not actionable.
			Results: []string{"project:abc-123#writer@user:testuser\tfalse"},
		})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	results, err := client.CheckAccess(context.Background(), "token", []string{"project:abc-123#writer"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	allowed, ok := results["project:abc-123#writer"]
	if !ok {
		t.Fatal("expected result for project:abc-123#writer")
	}
	if allowed {
		t.Error("expected allowed=false")
	}
}

func TestCheckAccess_Batch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req accessCheckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if len(req.Requests) != 3 {
			t.Errorf("expected 3 requests, got %d", len(req.Requests))
		}

		// Return results in a different order than requested (as the real API does).
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessCheckResponse{ //nolint:errcheck // Test handler write errors are not actionable.
			Results: []string{
				"project:bbb#auditor@user:testuser\tfalse",
				"project:aaa#writer@user:testuser\ttrue",
				"project:ccc#writer@user:testuser\ttrue",
			},
		})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	results, err := client.CheckAccess(context.Background(), "token", []string{
		"project:aaa#writer",
		"project:bbb#auditor",
		"project:ccc#writer",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if !results["project:aaa#writer"] {
		t.Error("expected project:aaa#writer to be allowed")
	}
	if results["project:bbb#auditor"] {
		t.Error("expected project:bbb#auditor to be denied")
	}
	if !results["project:ccc#writer"] {
		t.Error("expected project:ccc#writer to be allowed")
	}
}

func TestCheckAccess_HashSeparator(t *testing.T) {
	// Verify the # separator is used (not :) per Eric's clarification.
	var capturedBody accessCheckRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessCheckResponse{ //nolint:errcheck // Test handler write errors are not actionable.
			Results: []string{"project:my-uuid#writer@user:testuser\ttrue"},
		})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	_ = client.CheckProjectAccess(context.Background(), "token", "my-uuid", "writer") //nolint:errcheck // Return value intentionally ignored; test checks side effects.

	if len(capturedBody.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(capturedBody.Requests))
	}
	expected := "project:my-uuid#writer"
	if capturedBody.Requests[0] != expected {
		t.Errorf("expected %q, got %q", expected, capturedBody.Requests[0])
	}
}

func TestCheckAccess_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`)) //nolint:errcheck // Test handler write errors are not actionable.
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	_, err := client.CheckAccess(context.Background(), "bad-token", []string{"project:abc#writer"})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestCheckAccess_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`)) //nolint:errcheck // Test handler write errors are not actionable.
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	_, err := client.CheckAccess(context.Background(), "token", []string{"project:abc#writer"})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestCheckAccess_ResultCountMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return 1 result for 2 requests.
		_ = json.NewEncoder(w).Encode(accessCheckResponse{ //nolint:errcheck // Test handler write errors are not actionable.
			Results: []string{"project:a#writer@user:testuser\ttrue"},
		})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	_, err := client.CheckAccess(context.Background(), "token", []string{"project:a#writer", "project:b#writer"})
	if err == nil {
		t.Fatal("expected error for result count mismatch")
	}
}

func TestCheckProjectAccess_Allow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessCheckResponse{ //nolint:errcheck // Test handler write errors are not actionable.
			Results: []string{"project:uuid-123#writer@user:testuser\ttrue"},
		})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	err := client.CheckProjectAccess(context.Background(), "token", "uuid-123", "writer")
	if err != nil {
		t.Fatalf("expected nil error for allowed access, got: %v", err)
	}
}

func TestCheckProjectAccess_Deny(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessCheckResponse{ //nolint:errcheck // Test handler write errors are not actionable.
			Results: []string{"project:uuid-123#writer@user:testuser\tfalse"},
		})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	err := client.CheckProjectAccess(context.Background(), "token", "uuid-123", "writer")
	if err == nil {
		t.Fatal("expected error for denied access")
	}
}

func TestParseAccessResult(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantReq string
		wantOK  bool
		wantErr bool
	}{
		{
			name:    "allowed",
			input:   "project:abc-123#writer@user:alice\ttrue",
			wantReq: "project:abc-123#writer",
			wantOK:  true,
		},
		{
			name:    "denied",
			input:   "project:abc-123#owner@user:alice\tfalse",
			wantReq: "project:abc-123#owner",
			wantOK:  false,
		},
		{
			name:    "no tab",
			input:   "project:abc-123#writer@user:alice",
			wantErr: true,
		},
		{
			name:    "no at sign",
			input:   "something\ttrue",
			wantErr: true,
		},
		{
			// A client-credentials principal is "<client_id>@clients": the
			// last "@" is inside the principal, so matching on the sent
			// request's prefix is the only reading that yields the request.
			name:    "principal containing an at sign",
			input:   "project:abc-123#writer@user:client-id@clients\ttrue",
			wantReq: "project:abc-123#writer",
			wantOK:  true,
		},
		{
			name:    "result for a request that was not sent",
			input:   "project:other#writer@user:alice\ttrue",
			wantErr: true,
		}, {
			// The contract allows only true or false; any other status is a
			// failed check, never a denial.
			name:    "status neither true nor false",
			input:   "project:abc-123#writer@user:alice\tgarbage",
			wantErr: true,
		},
		{
			name:    "empty status",
			input:   "project:abc-123#writer@user:alice\t",
			wantErr: true,
		},
	}
	requests := []string{"project:abc-123#writer", "project:abc-123#owner"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, allowed, err := parseAccessResult(tt.input, requests)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req != tt.wantReq {
				t.Errorf("request = %q, want %q", req, tt.wantReq)
			}
			if allowed != tt.wantOK {
				t.Errorf("allowed = %v, want %v", allowed, tt.wantOK)
			}
		})
	}
}

func TestNewAccessCheckClient_TrailingSlash(t *testing.T) {
	// Verify trailing slash in apiURL is handled.
	client := NewAccessCheckClient("https://api.example.com/", nil)
	if client.apiURL != "https://api.example.com" {
		t.Errorf("expected trailing slash stripped, got %q", client.apiURL)
	}
}

// TestCheckRelations_DedupesChunksAndIsStrict pins the Clients-level helper:
// repeated requests are sent once, more than accessCheckBatchSize distinct
// requests go out in several POSTs, every request gets an answer, and the
// exchanged (static, here) token is the bearer.
func TestCheckRelations_DedupesChunksAndIsStrict(t *testing.T) {
	var batches [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer static-token-value" {
			t.Errorf("expected the LFX token as bearer, got %q", r.Header.Get("Authorization"))
		}
		var req accessCheckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		batches = append(batches, req.Requests)
		results := make([]string, 0, len(req.Requests))
		for _, q := range req.Requests {
			results = append(results, q+"@user:client-id@clients\t"+map[bool]string{true: "true", false: "false"}[q == "committee:c1#writer"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessCheckResponse{Results: results}) //nolint:errcheck // test handler
	}))
	defer server.Close()

	c := &Clients{staticLFXToken: "static-token-value", AccessCheck: NewAccessCheckClient(server.URL, server.Client())}
	requests := []string{"committee:c1#writer", "committee:c1#auditor", "committee:c1#writer"}
	for i := 0; i < accessCheckBatchSize; i++ {
		requests = append(requests, fmt.Sprintf("v1_meeting:m%d#organizer", i))
	}
	got, err := c.CheckRelations(context.Background(), requests)
	if err != nil {
		t.Fatalf("CheckRelations: %v", err)
	}
	if len(got) != accessCheckBatchSize+2 {
		t.Errorf("expected %d distinct answers, got %d", accessCheckBatchSize+2, len(got))
	}
	if !got["committee:c1#writer"] || got["committee:c1#auditor"] {
		t.Errorf("answers not mapped back to requests: %v %v", got["committee:c1#writer"], got["committee:c1#auditor"])
	}
	if len(batches) != 2 || len(batches[0]) != accessCheckBatchSize || len(batches[1]) != 2 {
		sizes := make([]int, len(batches))
		for i, b := range batches {
			sizes[i] = len(b)
		}
		t.Errorf("expected batches of [%d 2], got %v", accessCheckBatchSize, sizes)
	}
}

// TestCheckRelations_MissingAnswerIsAnError pins strictness: an answer the
// service left out is an error, never a silent false.
func TestCheckRelations_MissingAnswerIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// One answer for two requests: the count check and the per-request
		// lookup both have to notice.
		_ = json.NewEncoder(w).Encode(accessCheckResponse{Results: []string{"committee:c1#writer@user:u\ttrue", "committee:c1#writer@user:u\ttrue"}}) //nolint:errcheck // test handler
	}))
	defer server.Close()
	c := &Clients{staticLFXToken: "t", AccessCheck: NewAccessCheckClient(server.URL, server.Client())}
	if _, err := c.CheckRelations(context.Background(), []string{"committee:c1#writer", "committee:c1#auditor"}); err == nil {
		t.Fatal("expected an error when a request has no answer")
	}
}

// TestCheckRelations_EmptyIsNoCall pins that nothing is sent for no requests.
func TestCheckRelations_EmptyIsNoCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no request expected")
	}))
	defer server.Close()
	c := &Clients{staticLFXToken: "t", AccessCheck: NewAccessCheckClient(server.URL, server.Client())}
	got, err := c.CheckRelations(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("expected an empty answer and no error, got %v %v", got, err)
	}
}

// TestNewClients_AccessCheckBypassesDebugAndAuthWrappers pins the
// construction shape: the access-check client built by NewClients sends the
// token CheckRelations passes explicitly and nothing of its traffic reaches
// the debug logger, which would otherwise dump the bearer and the body.
func TestNewClients_AccessCheckBypassesDebugAndAuthWrappers(t *testing.T) {
	var logs bytes.Buffer
	debugLogger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer static-token-value" {
			t.Errorf("expected the explicit token as bearer, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessCheckResponse{Results: []string{"committee:c1#writer@user:u\ttrue"}}) //nolint:errcheck // test handler
	}))
	defer server.Close()

	clients, err := NewClients(context.Background(), ClientConfig{
		APIDomain:      server.URL,
		StaticLFXToken: "static-token-value",
		DebugLogger:    debugLogger,
	})
	if err != nil {
		t.Fatalf("NewClients: %v", err)
	}
	if clients.AccessCheck == nil {
		t.Fatal("NewClients must construct the AccessCheck client")
	}
	got, err := clients.CheckRelations(context.Background(), []string{"committee:c1#writer"})
	if err != nil || !got["committee:c1#writer"] {
		t.Fatalf("CheckRelations = %v, %v", got, err)
	}
	if logs.Len() != 0 {
		t.Errorf("access-check traffic must not reach the debug transport, got logs:\n%s", logs.String())
	}
}
