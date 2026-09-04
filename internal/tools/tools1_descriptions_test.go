// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestTools1Descriptions_FitSchemaBudget guards the four TOOLS-1 tool
// descriptions against the 2048-byte cap that clients enforce silently
// (schemaDescriptionBudget, shared with the lens tools' test). Bytes past
// the cap are invisible to the model.
func TestTools1Descriptions_FitSchemaBudget(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"count_lfx_resources", RegisterCountLFXResources},
		{"search_past_meeting_participants", RegisterSearchPastMeetingParticipants},
		{"get_org_committee_seats", RegisterGetOrgCommitteeSeats},
		{"search_projects", RegisterSearchProjects},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := listRegisteredTool(t, tc.name, tc.register)
			if n := len(tool.Description); n > schemaDescriptionBudget {
				t.Errorf("%s description is %d bytes; budget is %d", tc.name, n, schemaDescriptionBudget)
			}
		})
	}
}
