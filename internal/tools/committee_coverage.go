// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// coverageGroupBySize asks the query service for as many groups as the
// route allows; groups_complete says when a chunk held more.
const coverageGroupBySize = 1000

// coverageGroupByCommittee and coverageGroupByProject are the tag prefixes
// the member and membership counts are grouped by.
const (
	coverageGroupByCommittee = "committee_uid"
	coverageGroupByProject   = "project_uid"
)

// activeMembershipFilter keeps memberships whose stored status is Active.
const activeMembershipFilter = "status:Active"

// Gap vocabulary of projectCoverage.Gap.
const (
	gapNoCommittee      = "no_committee"
	gapNoBoardCommittee = "no_board_committee"
	gapEmptyBoard       = "empty_board"
)

// coverageNote travels with every audit result.
const coverageNote = "Counts cover committees, members and memberships indexed in LFX v2 and visible to your identity; a zero can be an access effect or a roster not yet onboarded — never report a project as having no seats from this result alone. Run under an identity with project-level audit rights for a program view."

// errGroupedCountsMissing is returned when a count answered without groups
// although records matched: the server ignored group_by, so the audit
// cannot attribute counts and must not guess.
var errGroupedCountsMissing = errors.New("the query service did not return grouped counts; audit unavailable")

// AuditCommitteeCoverageArgs defines the input parameters for the audit_committee_coverage tool.
type AuditCommitteeCoverageArgs struct {
	FoundationUID string `json:"foundation_uid" jsonschema:"(required) UID of the foundation (root project); the audit covers it and its direct child projects as visible to the caller"`
	Category      string `json:"category,omitempty" jsonschema:"Keep only committees of this category, matched case-insensitively (e.g. Board)"`
}

// committeeCoverageResult is the output of audit_committee_coverage.
type committeeCoverageResult struct {
	FoundationUID   string            `json:"foundation_uid"`
	ProjectsInScope int               `json:"projects_in_scope"`
	Category        string            `json:"category,omitempty"`
	Projects        []projectCoverage `json:"projects"`
	Complete        bool              `json:"complete"`
	// CountAccuracyBound is the largest undercount the query service allowed
	// for any returned group across the audit's counts; zero means every
	// count is exact. Any nonzero bound makes Complete false.
	CountAccuracyBound uint64 `json:"count_accuracy_bound"`
	Visibility         string `json:"visibility"`
	Note               string `json:"note"`
}

// projectCoverage is one project of the family with its committees, their
// visible member counts, its active memberships and the gap, if any.
type projectCoverage struct {
	ProjectUID                     string              `json:"project_uid"`
	ActiveMemberships              uint64              `json:"active_memberships"`
	Committees                     []committeeCoverage `json:"committees"`
	CommitteesWithNoVisibleMembers []string            `json:"committees_with_no_visible_members"`
	HasBoardCommittee              bool                `json:"has_board_committee"`
	Gap                            string              `json:"gap,omitempty"`
}

// committeeCoverage is one committee with the member count visible to the caller.
type committeeCoverage struct {
	UID            string `json:"uid"`
	Name           string `json:"name"`
	Category       string `json:"category"`
	VisibleMembers uint64 `json:"visible_members"`
}

// coverageCommittee is a committee as read from the index.
type coverageCommittee struct {
	UID        string
	Name       string
	Category   string
	ProjectUID string
}

// RegisterAuditCommitteeCoverage registers the audit_committee_coverage tool with the MCP server.
func RegisterAuditCommitteeCoverage(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "audit_committee_coverage",
		Description: "Audit which projects of a foundation have committees onboarded into LFX v2 and how many members each has, over the records visible to the caller. " +
			"foundation_uid is the root project; the audit covers it and its direct child projects. " +
			"Returns, per project: active_memberships, committees (uid, name, category, visible_members), committees_with_no_visible_members, has_board_committee and gap. " +
			"gap is set only for projects with active memberships: no_committee (none indexed), no_board_committee (none of board category), empty_board (a board committee with no visible member). " +
			"category keeps one committee category in the listed committees, matched case-insensitively; gap and has_board_committee are evaluated over every committee. complete=false means a count stopped early, missed groups or may be short by up to count_accuracy_bound. " +
			"A zero can be an access effect or a roster not yet onboarded. For a person's or organization's seats use get_org_committee_seats or search_committee_members.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Audit Committee Coverage",
			ReadOnlyHint: true,
		},
	}, handleAuditCommitteeCoverage)
}

// projectUIDTags returns one project_uid:<uid> tag per uid.
func projectUIDTags(uids []string) []string {
	tags := make([]string, 0, len(uids))
	for _, uid := range uids {
		tags = append(tags, coverageGroupByProject+":"+uid)
	}
	return tags
}

