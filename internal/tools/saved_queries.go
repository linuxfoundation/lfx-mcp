// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools implements the MCP tool handlers for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// query_lfx_semantic_layer_saved_queries — governed KPI recipes
// ---------------------------------------------------------------------------

// The saved queries listed here are defined in lf-dbt's
// models/semantic_saved_queries.yml; the list below is a routing surface, not
// the source of truth. When a saved query is added or retired there, update
// this description in the same change that updates the prod tool allowlist.
//
// Execution is proxied through the LFX Lens service's saved-query endpoint,
// which passes the name to the dbt Semantic Layer (dbt-sl-sdk: saved_query is
// mutually exclusive with metrics/group_by; where and limit apply on top).
// A saved query missing from the deployed manifest fails with a clear
// server-side error, which the handler returns verbatim — that is the signal
// the dbt side has not deployed it yet, not a reason to retry.
const savedQueriesDescription = `Governed KPI recipes: the headline figures decks and leadership ask for, pinned as dbt saved queries so every consumer computes them identically. When one matches the question, run it by name instead of assembling metrics/group_by through query_lfx_semantic_layer.

SAVED QUERIES (name: use case)
- kpi_members_and_dues_by_account: current members and dues by account, as-of today (active terms)
- kpi_new_members_by_year: new members per year, YTD-bounded
- kpi_membership_tier_split: active members and dues by tier (tier literals differ per foundation)
- kpi_membership_churn: churned memberships per year (churn-date axis; current year partial)
- kpi_maintainers_by_org: active maintainers by employer on LF projects
- kpi_contributors_by_project: distinct code contributors per project (never sum rows - people span projects)
- kpi_contributions_by_org: code contribution VOLUME by organization
- kpi_contributors_by_org: contributor HEADCOUNT by organization (not additive - people span accounts)
- kpi_event_registrations: accepted registrations per event (rows, not people)
- kpi_training_enrollments: enrollments per course/certification (TI+edX scope, not the lifetime trained headline)
- kpi_event_registrations_by_org / kpi_training_enrollments_by_org: the same by organization (edX enrollments all land in the NULL account)

READING RESULTS: *_by_org/_by_account rows come at the (account_name, account_rollup_name) PAIR grain - never read the rollup column as a ranking directly. Rollup-grain rankings: sum rows sharing the rollup value client-side (additive metrics only; for contributor headcount re-query grouped by rollup alone). A NULL account row is unresolved attribution, usually the largest row - never present it as an organization. where (MetricFlow syntax, e.g. {{ Dimension('project__foundation_slug') }} = 'akrites') and limit narrow the recipe. If the server reports the saved query does not exist, it is not deployed yet - answer via explore_lfx_semantic_layer + query_lfx_semantic_layer instead.`

// RegisterSavedQueries registers the query_lfx_semantic_layer_saved_queries tool.
func RegisterSavedQueries(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_lfx_semantic_layer_saved_queries",
		Description: savedQueriesDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Query LFX Semantic Layer Saved Queries",
			ReadOnlyHint: true,
		},
	}, handleSavedQueries)
}

// SavedQueriesArgs defines the input for query_lfx_semantic_layer_saved_queries.
//
// SavedQuery is the only required field, so under the schema compaction
// described on QuerySemanticLayerArgs its description is the one that survives
// intact; everything else the model must not get wrong stays in the tool
// description.
type SavedQueriesArgs struct {
	SavedQuery string `json:"saved_query" jsonschema:"Required. The saved query name, exactly as listed in the tool description (e.g. kpi_members_and_dues_by_account). The recipe's metrics and grouping are fixed - there are no metrics/group_by parameters here."`
	Where      string `json:"where,omitempty" jsonschema:"Optional MetricFlow filter applied on top of the recipe, e.g. {{ Dimension('project__foundation_slug') }} = 'akrites'. Dates are yyyy-mm-dd. Check literals with explore_lfx_semantic_layer's get_dimension_values first - an unknown literal returns zero rows, not an error."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum rows to return, ceiling 500. Use 10-20 for top-N questions."`
}

func handleSavedQueries(ctx context.Context, _ *mcp.CallToolRequest, args SavedQueriesArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	if args.SavedQuery == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: saved_query is required. Pick a name from the tool description, e.g. kpi_members_and_dues_by_account."}},
			IsError: true,
		}, nil, nil
	}

	if args.Limit > 500 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: limit must be 500 or less"}},
			IsError: true,
		}, nil, nil
	}

	reqBody := map[string]any{
		"saved_query": args.SavedQuery,
	}
	if args.Where != "" {
		reqBody["where"] = []string{args.Where}
	}
	if args.Limit > 0 {
		reqBody["limit"] = args.Limit
	}

	body, statusCode, err := lensConfig.ServiceClient.PostJSON(ctx, "/lfx-lens/semantic-layer/saved-query", reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("saved query API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))}},
			IsError: true,
		}, nil, nil
	}

	return lensPrettyJSON(body)
}
