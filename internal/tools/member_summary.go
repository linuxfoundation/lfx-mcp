// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxMembershipSummaryReads bounds the work of one tool call. A capped call
// returns the service's last token so the caller can continue with the same scope.
const maxMembershipSummaryReads = 10

// membershipSummaryView preserves the summary service's field names and omission
// rules. Generated query_svc types have no JSON tags and cannot be returned as-is.
type membershipSummaryView struct {
	B2bOrgUID            string               `json:"b2b_org_uid"`
	CompanyName          string               `json:"company_name"`
	ProjectUID           string               `json:"project_uid"`
	ProjectSlug          string               `json:"project_slug"`
	TermCount            uint64               `json:"term_count"`
	FirstStart           *string              `json:"first_start,omitempty"`
	LastEnd              *string              `json:"last_end,omitempty"`
	CurrentStatus        *string              `json:"current_status,omitempty"`
	CurrentTierName      *string              `json:"current_tier_name,omitempty"`
	CurrentStart         *string              `json:"current_start,omitempty"`
	CurrentEnd           *string              `json:"current_end,omitempty"`
	CurrentMembershipUID *string              `json:"current_membership_uid,omitempty"`
	TierNames            []string             `json:"tier_names"`
	Statuses             []string             `json:"statuses"`
	Terms                []membershipTermView `json:"terms"`
}

type membershipTermView struct {
	MembershipUID string  `json:"membership_uid"`
	Status        string  `json:"status"`
	TierName      string  `json:"tier_name"`
	Tier          *string `json:"tier,omitempty"`
	StartDate     *string `json:"start_date,omitempty"`
	EndDate       *string `json:"end_date,omitempty"`
}

func toMembershipSummaryView(s *querysvc.MembershipTermSummary) membershipSummaryView {
	terms := make([]membershipTermView, len(s.Terms))
	for i, term := range s.Terms {
		terms[i] = membershipTermView{
			MembershipUID: term.MembershipUID, Status: term.Status,
			TierName: term.TierName, Tier: term.Tier,
			StartDate: term.StartDate, EndDate: term.EndDate,
		}
	}
	return membershipSummaryView{
		B2bOrgUID: s.B2bOrgUID, CompanyName: s.CompanyName,
		ProjectUID: s.ProjectUID, ProjectSlug: s.ProjectSlug,
		TermCount: s.TermCount, FirstStart: s.FirstStart, LastEnd: s.LastEnd,
		CurrentStatus: s.CurrentStatus, CurrentTierName: s.CurrentTierName,
		CurrentStart: s.CurrentStart, CurrentEnd: s.CurrentEnd,
		CurrentMembershipUID: s.CurrentMembershipUID,
		TierNames:            s.TierNames, Statuses: s.Statuses, Terms: terms,
	}
}

// membershipSummaryKey mirrors query-service's membershipIdentityKey, separately
// on each side: nonempty UIDs are exact; fallback labels are trimmed and folded.
// Namespaces prevent a UID from colliding with a label of the same spelling.
func membershipSummaryKey(s *querysvc.MembershipTermSummary) [2]string {
	identity := func(uid, label string) string {
		if uid != "" {
			return "uid:" + uid
		}
		return "label:" + strings.ToLower(strings.TrimSpace(label))
	}
	return [2]string{identity(s.B2bOrgUID, s.CompanyName), identity(s.ProjectUID, s.ProjectSlug)}
}