// drainCommittees reads every committee of the projects in one chunk
// (tags match any project of the chunk), following page_token to the cap.
func drainCommittees(ctx context.Context, clients *lfxv2.Clients, chunk []string) ([]coverageCommittee, error) {
	var out []coverageCommittee
	resourceType := committeeResourceType
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= participantMaxDrainPages {
			return nil, fmt.Errorf("the committee list exceeds the %d-page cap", participantMaxDrainPages)
		}
		result, err := clients.QuerySvc.QueryResources(ctx, &querysvc.QueryResourcesPayload{
			Version:   "1",
			Type:      &resourceType,
			Tags:      projectUIDTags(chunk),
			PageSize:  participantDrainPageSize,
			Sort:      "name_asc",
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range result.Resources {
			if r == nil {
				continue
			}
			data, _ := r.Data.(map[string]any)
			str := func(key string) string {
				v, _ := data[key].(string)
				return v
			}
			c := coverageCommittee{UID: str("uid"), Name: str("name"), Category: str("category"), ProjectUID: str("project_uid")}
			if c.UID == "" && r.ID != nil {
				c.UID = *r.ID
			}
			if c.UID != "" {
				out = append(out, c)
			}
		}
		if result.PageToken == nil || *result.PageToken == "" {
			return out, nil
		}
		pageToken = result.PageToken
	}
}

// groupedCount is one grouped count answer: counts by group key, whether
// the answer is complete, and the undercount the service allowed per group.
type groupedCount struct {
	ByKey         map[string]uint64
	Complete      bool
	AccuracyBound uint64
}

// countGrouped runs one grouped count over the chunk and fails closed when
// the server answered records without groups.
func countGrouped(ctx context.Context, clients *lfxv2.Clients, resourceType string, chunk []string, groupBy string, filtersAll []string) (groupedCount, error) {
	rt := resourceType
	gb := groupBy
	size := coverageGroupBySize
	payload := &querysvc.QueryResourcesCountPayload{
		Version:     "1",
		Type:        &rt,
		Tags:        projectUIDTags(chunk),
		GroupBy:     &gb,
		GroupBySize: &size,
	}
	if len(filtersAll) > 0 {
		payload.FiltersAll = filtersAll
	}
	result, err := clients.QuerySvc.QueryResourcesCount(ctx, payload)
	if err != nil {
		return groupedCount{}, err
	}
	// Records matched but no group came back: the server did not honour
	// group_by (or answered an empty array), so nothing can be attributed.
	if len(result.Groups) == 0 && result.Count > 0 {
		return groupedCount{}, errGroupedCountsMissing
	}
	out := groupedCount{ByKey: map[string]uint64{}, Complete: !result.HasMore}
	if result.GroupsComplete == nil || !*result.GroupsComplete {
		out.Complete = false
	}
	// A nonzero bound means a returned group's count may be an undercount;
	// the answer is then not complete, whatever groups_complete says.
	if result.GroupCountErrorUpperBound != nil && *result.GroupCountErrorUpperBound > 0 {
		out.AccuracyBound = *result.GroupCountErrorUpperBound
		out.Complete = false
	}
	for _, g := range result.Groups {
		if g != nil {
			out.ByKey[g.Key] += g.Count
		}
	}
	return out, nil
}

// buildCoverage assembles the per-project rows: every family uid (the
// family carries each uid once), root first then by uid; committees by name. The gap and
// has_board_committee are evaluated over every committee of the project;
// wantCategory (lower-cased, trimmed; "" for all) narrows only the emitted
// committees and committees_with_no_visible_members, so a category filter
// never turns a project with a board into a no_committee gap.
func buildCoverage(family []string, committees []coverageCommittee, membersByCommittee, membershipsByProject map[string]uint64, wantCategory string) []projectCoverage {
	byProject := map[string][]coverageCommittee{}
	for _, c := range committees {
		byProject[c.ProjectUID] = append(byProject[c.ProjectUID], c)
	}
	ordered := make([]string, len(family))
	copy(ordered, family)
	if len(ordered) > 1 {
		sort.Strings(ordered[1:])
	}

	rows := make([]projectCoverage, 0, len(ordered))
	for _, uid := range ordered {
		row := projectCoverage{
			ProjectUID:                     uid,
			ActiveMemberships:              membershipsByProject[uid],
			Committees:                     []committeeCoverage{},
			CommitteesWithNoVisibleMembers: []string{},
		}
		list := byProject[uid]
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Name != list[j].Name {
				return list[i].Name < list[j].Name
			}
			return list[i].UID < list[j].UID
		})
		emptyBoard := false
		for _, c := range list {
			members := membersByCommittee[c.UID]
			if isBoardCategory(c.Category) {
				row.HasBoardCommittee = true
				if members == 0 {
					emptyBoard = true
				}
			}
			if wantCategory != "" && strings.ToLower(strings.TrimSpace(c.Category)) != wantCategory {
				continue
			}
			row.Committees = append(row.Committees, committeeCoverage{UID: c.UID, Name: c.Name, Category: c.Category, VisibleMembers: members})
			if members == 0 {
				row.CommitteesWithNoVisibleMembers = append(row.CommitteesWithNoVisibleMembers, c.UID)
			}
		}
		if row.ActiveMemberships > 0 {
			switch {
			case len(list) == 0:
				row.Gap = gapNoCommittee
			case !row.HasBoardCommittee:
				row.Gap = gapNoBoardCommittee
			case emptyBoard:
				row.Gap = gapEmptyBoard
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// handleAuditCommitteeCoverage implements the audit_committee_coverage tool logic.
func handleAuditCommitteeCoverage(ctx context.Context, req *mcp.CallToolRequest, args AuditCommitteeCoverageArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if orgSeatsConfig == nil || orgSeatsConfig.Clients == nil {
		logger.ErrorContext(ctx, "committee coverage tool not configured")
		return errorResult("Error: committee coverage tool not configured"), nil, nil
	}
	if strings.TrimSpace(args.FoundationUID) == "" {
		return errorResult("Error: foundation_uid is required (the root project's UID; resolve it with search_projects)"), nil, nil
	}

	var tokenInfo *auth.TokenInfo
	if req.Extra != nil {
		tokenInfo = req.Extra.TokenInfo
	}
	ctx, err := orgSeatsConfig.Clients.TokenFromRequest(ctx, tokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to resolve LFX authentication", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to extract MCP token: %v", err)), nil, nil
	}
	clients := orgSeatsConfig.Clients

	logger.InfoContext(ctx, "auditing committee coverage", "foundation_uid", args.FoundationUID, "category", args.Category)

	family, err := resolveFoundationFamily(ctx, clients, args.FoundationUID)
	if err != nil {
		logger.ErrorContext(ctx, "foundation family resolution failed", "error", err)
		return errorResult(friendlyAPIError("failed to resolve the foundation's projects", err)), nil, nil
	}
	wantCategory := strings.ToLower(strings.TrimSpace(args.Category))
	var committees []coverageCommittee
	dropped := 0
	membersByCommittee := map[string]uint64{}
	membershipsByProject := map[string]uint64{}
	complete := true
	var accuracyBound uint64

	for _, chunk := range chunkStrings(family, orgSeatsProjectChunk) {
		found, err := drainCommittees(ctx, clients, chunk)
		if err != nil {
			logger.ErrorContext(ctx, "committee list failed", "error", err)
			return errorResult(friendlyAPIError("failed to list committees", err)), nil, nil
		}
		for _, c := range found {
			if c.ProjectUID == "" {
				// No project to attribute it to; it cannot appear in any row.
				dropped++
				continue
			}
			committees = append(committees, c)
		}

		members, err := countGrouped(ctx, clients, committeeMemberResourceType, chunk, coverageGroupByCommittee, nil)
		if err != nil {
			logger.ErrorContext(ctx, "committee member count failed", "error", err)
			return errorResult(friendlyAPIError("failed to count committee members", err)), nil, nil
		}
		for k, v := range members.ByKey {
			membersByCommittee[k] += v
		}
		complete = complete && members.Complete
		accuracyBound = max(accuracyBound, members.AccuracyBound)

		memberships, err := countGrouped(ctx, clients, memberResourceType, chunk, coverageGroupByProject, []string{activeMembershipFilter})
		if err != nil {
			logger.ErrorContext(ctx, "membership count failed", "error", err)
			return errorResult(friendlyAPIError("failed to count memberships", err)), nil, nil
		}
		for k, v := range memberships.ByKey {
			membershipsByProject[k] += v
		}
		complete = complete && memberships.Complete
		accuracyBound = max(accuracyBound, memberships.AccuracyBound)
	}

	out := committeeCoverageResult{
		FoundationUID:      args.FoundationUID,
		ProjectsInScope:    len(family),
		Category:           args.Category,
		Projects:           buildCoverage(family, committees, membersByCommittee, membershipsByProject, wantCategory),
		Complete:           complete,
		CountAccuracyBound: accuracyBound,
		Visibility:         "caller",
		Note:               coverageNote,
	}

	if dropped > 0 {
		logger.WarnContext(ctx, "committees without project_uid left out of the audit", "count", dropped)
	}
	logger.InfoContext(ctx, "audit_committee_coverage succeeded", "foundation_uid", args.FoundationUID, "projects", len(out.Projects), "committees", len(committees), "complete", complete)
	return jsonResult(ctx, logger, "audit_committee_coverage formatted", out)
}
