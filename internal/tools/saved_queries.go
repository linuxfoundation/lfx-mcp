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
const savedQueriesDescription = `Run saved queries for common semantic layer questions: fixed recipes that return the same figure on every run. When a question matches one, prefer this tool over the explore_lfx_semantic_layer + query_lfx_semantic_layer flow.

If you have not read read_lfx_saved_queries_guidance yet this session, read it BEFORE using this tool - it carries each recipe's use case, the filter mechanics, and how to read the results.

SAVED QUERIES: kpi_members_and_dues_by_account, kpi_new_members_by_year, kpi_membership_tier_split, kpi_membership_churn, kpi_maintainers_by_org, kpi_contributors_by_project, kpi_contributions_by_org, kpi_contributors_by_org, kpi_event_registrations, kpi_event_registrations_by_org, kpi_training_enrollments, kpi_training_enrollments_by_org.

Building a deck or briefing? Also read read_lfx_deck_building_guidance.`

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
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum rows to return, ceiling 500, default 100 when omitted. Use 10-20 for top-N questions."`
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
	// An omitted limit must not return the full result set: unbounded saved
	// queries (e.g. every member account) overflow MCP clients' tool-result
	// budgets. Default to 100, the size the guidance recommends for breakdowns.
	limit := args.Limit
	if limit <= 0 {
		limit = 100
	}
	reqBody["limit"] = limit

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
