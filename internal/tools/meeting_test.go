// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMeetingToolsDescribeCallerVisibility pins both terminology modes and
// preserves the availability-qualified events redirect. Meeting totals exist
// on the layer; these tools describe only the records the caller can see.
func TestMeetingToolsDescribeCallerVisibility(t *testing.T) {
	for _, tc := range []struct {
		toolName string
		register func(*mcp.Server)
		wants    []string
	}{
		{
			toolName: "search_meetings",
			register: func(s *mcp.Server) { RegisterSearchMeetings(s, false) },
			wants: []string{
				"Returns the meetings visible to the caller, as in LFX Self Serve.",
				// The events redirect applies only when available.
				"when query_lfx_standard_metrics is available to you, read read_lfx_standard_metrics_guidance and use it",
			},
		},
		{
			toolName: "search_meetings",
			register: func(s *mcp.Server) { RegisterSearchMeetings(s, true) },
			wants: []string{
				"Returns the meetings visible to the caller, as in LFX Self Serve.",
				"when query_lfx_standard_metrics is available to you, read read_lfx_standard_metrics_guidance and use it",
			},
		},
		{
			toolName: "search_past_meetings",
			register: func(s *mcp.Server) { RegisterSearchPastMeetings(s, false) },
			wants: []string{
				"Returns the past meetings visible to the caller.",
			},
		},
		{
			toolName: "search_past_meetings",
			register: func(s *mcp.Server) { RegisterSearchPastMeetings(s, true) },
			wants: []string{
				"Returns the past meetings visible to the caller.",
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

// TestPastMeetingSummaryDescriptionsPreferEditedContent pins that both
// summary tools tell the model which version to present: a summary record
// carries the generated text in content and, once edited, the edited text in
// edited_content. The rule has no required parameter to live on, so it must
// stay in the tool description.
func TestPastMeetingSummaryDescriptionsPreferEditedContent(t *testing.T) {
	for _, tc := range []struct {
		toolName string
		register func(*mcp.Server)
	}{
		{toolName: "search_past_meeting_summaries", register: RegisterSearchPastMeetingSummaries},
		{toolName: "get_past_meeting_summary", register: RegisterGetPastMeetingSummary},
	} {
		t.Run(tc.toolName, func(t *testing.T) {
			tool := listRegisteredTool(t, tc.toolName, tc.register)
			for _, want := range []string{"edited_content", "supersedes the generated content"} {
				if !strings.Contains(tool.Description, want) {
					t.Errorf("description missing %q", want)
				}
			}
			if strings.Contains(tool.Description, "Insights") {
				t.Error("description must not mention Insights")
			}
			if n := len(tool.Description); n > schemaDescriptionBudget {
				t.Errorf("description is %d bytes, budget %d", n, schemaDescriptionBudget)
			}
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Error("summary tool must stay read-only")
			}
		})
	}
}
