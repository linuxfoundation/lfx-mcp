// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package serviceapi

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestNewClient_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "missing BaseURL",
			cfg:     Config{APIKey: "key"},
			wantErr: true,
		},
		{
			name:    "missing APIKey",
			cfg:     Config{BaseURL: "https://example.com"},
			wantErr: true,
		},
		{
			name: "valid config",
			cfg:  Config{BaseURL: "https://example.com", APIKey: "key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGet_APIKeyHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "ApiKey test-key" {
			t.Errorf("expected 'ApiKey test-key', got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	body, status, err := client.Get(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("expected 200, got %d", status)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("unexpected body: %s", string(body))
	}
}

func TestGet_QueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "pending" {
			t.Errorf("expected status=pending, got %q", r.URL.Query().Get("status"))
		}
		if r.URL.Query().Get("page") != "2" {
			t.Errorf("expected page=2, got %q", r.URL.Query().Get("page"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`ok`))
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, APIKey: "key", HTTPClient: server.Client()})

	query := url.Values{"status": {"pending"}, "page": {"2"}}
	_, status, err := client.Get(context.Background(), "/items", query)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("expected 200, got %d", status)
	}
}

func TestPostJSON(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "ApiKey json-key" {
			t.Errorf("expected 'ApiKey json-key', got %q", r.Header.Get("Authorization"))
		}

		var p payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if p.Name != "test" || p.Count != 42 {
			t.Errorf("unexpected payload: %+v", p)
		}

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"new-123"}`))
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, APIKey: "json-key", HTTPClient: server.Client()})

	body, status, err := client.PostJSON(context.Background(), "/items", payload{Name: "test", Count: 42})
	if err != nil {
		t.Fatalf("PostJSON failed: %v", err)
	}
	if status != http.StatusCreated {
		t.Errorf("expected 201, got %d", status)
	}
	if string(body) != `{"id":"new-123"}` {
		t.Errorf("unexpected body: %s", string(body))
	}
}

func TestPostMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify content type is multipart/form-data.
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("failed to parse content type: %v", err)
		}
		if mediaType != "multipart/form-data" {
			t.Errorf("expected multipart/form-data, got %q", mediaType)
		}

		// Verify API key is present.
		if r.Header.Get("Authorization") != "ApiKey mp-key" {
			t.Errorf("expected 'ApiKey mp-key', got %q", r.Header.Get("Authorization"))
		}

		// Parse multipart form.
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("failed to parse multipart form: %v", err)
		}
		if r.FormValue("message") != "onboard Google to pytorch" {
			t.Errorf("unexpected message: %q", r.FormValue("message"))
		}
		if r.FormValue("stream") != "false" {
			t.Errorf("unexpected stream: %q", r.FormValue("stream"))
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"run_id":"run-456"}`))
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, APIKey: "mp-key", HTTPClient: server.Client()})

	fields := map[string]string{
		"message": "onboard Google to pytorch",
		"stream":  "false",
	}
	body, status, err := client.PostMultipart(context.Background(), "/agents/preview/runs", fields)
	if err != nil {
		t.Fatalf("PostMultipart failed: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("expected 200, got %d", status)
	}
	if string(body) != `{"run_id":"run-456"}` {
		t.Errorf("unexpected body: %s", string(body))
	}
}

func TestGet_NonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, APIKey: "key", HTTPClient: server.Client()})

	body, status, err := client.Get(context.Background(), "/missing", nil)
	if err != nil {
		t.Fatalf("Get should not return error for non-2xx status: %v", err)
	}
	if status != http.StatusNotFound {
		t.Errorf("expected 404, got %d", status)
	}
	if string(body) != `{"error":"not found"}` {
		t.Errorf("unexpected body: %s", string(body))
	}
}

func TestGet_TrailingSlashBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should NOT have double slash.
		if r.URL.Path != "/api/test" {
			t.Errorf("expected /api/test, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL + "/api/", APIKey: "key", HTTPClient: server.Client()})

	_, _, err := client.Get(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
}

func TestPostJSON_EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "{}" {
			t.Errorf("expected empty JSON object, got %q", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient(Config{BaseURL: server.URL, APIKey: "key", HTTPClient: server.Client()})

	_, _, err := client.PostJSON(context.Background(), "/test", struct{}{})
	if err != nil {
		t.Fatalf("PostJSON failed: %v", err)
	}
}
