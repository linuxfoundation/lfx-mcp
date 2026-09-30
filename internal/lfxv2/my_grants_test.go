// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client utilities for interacting with LFX v2 APIs, including OAuth2 token exchange.
package lfxv2

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMyGrants(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       []MyGrant
		wantError  bool
	}{
		{"valid and malformed", `{"grants":["project:b#writer@user:alice","project:a#auditor@user:auth0|alice","project:c#owner@user:alice@example.org","committee:d#member@user:alice","bad","project:a#writer@team:staff#member","project:a#viewer@user:*"]}`, []MyGrant{{"project", "b", "writer"}, {"project", "a", "auditor"}, {"project", "c", "owner"}}, false},
		{"empty", `{"grants":[]}`, []MyGrant{}, false},
		{"null grants", `{"grants":null}`, []MyGrant{}, false},
		{"invalid json", `{`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/my-grants" || r.URL.Query().Get("v") != "1" || r.URL.Query().Get("object_type") != "project" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("caller token not forwarded")
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			got, err := NewAccessCheckClient(server.URL+"/", server.Client()).MyGrants(context.Background(), "test-token", "project")
			if (err != nil) != tc.wantError || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, %v; want %#v, error=%v", got, err, tc.want, tc.wantError)
			}
		})
	}
}

func TestMyGrants_URLTypeEscaping(t *testing.T) {
	const typ = "project&object_type=committee +/"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query(); got.Get("object_type") != typ || len(got["object_type"]) != 1 || len(got) != 2 {
			t.Errorf("query was not escaped: %v", got)
		}
		_, _ = w.Write([]byte(`{"grants":[]}`))
	}))
	defer server.Close()
	if _, err := NewAccessCheckClient(server.URL, server.Client()).MyGrants(context.Background(), "token", typ); err != nil {
		t.Fatal(err)
	}
}

func TestMyGrants_UpstreamStatus(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("upstream detail"))
			}))
			defer server.Close()
			_, err := NewAccessCheckClient(server.URL, server.Client()).MyGrants(context.Background(), "token", "project")
			if err == nil || !strings.Contains(err.Error(), "upstream detail") {
				t.Fatalf("missing diagnostic: %v", err)
			}
			want := 0
			if status == 401 || status == 403 || status == 404 {
				want = status
			}
			for _, e := range []error{err, fmt.Errorf("wrapped: %w", err)} {
				if got := UpstreamStatus(e); got != want {
					t.Errorf("status=%d, want %d", got, want)
				}
				if IsRefusalWithoutServiceMessage(e) != (status == 401 || status == 403) {
					t.Errorf("wrong refusal classification: %v", e)
				}
			}
		})
	}
}

func TestMyGrants_BoundsErrorDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(strings.Repeat("x", 8192)))
	}))
	defer server.Close()
	_, err := NewAccessCheckClient(server.URL, server.Client()).MyGrants(context.Background(), "token", "project")
	if err == nil || !strings.HasSuffix(err.Error(), " [truncated]") || strings.Count(err.Error(), "x") != 4096 {
		t.Fatalf("error diagnostic not bounded: %v", err)
	}
}

func TestParseMyGrant_RejectsMalformed(t *testing.T) {
	for _, tuple := range []string{
		"", "project", "project:a", "project:a#writer", "project:a#writer@user:",
		"project:#writer@user:a", "project:a#@user:a", "project:a#writer#owner@user:a",
		"project:a#writer@team:staff#member", "project:a#viewer@user:*", "project:a#writer@user:a#member",
		"project:a:b#writer@user:a", "project:a#writer@user:a\ttrue", "project:a b#writer@user:a",
		"committee:a#member@user:a",
	} {
		if grant, ok := parseMyGrant(tuple, "project"); ok {
			t.Errorf("accepted malformed tuple %q: %+v", tuple, grant)
		}
	}
}
