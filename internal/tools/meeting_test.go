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
// ownership claim: the two route in opposite directions on purpose.
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
				// The opposite-direction redirect for event data must survive.
				"use query_lfx_semantic_layer (preferred)",
			},
		},
		{
			toolName: "search_meetings",
			register: func(s *mcp.Server) { RegisterSearchMeetings(s, true) },
			wants: []string{
				"live HERE - prefer these tools over the semantic layer or query_lfx_lens",
				"use query_lfx_semantic_layer (preferred)",
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
