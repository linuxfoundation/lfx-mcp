// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCommitteeToolsClaimGovernanceRosters guards the routing contract from
// the committee side. The semantic-layer and lens descriptions redirect
// governance-roster questions here; these descriptions must claim that
// ownership in both terminology modes, or an agent arriving from the
// redirect finds a tool that does not say it is the right one. The eval's
// board/ambassador questions were answered "unavailable" or fabricated when
// no surface named the committee tools as the roster source.
func TestCommitteeToolsClaimGovernanceRosters(t *testing.T) {
	for _, tc := range []struct {
		toolName string
		asGroups bool
		register func(*mcp.Server)
		wants    []string
	}{
		{
			toolName: "search_committees",
			register: func(s *mcp.Server) { RegisterSearchCommittees(s, false) },
			wants: []string{
				"system of record for governance bodies",
				"boards, TOCs/TACs, working groups, ambassador programs",
				"Prefer this over the semantic layer or query_lfx_lens",
			},
		},
		{
			toolName: "search_groups",
			register: func(s *mcp.Server) { RegisterSearchCommittees(s, true) },
			wants: []string{
				"system of record for governance bodies",
				"Prefer this over the semantic layer or query_lfx_lens",
			},
		},
		{
			toolName: "search_committee_members",
			register: func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, false) },
			wants: []string{
				"authoritative roster source",
				"paginate until page_token is absent",
				"no country",
			},
		},
		{
			toolName: "search_group_members",
			register: func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, true) },
			wants: []string{
				"authoritative roster source",
				"paginate until page_token is absent",
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
		})
	}
}