// readMembershipSummaries uses the caller context already authenticated by
// handleSearchMembers. It never refolds terms: the source owns that definition.
func readMembershipSummaries(ctx context.Context, req *mcp.CallToolRequest, args SearchMembersArgs) (*mcp.CallToolResult, memberSearchResult, error) {
	logger := newToolLogger(ctx, req)
	payload := &querysvc.QueryMembershipSummaryPayload{Version: "1"}
	if args.ProjectUID != "" {
		payload.ProjectUID = &args.ProjectUID
	}
	if args.B2bOrgUID != "" {
		payload.B2bOrgUID = &args.B2bOrgUID
	}
	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	var total uint64
	complete := false
	out := memberSearchResult{
		Summaries:  make([]membershipSummaryView, 0),
		TermsTotal: &total, Complete: &complete,
		Warnings: membershipSummaryArgumentWarnings(req, args),
	}
	if args.PageToken != "" {
		out.Warnings = append([]string{"Continuation of an earlier summary read: this response holds the summaries from the supplied page_token onward; combine it with the earlier output. complete refers to the remainder of the read, not to the whole scope."}, out.Warnings...)
	}
	seen := make(map[[2]string]int)
	repeated := false
	for read := 0; read < maxMembershipSummaryReads; read++ {
		result, err := memberConfig.Clients.QuerySvc.QueryMembershipSummary(ctx, payload)
		if err != nil {
			logger.ErrorContext(ctx, "QueryMembershipSummary failed", "error", err)
			return nil, memberSearchResult{}, toolError(friendlyAPIError("failed to search members", err))
		}
		for _, summary := range result.Summaries {
			key := membershipSummaryKey(summary)
			if previousRead, exists := seen[key]; exists && previousRead != read {
				repeated = true
			}
			seen[key] = read
			out.Summaries = append(out.Summaries, toMembershipSummaryView(summary))
		}
		total += result.TermsTotal
		complete = result.Complete
		out.PageToken = result.PageToken
		if complete {
			break
		}
		if !hasPageToken(result.PageToken) {
			out.Warnings = append(out.Warnings, "This membership summary is partial and has no continuation token; re-read with the organisation's b2b_org_uid and project_uid as the scope.")
			break
		}
		if derefStr(result.PageToken) == derefStr(payload.PageToken) {
			out.Warnings = append(out.Warnings, "This membership summary is partial because its page_token did not advance; re-read with the organisation's b2b_org_uid and project_uid as the scope.")
			break
		}
		if read+1 == maxMembershipSummaryReads {
			out.Warnings = append(out.Warnings, "This membership summary is partial; continue with summary=true and the returned page_token using the same project_uid and b2b_org_uid scope.")
			break
		}
		payload.PageToken = result.PageToken
	}
	// Until the source guarantees whole pairs at read boundaries, keep split
	// rows intact and flag them instead of manufacturing a combined summary.
	if repeated {
		complete = false
		out.Warnings = append(out.Warnings, "The service split an organisation's membership records across reads, so its summary is partial; re-read with that organisation's b2b_org_uid and project_uid as the scope to get it whole.")
	}
	if complete && len(out.Summaries) == 0 {
		out.Warnings = append(out.Warnings, searchWarnings("membership summaries", 0, 0, false, args.PageToken != "")...)
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return nil, memberSearchResult{}, toolError(fmt.Sprintf("Error: failed to format result: %v", err))
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(prettyJSON)}}}, out, nil
}

func membershipSummaryArgumentWarnings(req *mcp.CallToolRequest, args SearchMembersArgs) []string {
	// Presence matters even for an explicitly supplied false, zero or empty
	// string. Direct handler callers can also supply nonzero typed arguments.
	var supplied map[string]json.RawMessage
	if req != nil && req.Params != nil {
		_ = json.Unmarshal(req.Params.Arguments, &supplied)
	}
	var ignored []string
	for _, param := range []struct {
		name string
		set  bool
	}{
		{"search_name", args.SearchName != ""},
		{"tier_uid", args.TierUID != ""},
		{"tier_name", args.TierName != ""},
		{"include_inactive", args.IncludeInactive},
		{"page_size", args.PageSize != 0},
	} {
		if _, present := supplied[param.name]; present || param.set {
			ignored = append(ignored, param.name)
		}
	}
	if len(ignored) == 0 {
		return nil
	}
	return []string{"Ignored parameters for summary=true: " + strings.Join(ignored, ", ") + "."}
}
