// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCommitteeToolsDescribeRosters pins the governance scope, caller
// visibility, counting route and recorded dates in both terminology modes.
func TestCommitteeToolsDescribeRosters(t *testing.T) {
	for _, tc := range []struct {
		toolName string
		register func(*mcp.Server)
		wants    []string
	}{
		{
			toolName: "search_committees",
			register: func(s *mcp.Server) { RegisterSearchCommittees(s, false) },
			wants: []string{
				"system of record for governance bodies",
				"boards, TOCs/TACs, working groups, ambassador programs",
				"Returns the committees visible to the caller, as in LFX Self Serve.",
			},
		},
		{
			toolName: "search_groups",
			register: func(s *mcp.Server) { RegisterSearchCommittees(s, true) },
			wants: []string{
				"system of record for governance bodies",
				"boards, TOCs/TACs, working groups, ambassador programs",
				"Returns the committees visible to the caller, as in LFX Self Serve.",
			},
		},
		{
			toolName: "search_committee_members",
			register: func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, false) },
			wants: []string{
				"The authoritative source for committee rosters.",
				"Count with count_lfx_resources; records carry organization, role, voting status, term dates if recorded, no country.",
			},
		},
		{
			toolName: "search_group_members",
			register: func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, true) },
			wants: []string{
				"The authoritative source for committee rosters.",
				"Count with count_lfx_resources; records carry organization, role, voting status, term dates if recorded, no country.",
			},
		},
	} {
		t.Run(tc.toolName, func(t *testing.T) {
			tool := listRegisteredTool(t, tc.toolName, tc.register)
			for _, want := range tc.wants {
				if !strings.Contains(tool.Description, want) {
					t.Errorf("%s description missing %q", tc.toolName, want)
				}
			}
			if strings.Contains(tool.Description, "For counts, paginate") {
				t.Error("a roster search must not recommend paging to count")
			}
		})
	}
}
