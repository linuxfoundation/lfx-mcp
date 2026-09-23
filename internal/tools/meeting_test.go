// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMeetingToolsClaimMeetingQuestions guards the routing contract from the
// meeting side. The semantic layer has no meeting metrics and lens is not
// the meeting lane, so the meeting tools must claim ownership in both
// terminology modes - otherwise a meeting question bounces between tools
// that each disclaim it. The existing events redirect (meetings tool ->
// semantic layer for conferences/registrations) must survive alongside the
// ownership claim: the two route in opposite directions on purpose. Events
// are standard metrics, so the redirect names the served standard-metrics
// tools, not unserved semantic-layer discovery actions.
func TestMeetingToolsClaimMeetingQuestions(t *testing.T) {
	for _, tc := range []struct {
		toolName string
		register func(*mcp.Server)
		wants    []string
	}{
		{
			toolName: "search_meetings",
			register: func(s *mcp.Server) { RegisterSearchMeetings(s, false) },
			wants: []string{
				"live HERE - prefer these tools over the semantic layer or query_lfx_lens",
				// The opposite-direction redirect applies only when available.
				"when query_lfx_standard_metrics is available to you, read read_lfx_standard_metrics_guidance and use it",
			},
		},
		{
			toolName: "search_meetings",
			register: func(s *mcp.Server) { RegisterSearchMeetings(s, true) },
			wants: []string{
				"live HERE - prefer these tools over the semantic layer or query_lfx_lens",
				"when query_lfx_standard_metrics is available to you, read read_lfx_standard_metrics_guidance and use it",
			},
		},
		{
			toolName: "search_past_meetings",
			register: func(s *mcp.Server) { RegisterSearchPastMeetings(s, false) },
			wants: []string{
				"live here, not in the semantic layer or query_lfx_lens",
			},
		},
		{
			toolName: "search_past_meetings",
			register: func(s *mcp.Server) { RegisterSearchPastMeetings(s, true) },
			wants: []string{
				"live here, not in the semantic layer or query_lfx_lens",
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

// TestSearchMeetingsRedirectNamesServedToolsOnly pins that the events
// redirect on search_meetings (both modes) names only tools the server
// serves; list_metrics, get_dimension_values and help('doctrine') are not
// served tool names and query_lfx_semantic_layer is not the events lane.
func TestSearchMeetingsRedirectNamesServedToolsOnly(t *testing.T) {
	for _, asGroups := range []bool{false, true} {
		tool := listRegisteredTool(t, "search_meetings", func(s *mcp.Server) { RegisterSearchMeetings(s, asGroups) })
		for _, banned := range []string{"list_metrics", "get_dimension_values", "help('doctrine')", "query_lfx_semantic_layer (preferred)"} {
			if strings.Contains(tool.Description, banned) {
				t.Errorf("asGroups=%v: description still names %q", asGroups, banned)
			}
		}
		if n := len(tool.Description); n > schemaDescriptionBudget {
			t.Errorf("asGroups=%v: description is %d bytes, budget %d", asGroups, n, schemaDescriptionBudget)
		}
	}
}
