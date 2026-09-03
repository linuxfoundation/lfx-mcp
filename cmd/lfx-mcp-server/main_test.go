// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSplitTrimmed(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "empty string is unset",
			input: "",
			want:  []string{},
		},
		{
			name:  "single value",
			input: "hello_world",
			want:  []string{"hello_world"},
		},
		{
			name:  "multiple values",
			input: "a,b,c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "values with surrounding whitespace",
			input: "a, b ,  c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:    "lone comma is malformed",
			input:   ",",
			wantErr: true,
		},
		{
			name:    "leading comma is malformed",
			input:   ",a,b",
			wantErr: true,
		},
		{
			name:    "trailing comma is malformed",
			input:   "a,b,",
			wantErr: true,
		},
		{
			name:    "embedded empty entry is malformed",
			input:   "a,,b",
			wantErr: true,
		},
		{
			name:    "whitespace-only entry is malformed",
			input:   "a, ,b",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitTrimmed(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitTrimmed(%q) = %#v, nil, want an error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitTrimmed(%q) returned unexpected error: %v", tt.input, err)
			}
			if got == nil {
				t.Fatal("splitTrimmed returned nil, want non-nil slice")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitTrimmed(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

// staffOnlyTools are the lens-backed tools and the guidance that documents
// them: they read cross-project warehouse data, so newServer registers them
// for LF staff only, exactly alike — a tool this list grows by is gated the
// day it is added, not when someone notices.
var staffOnlyTools = []string{
	"query_lfx_lens",
	"explore_lfx_semantic_layer",
	"query_lfx_semantic_layer",
	"query_lfx_standard_metrics",
	"read_lfx_semantic_layer_guidance",
	"read_lfx_standard_metrics_guidance",
	"read_lfx_deck_building_guidance",
}

// listedTools is the tools/list a caller holding token sees from a server
// with every name in staffOnlyTools enabled.
func listedTools(t *testing.T, token *auth.TokenInfo) map[string]bool {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	server := newServer(Config{Tools: staffOnlyTools}, "test", token)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect failed: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	res, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	listed := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		listed[tool.Name] = true
	}
	return listed
}

// TestNewServer_LensToolsAreStaffOnly pins the gate: a read-scoped caller who
// is not LF staff sees none of the lens-backed tools or their guidance, and a
// read-scoped staff caller sees all of them.
func TestNewServer_LensToolsAreStaffOnly(t *testing.T) {
	reader := &auth.TokenInfo{Scopes: []string{tools.ScopeRead}}
	staff := &auth.TokenInfo{
		Scopes: []string{tools.ScopeRead},
		Extra:  map[string]any{tools.ClaimLFStaff: true},
	}

	forReader := listedTools(t, reader)
	forStaff := listedTools(t, staff)
	for _, name := range staffOnlyTools {
		if forReader[name] {
			t.Errorf("%s is listed for a non-staff reader; it must be staff-only", name)
		}
		if !forStaff[name] {
			t.Errorf("%s is not listed for a staff reader", name)
		}
	}
}
