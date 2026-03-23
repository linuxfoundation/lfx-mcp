// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"context"
	"encoding/json"
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
		json.NewEncoder(w).Encode(accessCheckResponse{Results: []string{"allow"}})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	results, err := client.CheckAccess(context.Background(), "test-v2-token", []string{"project:abc-123#writer"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "allow" {
		t.Errorf("expected [allow], got %v", results)
	}
}

func TestCheckAccess_Deny(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(accessCheckResponse{Results: []string{"deny"}})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	results, err := client.CheckAccess(context.Background(), "token", []string{"project:abc-123#writer"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "deny" {
		t.Errorf("expected [deny], got %v", results)
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(accessCheckResponse{
			Results: []string{"allow", "deny", "allow"},
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
	if results[0] != "allow" || results[1] != "deny" || results[2] != "allow" {
		t.Errorf("unexpected results: %v", results)
	}
}

func TestCheckAccess_HashSeparator(t *testing.T) {
	// Verify the # separator is used (not :) per Eric's clarification.
	var capturedBody accessCheckRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(accessCheckResponse{Results: []string{"allow"}})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	client.CheckProjectAccess(context.Background(), "token", "my-uuid", "writer")

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
		w.Write([]byte(`{"error":"invalid token"}`))
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
		w.Write([]byte(`not json`))
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
		json.NewEncoder(w).Encode(accessCheckResponse{Results: []string{"allow"}})
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
		json.NewEncoder(w).Encode(accessCheckResponse{Results: []string{"allow"}})
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
		json.NewEncoder(w).Encode(accessCheckResponse{Results: []string{"deny"}})
	}))
	defer server.Close()

	client := NewAccessCheckClient(server.URL, server.Client())
	err := client.CheckProjectAccess(context.Background(), "token", "uuid-123", "writer")
	if err == nil {
		t.Fatal("expected error for denied access")
	}
}

func TestNewAccessCheckClient_TrailingSlash(t *testing.T) {
	// Verify trailing slash in apiURL is handled.
	client := NewAccessCheckClient("https://api.example.com/", nil)
	if client.apiURL != "https://api.example.com" {
		t.Errorf("expected trailing slash stripped, got %q", client.apiURL)
	}
}
