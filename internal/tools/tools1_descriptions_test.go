// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestTools1Descriptions_FunctionVisibilityAndBudget checks the served tool
// and parameter descriptions in both terminology modes. Bytes beyond the
// shared schema budget are silently invisible to clients.
func TestTools1Descriptions_FunctionVisibilityAndBudget(t *testing.T) {
	preferRoute := regexp.MustCompile(`(?i)prefer[^.!?]*(semantic layer|query_lfx_lens)`)
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"count_lfx_resources", RegisterCountLFXResources},
		{"search_meetings", func(s *mcp.Server) { RegisterSearchMeetings(s, false) }},
		{"search_meetings", func(s *mcp.Server) { RegisterSearchMeetings(s, true) }},
		{"search_past_meetings", func(s *mcp.Server) { RegisterSearchPastMeetings(s, false) }},
		{"search_past_meetings", func(s *mcp.Server) { RegisterSearchPastMeetings(s, true) }},
		{"search_committees", func(s *mcp.Server) { RegisterSearchCommittees(s, false) }},
		{"search_groups", func(s *mcp.Server) { RegisterSearchCommittees(s, true) }},
		{"search_past_meeting_participants", func(s *mcp.Server) { RegisterSearchPastMeetingParticipants(s, false) }},
		{"search_past_meeting_participants", func(s *mcp.Server) { RegisterSearchPastMeetingParticipants(s, true) }},
		{"search_meeting_attachments", RegisterSearchMeetingAttachments},
		{"get_org_committee_seats", RegisterGetOrgCommitteeSeats},
		{"audit_committee_coverage", RegisterAuditCommitteeCoverage},
		{"search_projects", RegisterSearchProjects},
		{"get_membership_key_contacts", RegisterGetMembershipKeyContacts},
		{"search_committee_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, false) }},
		{"search_group_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := listRegisteredTool(t, tc.name, tc.register)
			if n := len(tool.Description); n > schemaDescriptionBudget {
				t.Errorf("%s description is %d bytes; budget is %d", tc.name, n, schemaDescriptionBudget)
			}
			descriptions := []string{tool.Description}
			for _, property := range schemaProperties(t, tool) {
				descriptions = append(descriptions, schemaPropertyDescription(t, tool, property))
			}
			for _, description := range descriptions {
				if preferRoute.MatchString(description) || strings.Contains(description, "live here, not in the semantic layer") {
					t.Errorf("%s must describe its function and visibility, not prefer-routing: %q", tc.name, description)
				}
			}
			if tc.name == "search_past_meeting_participants" {
				if got := schemaPropertyDescription(t, tool, "count_only"); !strings.Contains(got, "participant records (not people or attendances)") {
					t.Errorf("count_only must distinguish records from people and attendances: %q", got)
				}
				// The count mode's result shape, warnings included (count caveats
				// are warnings, not part of the note).
				if want := "count_only returns the record count with complete, visibility and warnings."; !strings.Contains(tool.Description, want) {
					t.Errorf("description must state the count_only result shape %q", want)
				}
			}
			if tc.name == "count_lfx_resources" {
				for _, want := range []string{"parent (the type's own ref)", "filters_all / filters_or on data fields.", "group_by=<tag prefix> returns groups [{key, count}] with groups_complete", "metric=cardinality:<tag prefix> returns metric_value with metric_complete"} {
					if !strings.Contains(tool.Description, want) {
						t.Errorf("count description missing %q", want)
					}
				}
				if strings.Contains(tool.Description, "type lacks counts 0") {
					t.Error("count description must not promise zero for unsupported filters")
				}
				// The per-type parent refs, the attachment types' included, live
				// in the parent parameter's text.
				const parent = "Parent ref: committee:<uid> for committee_member, project:<uid> for committee, meeting:<id> for v1_meeting_attachment, past_meeting:<meeting_and_occurrence_id> for v1_past_meeting_participant and v1_past_meeting_attachment"
				if got := schemaPropertyDescription(t, tool, "parent"); got != parent {
					t.Errorf("parent must name the type-specific ref: %q", got)
				}
			}
		})
	}
}
