// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-mcp/internal/serviceapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// LensConfig holds configuration shared by LFX Lens tools.
type LensConfig struct {
	ServiceAuth
	ServiceClient *serviceapi.Client
}

var lensConfig *LensConfig

// SetLensConfig sets the configuration for LFX Lens tools.
func SetLensConfig(cfg *LensConfig) {
	lensConfig = cfg
}

// ---------------------------------------------------------------------------
// query_lfx_lens — ad-hoc SQL generation
// ---------------------------------------------------------------------------

// RegisterQueryLFXLens registers the query_lfx_lens tool.
func RegisterQueryLFXLens(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "query_lfx_lens",
		Description: `Ask natural language questions about a project's data using ad-hoc SQL generation.

Best for:
- Open-ended analysis (e.g. "which CNCF projects need attention?", "overview of contributions")
- Time series and trends (e.g. "memberships by month for the last 2 years", "YoY growth")
- Questions involving subprojects (e.g. "maintainers per project", "health scores by project")
- Cross-domain joins — maintainers and contributors are separate data models, so questions combining them (e.g. "top maintainers by name with their contribution counts") need this tool
- Exploratory analysis (e.g. "are we dependent on a few organizations?")

Use search_projects first to find the project slug. Prefer query_lfx_semantic_layer for simple, specific aggregates (e.g. "how many active maintainers does CNCF have?", "current membership count").

This tool runs synchronously. Queries take 30–60 seconds — please wait for the result without retrying.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "LFX Lens Query",
			ReadOnlyHint: true,
		},
	}, handleQueryLFXLens)
}

// QueryLFXLensArgs defines the input for query_lfx_lens.
type QueryLFXLensArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug from search_projects (e.g. 'cncf') (required)"`
	Input       string `json:"input" jsonschema:"Natural language question. Best for open-ended analysis, time series/trends, subproject questions, cross-domain joins, and exploratory questions. For simple specific aggregates (single count, total revenue) prefer query_lfx_semantic_layer. Takes 30-60s. (required)"`
}

type lensWorkflowAdditional struct {
	Foundation lensFoundation `json:"foundation"`
}

type lensFoundation struct {
	Slug string `json:"slug"`
}

type lensQueryResponse struct {
	Content    string `json:"content,omitempty"`
	Status     string `json:"status"`
	SessionID  string `json:"session_id"`
	RunID      string `json:"run_id,omitempty"`
	WorkflowID string `json:"workflow_id,omitempty"`
}

const lensWorkflowID = "lfx-lens-mcp-workflow"

