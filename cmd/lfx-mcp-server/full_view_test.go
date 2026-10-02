// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package main provides the LFX MCP server binary with support for stdio and HTTP transports.
package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	lfxauth "github.com/linuxfoundation/lfx-mcp/internal/auth"
	"github.com/linuxfoundation/lfx-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// probeFullView connects to a newServer built for token over the in-memory
// transport, with one extra probe tool registered that reports the full-view
// flag the receiving middleware put on the handler's context. It returns that
// flag and the tools/list the caller sees.
func probeFullView(t *testing.T, token *auth.TokenInfo) (fullView bool, listed map[string]bool) {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	server := newServer(Config{Tools: staffOnlyTools}, "test", token)
	type probeOut struct {
		FullView bool `json:"full_view"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "probe_full_view"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, probeOut, error) {
		return nil, probeOut{FullView: tools.HasFullView(ctx)}, nil
	})

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

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "probe_full_view", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("unexpected structured content %T", res.StructuredContent)
	}
	fullView, _ = out["full_view"].(bool)

	list, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	listed = make(map[string]bool, len(list.Tools))
	for _, tool := range list.Tools {
		listed[tool.Name] = true
	}
	return fullView, listed
}

// TestNewServer_FullViewMiddlewareAgreesWithIsFullViewCaller pins D1/D2: the
// flag handlers read from the context is tools.IsFullViewCaller of the same
// token that drove registration, for every token kind; and the staff-only
// Lens tools follow tools.IsStaffCaller, so an API-key caller has the full
// view of people data without gaining the Lens tools.
func TestNewServer_FullViewMiddlewareAgreesWithIsFullViewCaller(t *testing.T) {
	cases := []struct {
		name  string
		token *auth.TokenInfo
	}{
		{"nil token", nil},
		{"plain reader", &auth.TokenInfo{Scopes: []string{tools.ScopeRead}, Extra: map[string]any{"raw_token": "x"}}},
		{"lf_staff", &auth.TokenInfo{Scopes: []string{tools.ScopeRead}, Extra: map[string]any{tools.ClaimLFStaff: true}}},
		{"lf_staff string", &auth.TokenInfo{Scopes: []string{tools.ScopeRead}, Extra: map[string]any{tools.ClaimLFStaff: "true"}}},
		{"machine", &auth.TokenInfo{Scopes: []string{tools.ScopeRead}, Extra: map[string]any{lfxauth.MachineAccountExtraKey: true}}},
		{"api key", &auth.TokenInfo{Scopes: []string{tools.ScopeRead, tools.ScopeManage}, Extra: map[string]any{lfxauth.APIKeyAuthExtraKey: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fullView, listed := probeFullView(t, tc.token)
			if want := tools.IsFullViewCaller(tc.token); fullView != want {
				t.Errorf("middleware flag = %v, IsFullViewCaller = %v", fullView, want)
			}
			wantStaff := tools.IsStaffCaller(tc.token)
			for _, name := range staffOnlyTools {
				if listed[name] != wantStaff {
					t.Errorf("%s listed = %v, IsStaffCaller = %v", name, listed[name], wantStaff)
				}
			}
		})
	}
}

// TestNewServer_APIKeyIsFullViewButNotStaff states the one asymmetry in
// words, so a change to either helper that collapses them is caught.
func TestNewServer_APIKeyIsFullViewButNotStaff(t *testing.T) {
	apiKey := &auth.TokenInfo{Scopes: []string{tools.ScopeRead, tools.ScopeManage}, Extra: map[string]any{lfxauth.APIKeyAuthExtraKey: true}}
	fullView, listed := probeFullView(t, apiKey)
	if !fullView {
		t.Error("an API-key caller must have the full view of people data")
	}
	for _, name := range staffOnlyTools {
		if listed[name] {
			t.Errorf("%s must stay absent for an API-key caller", name)
		}
	}
}
