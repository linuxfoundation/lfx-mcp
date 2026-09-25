// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package main implements the LFX MCP server entry point.
package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registrationBranch matches an uncommented tool registration gate in
// newServer, e.g. `if enabledTools["get_meeting"] && canRead {`.
var registrationBranch = regexp.MustCompile(`^\s*if enabledTools\["([a-z0-9_]+)"\]`)

// hostCredentialTerms are the phrases that would mean a tool advertises
// meeting host keys or host credentials. They are matched against the
// lower-cased tools/list text with spaces, underscores and hyphens removed, so
// "host key", host_key, hostKey and HostKey all match "hostkey".
var hostCredentialTerms = []string{"hostkey", "hostcredential", "hostpin"}

// everyRegisteredToolName returns every tool name newServer has a
// registration branch for, read from main.go so that a tool added later is
// covered without editing this test.
func everyRegisteredToolName(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	seen := make(map[string]bool)
	var names []string
	for _, line := range strings.Split(string(src), "\n") {
		if m := registrationBranch.FindStringSubmatch(line); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	if len(names) == 0 {
		t.Fatal("found no tool registration branches in main.go; the guard would pass vacuously")
	}
	return names
}

// listAllToolsForStaff returns the full tools/list a staff caller holding
// both scopes sees from a server with the given tools enabled.
func listAllToolsForStaff(t *testing.T, cfg Config) []*mcp.Tool {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	staff := &auth.TokenInfo{
		Scopes: []string{tools.ScopeRead, tools.ScopeManage},
		Extra:  map[string]any{tools.ClaimLFStaff: true},
	}
	server := newServer(cfg, "test", staff)

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
	return res.Tools
}

// TestNewServer_NoHostCredentialsInToolsList pins that no tool, as a client sees it in
// tools/list, advertises meeting host keys or host credentials: not in its
// name, title, description, input schema or output schema. It runs with every
// tool enabled for a staff caller, so every registration branch is covered,
// in both committee and group terminology. A tool that starts offering host
// credentials needs a deliberate gating change in newServer and a reviewed
// update to this test.
func TestNewServer_NoHostCredentialsInToolsList(t *testing.T) {
	names := everyRegisteredToolName(t)
	for _, asGroups := range []bool{false, true} {
		listed := listAllToolsForStaff(t, Config{Tools: names, CommitteesAsGroups: asGroups})
		if len(listed) != len(names) {
			t.Errorf("committees_as_groups=%v: %d tools listed for %d registration branches; every branch must register for a staff caller",
				asGroups, len(listed), len(names))
		}
		for _, tool := range listed {
			raw, err := json.Marshal(tool)
			if err != nil {
				t.Fatalf("marshal %s: %v", tool.Name, err)
			}
			text := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(string(raw)))
			for _, term := range hostCredentialTerms {
				if strings.Contains(text, term) {
					t.Errorf("committees_as_groups=%v: tool %s mentions %q in tools/list; "+
						"no tool may advertise meeting host credentials without a deliberate gating change",
						asGroups, tool.Name, term)
				}
			}
		}
	}
}