func handleQueryLFXLens(ctx context.Context, req *mcp.CallToolRequest, args QueryLFXLensArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	if args.ProjectSlug == "" || args.Input == "" {
		return nil, nil, fmt.Errorf("project_slug and input are required")
	}

	userID := AnonymousUserID
	if req.Extra.TokenInfo != nil && req.Extra.TokenInfo.UserID != "" {
		userID = req.Extra.TokenInfo.UserID
	}

	sessionID := userID + "-" + time.Now().UTC().Format("2006-01-02T15:04:05Z")

	additionalData, err := json.Marshal(lensWorkflowAdditional{
		Foundation: lensFoundation{Slug: args.ProjectSlug},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal additional_data: %w", err)
	}

	startPath := fmt.Sprintf("/workflows/%s/runs", lensWorkflowID)
	body, statusCode, err := lensConfig.ServiceClient.PostMultipart(ctx, startPath, map[string]string{
		"message":         args.Input,
		"additional_data": string(additionalData),
		"user_id":         userID,
		"session_id":      sessionID,
		"stream":          "false",
		"background":      "false",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("lens API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("lens service returned status %d: %s", statusCode, string(body))
	}

	var resp lensQueryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("failed to parse Lens response: %w", err)
	}

	if resp.Status == "ERROR" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Workflow error: %s", resp.Content)}},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: resp.Content}},
	}, nil, nil
}

// ---------------------------------------------------------------------------
// query_lfx_semantic_layer — structured metric queries
// ---------------------------------------------------------------------------

// RegisterSemanticLayer registers the query_lfx_semantic_layer tool.
func RegisterSemanticLayer(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "query_lfx_semantic_layer",
		Description: `LFX Insights Semantic Layer — pre-aggregated metrics for memberships & revenue, code activities & contributions, maintainers, project health scores, projects, events & sponsorships, and education & certifications.

Best for direct, well-scoped questions that ask for a specific number or breakdown: totals, counts, sums, averages, or single-dimension groupings for a known project or foundation (e.g. "how many active maintainers does CNCF have?", "current membership revenue by tier", "total software value for Kubernetes"). Returns deterministic results in seconds rather than the 30-60s of ad-hoc SQL.

Use query_lfx_lens INSTEAD for:
- Open-ended analysis (e.g. "which projects need attention?", "contribution overview")
- Time series and trends (e.g. "memberships by month for the last 2 years", "YoY growth")
- Questions involving subprojects (e.g. "maintainers per project", "health scores by project")
- Cross-domain joins — maintainers and contributors are separate data models, so questions combining them (e.g. "maintainers by name with their contribution counts") need query_lfx_lens
- Exploratory analysis (e.g. "are we dependent on a few organizations?")

Cross-model limitation: metrics from different domains (e.g. active_maintainers + total_activities) can be queried together but can only be grouped/filtered by metric_time and project manager — not by project_slug or domain-specific dimensions like account_name, member_display_name, or membership_tier. For any filtering or grouping by domain-specific dimensions, query one domain at a time.

Note: the maintainers model does not have individual maintainer names — only organization (account_name). For questions asking for maintainer names, use query_lfx_lens directly.

Tip: call action="describe" with target set to an action name to get detailed usage instructions and examples if needed.

Start by calling list_metrics to discover what metrics are available for the user's question.

Actions:

- list_metrics: First step for any data question. Returns metric names, descriptions, and types. When <=10 metrics match, dimensions are included in the response — you can go straight to query without calling get_dimensions. Often this is enough to build a query directly.

- get_dimensions: Get the fields you can group by or filter on for specific metrics. Returns qualified_names (e.g. "asset_id__membership_tier", "maintainer_key__project_slug") which are the exact strings to use in group_by and where params of a query. Use this when list_metrics returned too many results to include dimensions inline, or when you need the full detail (descriptions, types, granularities).

- query: Execute a metric query. Requires metrics (from list_metrics). Optional: group_by, where, order_by, limit (max 500, default unlimited — always set a reasonable limit to avoid huge result sets). For lookback queries prefer order_by + limit over complex where filters.

- describe: Get detailed usage instructions, syntax reference, and examples for any action above. Call with target="query" to see full where-clause syntax and worked examples.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "LFX Insights Semantic Layer",
			ReadOnlyHint: true,
		},
	}, handleSemanticLayer)
}

// SemanticLayerLFXLensArgs defines the input for the unified semantic layer tool.
type SemanticLayerLFXLensArgs struct {
	Action  string   `json:"action" jsonschema:"Required. Start with list_metrics — often enough to go straight to query without get_dimensions. Best for direct questions (counts, revenue, scores). For open-ended, time series, subproject, exploratory, or cross-domain questions (maintainers and contributors are separate models) use query_lfx_lens. Values: list_metrics, get_dimensions, query, describe"`
	Target  string   `json:"target,omitempty" jsonschema:"For action=describe only: which action to get help for (e.g. 'query')"`
	Metrics []string `json:"metrics,omitempty" jsonschema:"Metric names from list_metrics (for get_dimensions and query actions)"`
	Search  string   `json:"search,omitempty" jsonschema:"Search term to filter results (for list_metrics and get_dimensions)"`
	GroupBy []string `json:"group_by,omitempty" jsonschema:"Dimension qualified_names to group by, from the dimensions field in list_metrics or get_dimensions response (for query)"`
	Where   string   `json:"where,omitempty" jsonschema:"Filter expression using dimension qualified_names, e.g. {{ Dimension('asset_id__project_slug') }} = 'cncf' (for query)"`
	OrderBy []string `json:"order_by,omitempty" jsonschema:"Fields to sort by, must also appear in group_by, prefix with - for descending (for query)"`
	Limit   int      `json:"limit,omitempty" jsonschema:"Max rows to return, max 500 (for query)"`
}

var lensDescribeTexts = map[string]string{
	"list_metrics": `list_metrics — Discover available LFX Insights metrics.

Returns metric names, descriptions, types, and labels. When <=10 metrics match, each metric also includes its available dimension qualified_names — so you can go straight to a query without calling get_dimensions.

Parameters:
  search (optional): filter term matched against name and description

Example:
  action: "list_metrics", search: "maintainer"
  → returns active_maintainers, total_maintainers, etc. with their dimensions`,

	"get_dimensions": `get_dimensions — Get dimensions available for specified metrics.

Dimensions are attributes you can group by or filter on. The qualified_name in the response is the exact string to use in group_by and where clauses.

Use this when list_metrics returned too many results to include dimensions inline, or when you need full dimension detail (descriptions, types, time granularities).

Parameters:
  metrics (required): metric names to get dimensions for
  search (optional): filter dimensions by name

Examples:
  action: "get_dimensions", metrics: ["active_maintainers"]
  → finds: maintainer_key__account_name, maintainer_key__project_slug, maintainer_key__platform, ...

  action: "get_dimensions", metrics: ["current_membership_revenue"]
  → finds: asset_id__membership_tier, asset_id__project_slug, asset_id__account_name, ...`,

	"query": `query — Execute a metric query against the Semantic Layer.

Parameters:
  metrics (required): metric names to query.
  group_by (optional): dimension qualified_names (from list_metrics dimensions or get_dimensions response).
  where (optional): MetricFlow filter expressions. Use the qualified_name from dimensions:
    - Categorical: {{ Dimension('qualified_name') }} = 'value'
    - Time: {{ TimeDimension('qualified_name', 'GRAIN') }} >= '2024-01-01'
    - Dates must be yyyy-mm-dd format.
  order_by (optional): fields to sort by. Must also appear in group_by or metrics. Prefix with - for descending.
  limit (optional): max rows to return (max 500). Always set a reasonable limit to avoid huge result sets — use 10-20 for "top N" queries, 50-100 for breakdowns.

For lookback queries (e.g. "last 6 months"), prefer order_by descending on a time dimension + limit, rather than complex where filters.

Examples:

"How many active maintainers does CNCF have?"
  action: "query"
  metrics: ["active_maintainers"]
  where: "{{ Dimension('maintainer_key__project_slug') }} = 'cncf'"

"Membership revenue by tier for CNCF"
  action: "query"
  metrics: ["current_membership_revenue"]
  group_by: ["asset_id__membership_tier"]
  where: "{{ Dimension('asset_id__project_slug') }} = 'cncf'"
  order_by: ["-current_membership_revenue"]

"Top 10 projects by health score"
  action: "query"
  metrics: ["avg_project_health_score"]
  group_by: ["health_metric_key__project_slug", "health_metric_key__project_name"]
  order_by: ["-avg_project_health_score"]
  limit: 10`,
}

func handleSemanticLayer(ctx context.Context, _ *mcp.CallToolRequest, args SemanticLayerLFXLensArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	switch args.Action {
	case "describe":
		return handleLensDescribe(args.Target)
	case "list_metrics":
		return handleLensListMetrics(ctx, args)
	case "get_dimensions":
		return handleLensGetDimensions(ctx, args)
	case "query":
		return handleLensQueryMetrics(ctx, args)
	default:
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Unknown action %q. Valid actions: describe, list_metrics, get_dimensions, query", args.Action)}},
			IsError: true,
		}, nil, nil
	}
}

func handleLensDescribe(target string) (*mcp.CallToolResult, any, error) {
	if target == "" {
		var sb strings.Builder
		sb.WriteString("Available actions (use target to get details):\n\n")
		for _, action := range []string{"list_metrics", "get_dimensions", "query"} {
			lines := strings.SplitN(lensDescribeTexts[action], "\n", 2)
			sb.WriteString("  " + lines[0] + "\n")
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
		}, nil, nil
	}

	text, ok := lensDescribeTexts[target]
	if !ok {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Unknown action %q. Valid targets: list_metrics, get_dimensions, query", target)}},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, nil, nil
}

func handleLensListMetrics(ctx context.Context, args SemanticLayerLFXLensArgs) (*mcp.CallToolResult, any, error) {
	params := url.Values{}
	if args.Search != "" {
		params.Set("search", args.Search)
	}
	return lensDoGet(ctx, "/lfx-lens/semantic-layer/metrics", params)
}

func handleLensGetDimensions(ctx context.Context, args SemanticLayerLFXLensArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: metrics parameter is required for get_dimensions"}},
			IsError: true,
		}, nil, nil
	}

	params := url.Values{}
	params.Set("metrics", strings.Join(args.Metrics, ","))
	if args.Search != "" {
		params.Set("search", args.Search)
	}
	return lensDoGet(ctx, "/lfx-lens/semantic-layer/dimensions", params)
}

func handleLensQueryMetrics(ctx context.Context, args SemanticLayerLFXLensArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: metrics parameter is required for query"}},
			IsError: true,
		}, nil, nil
	}

	reqBody := map[string]any{
		"metrics": args.Metrics,
	}
	if len(args.GroupBy) > 0 {
		reqBody["group_by"] = args.GroupBy
	}
	if args.Where != "" {
		reqBody["where"] = []string{args.Where}
	}
	if len(args.OrderBy) > 0 {
		reqBody["order_by"] = args.OrderBy
	}
	if args.Limit > 0 {
		reqBody["limit"] = args.Limit
	}

	body, statusCode, err := lensConfig.ServiceClient.PostJSON(ctx, "/lfx-lens/semantic-layer/query", reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("query API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))}},
			IsError: true,
		}, nil, nil
	}

	return lensPrettyJSON(body)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func lensDoGet(ctx context.Context, path string, params url.Values) (*mcp.CallToolResult, any, error) {
	body, statusCode, err := lensConfig.ServiceClient.Get(ctx, path, params)
	if err != nil {
		return nil, nil, fmt.Errorf("API call to %s failed: %w", path, err)
	}
	if statusCode != http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))}},
			IsError: true,
		}, nil, nil
	}

	return lensPrettyJSON(body)
}

func lensPrettyJSON(body []byte) (*mcp.CallToolResult, any, error) {
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
	pretty, _ := json.MarshalIndent(raw, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(pretty)}},
	}, nil, nil
}
