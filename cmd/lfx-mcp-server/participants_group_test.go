// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package main implements the LFX MCP server entry point.
package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewServer_PastMeetingParticipantsTerminology(t *testing.T) {
	for _, tc := range []struct {
		name     string
		asGroups bool
		want     string
		absent   string
	}{
		{"committees", false, "committee_uid", "group_uid"},
		{"groups", true, "group_uid", "committee_uid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if logger == nil {
				logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			}
			server := newServer(Config{
				Tools:              []string{"search_past_meeting_participants"},
				CommitteesAsGroups: tc.asGroups,
			}, "test", &auth.TokenInfo{Scopes: []string{tools.ScopeRead}})
			ctx := context.Background()
			clientTransport, serverTransport := mcp.NewInMemoryTransports()
			serverSession, err := server.Connect(ctx, serverTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = serverSession.Close() })
			client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
			session, err := client.Connect(ctx, clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Tools) != 1 {
				t.Fatalf("expected only the participant tool, got %d tools", len(result.Tools))
			}
			schema := result.Tools[0].InputSchema.(map[string]any)
			properties := schema["properties"].(map[string]any)
			if _, ok := properties[tc.want]; !ok {
				t.Errorf("committees_as_groups=%v must expose %s", tc.asGroups, tc.want)
			}
			if _, ok := properties[tc.absent]; ok {
				t.Errorf("committees_as_groups=%v must not expose %s", tc.asGroups, tc.absent)
			}
		})
	}
}
