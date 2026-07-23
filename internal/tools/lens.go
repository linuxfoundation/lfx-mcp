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

Always use this tool for:
- All membership questions (e.g. "current members", "membership revenue by tier", "churn rate")
- Maintainer names or maintainer+activities data joins, where activities data is the code activities model
  with code contributions, PRs, commits etc (e.g. "top maintainers by contributions", "who maintains Kubernetes?").
  IMPORTANT: activities data (contributors, PRs, code contributions etc) not involving maintainers should use query_lfx_semantic_layer.
- Maintainer time series and trends (the maintainer model lacks good time granularity)
- Event sponsorships (the semantic layer should be used for events and event registration data not related to sponsorships)

Also use this tool for:
- Open-ended or exploratory analysis (e.g. "which projects need attention?", "contribution overview")
- Questions involving subprojects (e.g. "maintainers per project", "health scores by project")
- Cross-domain joins that the semantic layer cannot do (e.g. maintainers + activities)
- Any question where query_lfx_semantic_layer is struggling or returning errors

Important: questions just about contributors/activities (without maintainer joins) should use query_lfx_semantic_layer — it has full contributor data including names, organizations, and activity breakdowns.

Use search_projects first to find the project slug.

This tool runs synchronously. Queries take 15–30 seconds — please wait for the result without retrying.
Tips:
- This tool returns at most 200 rows per request. If you need more results, explicitly request pagination, for example "page 2", "next 200 rows", or "use LIMIT/OFFSET pagination with a stable ORDER BY" (e.g. all registrations for an event).`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Query LFX Lens",
			ReadOnlyHint: true,
		},
	}, handleQueryLFXLens)
}

// QueryLFXLensArgs defines the input for query_lfx_lens.
type QueryLFXLensArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug from search_projects (e.g. 'cncf') (required)"`
	Input       string `json:"input" jsonschema:"Natural language question. Always use for memberships, maintainer names/trends, open-ended analysis, subproject questions, cross-domain joins, and exploratory questions. Takes 15-30s. (required)"`
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

// Description fragments for query_lfx_semantic_layer. The description is
// assembled from a shared template with three variant slots so the shared
// prose cannot drift between the staff and non-staff variants.
const (
	// semanticLayerDescHead is everything before the "Use search_projects ..." guidance line.
	semanticLayerDescHead = `LFX Insights Semantic Layer — pre-aggregated metrics for code activities & contributions, maintainer counts, project health scores, projects, events & event registrations, and education & certifications. Returns deterministic results in seconds.

Best for direct, well-scoped questions: totals, counts, averages, breakdowns by a single dimension, and time series (e.g. "total activities for CNCF", "active maintainers by organization", "health score trend by month", "total enrollments by course"). This is also the right tool for contributor/activity questions — it has full contributor data including names, organizations, and activity breakdowns.

Use query_lfx_lens INSTEAD for:
- All membership questions (memberships model works better with ad-hoc SQL)
- Maintainer names, maintainer+contribution (activities data) joins, or maintainer trends
- Open-ended or exploratory analysis (e.g. "which projects need attention?")
- Questions involving subprojects (e.g. "health scores by project")
- Cross-domain joins (maintainers and contributors are separate models)
- Any question where this tool is struggling or returning errors
- Event sponsorships. All other event and event registration data is fine here

`

	// semanticLayerDescMid sits between the search_projects guidance (slot A)
	// and the query CRITICAL rule 1 text (slot B).
	semanticLayerDescMid = `

Actions:

- list_metrics: First step. Returns metrics with descriptions. When <=15 match, dimensions are included — often enough to go straight to query.

- get_dimensions: Get group_by/filter fields for specific metrics. Use when list_metrics returned too many results to include dimensions.

- query: Execute a metric query. CRITICAL rules:
  1. `

	// semanticLayerDescTail sits between the query CRITICAL rule 1 text (slot B)
	// and the final tip line (slot C).
	semanticLayerDescTail = `
  2. Different metrics use different entity prefixes — always check the dimensions list from list_metrics to find the correct qualified_names. Do not guess prefixes.
  3. Set a reasonable limit (10-50) to avoid huge results.
  4. If you have loaded in metrics and dimensions, and you still can't get the data you are looking for in 5 query turns or less, use query_lfx_lens.

- describe: Get detailed syntax reference and examples for any action.

Tips:
- Contributors and code-related data (commits, PRs, insertions, deletions) are in the activities model — search for "activities" in list_metrics.
  IMPORTANT: Questions about contributors and code-related topics that do not involve maintainers should prefer this tool.
- Events metrics use project_name rather than project_slug for filtering.
- `

	// Slot A: search_projects guidance.
	semanticLayerSlotSearchProjects      = `Use search_projects first to find the project slug. Then call list_metrics to discover available metrics.`
	semanticLayerSlotSearchProjectsStaff = `Use search_projects to find a project slug when scoping to a foundation. Then call list_metrics to discover available metrics.`

	// Slot B: query CRITICAL rule 1.
	semanticLayerSlotScopeRule      = `The where clause MUST include a project scope filter — the project_slug parameter alone does not filter the data. Check the dimensions list for the correct one (e.g. registration_id__project_slug). Some models don't have project_slug — they use project_name instead. In that case, use the full project name from search_projects (e.g. "Cloud Native Computing Foundation (CNCF)").`
	semanticLayerSlotScopeRuleStaff = `project_slug is optional. When provided, where-clause project filters are validated against that foundation's subtree. It may be omitted for global or cross-foundation questions. To scope to a project, add a where filter — check the dimensions list for the correct one (e.g. registration_id__project_slug). Some models don't have project_slug — they use project_name instead. In that case, use the full project name from search_projects (e.g. "Cloud Native Computing Foundation (CNCF)").`

	// Slot C: final tip.
	semanticLayerSlotFinalTip      = `Questions about The Linux Foundation (slug is tlf) still need to be scoped with the correct where clause.`
	semanticLayerSlotFinalTipStaff = `For membership metrics, a Linux Foundation ('tlf') filter only captures direct LF memberships — for global membership aggregates, omit the project filter. Activity metrics are fanned out to foundations, so either a 'tlf' foundation filter or no filter works for global questions.`
)

// semanticLayerDescription assembles the tool description for the given variant.
func semanticLayerDescription(isStaff bool) string {
	if isStaff {
		return semanticLayerDescHead + semanticLayerSlotSearchProjectsStaff +
			semanticLayerDescMid + semanticLayerSlotScopeRuleStaff +
			semanticLayerDescTail + semanticLayerSlotFinalTipStaff
	}
	return semanticLayerDescHead + semanticLayerSlotSearchProjects +
		semanticLayerDescMid + semanticLayerSlotScopeRule +
		semanticLayerDescTail + semanticLayerSlotFinalTip
}

// RegisterSemanticLayer registers the query_lfx_semantic_layer tool.
// When isStaff is true, the tool is registered with a staff variant that makes
// project_slug optional for all actions (and where optional for the query
// action), allowing global and cross-foundation queries; otherwise the
// standard scoped variant with mandatory project scoping is used.
func RegisterSemanticLayer(server *mcp.Server, isStaff bool) {
	annotations := &mcp.ToolAnnotations{
		Title:        "Query LFX Semantic Layer",
		ReadOnlyHint: true,
	}
	if isStaff {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "query_lfx_semantic_layer",
			Description: semanticLayerDescription(true),
			Annotations: annotations,
		}, handleSemanticLayerStaff)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_lfx_semantic_layer",
		Description: semanticLayerDescription(false),
		Annotations: annotations,
	}, handleSemanticLayer)
}

// SemanticLayerLFXLensArgs defines the input for the unified semantic layer tool.
type SemanticLayerLFXLensArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug from search_projects (e.g. 'cncf') (required)"`
	Action      string `json:"action" jsonschema:"Required. Start with list_metrics — often enough to go straight to query. Best for activities, maintainer counts, health scores, projects, events, education. For memberships, maintainer names/trends, open-ended, subproject, or exploratory questions use query_lfx_lens instead. Values: list_metrics, get_dimensions, query, describe"`
	Target      string `json:"target,omitempty" jsonschema:"For action=describe only: which action to get help for (e.g. 'query')"`
	Metrics     string `json:"metrics,omitempty" jsonschema:"Comma-separated metric names from list_metrics (for get_dimensions and query)"`
	Search      string `json:"search,omitempty" jsonschema:"Search term to filter results (for list_metrics and get_dimensions)"`
	GroupBy     string `json:"group_by,omitempty" jsonschema:"Comma-separated dimension qualified_names to group by (for query)"`
	Where       string `json:"where,omitempty" jsonschema:"Required for query action. MUST include a project scope filter using {{ Dimension('qualified_name') }} = 'value' syntax. Find the correct project_slug or project_name dimension from list_metrics dimensions. Example: {{ Dimension('registration_id__project_slug') }} = 'cncf'"`
	OrderBy     string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields, prefix with - for descending (for query)"`
	Limit       int    `json:"limit,omitempty" jsonschema:"Max rows to return, max 500 (for query)"`
}

// SemanticLayerStaffLFXLensArgs defines the input for the staff variant of the
// unified semantic layer tool. It mirrors SemanticLayerLFXLensArgs field for
// field, but project_slug and where are optional so staff callers can run
// global and cross-foundation queries. It must remain a distinct type from
// SemanticLayerLFXLensArgs: the MCP schema cache is keyed by reflect.Type, so
// the two variants of the tool need distinct arg struct types.
type SemanticLayerStaffLFXLensArgs struct {
	ProjectSlug string `json:"project_slug,omitempty" jsonschema:"Optional project slug from search_projects (e.g. 'cncf'). When provided, where-clause project filters are validated against that foundation's subtree. May be omitted for global or cross-foundation queries."`
	Action      string `json:"action" jsonschema:"Required. Start with list_metrics — often enough to go straight to query. Best for activities, maintainer counts, health scores, projects, events, education. For memberships, maintainer names/trends, open-ended, subproject, or exploratory questions use query_lfx_lens instead. Values: list_metrics, get_dimensions, query, describe"`
	Target      string `json:"target,omitempty" jsonschema:"For action=describe only: which action to get help for (e.g. 'query')"`
	Metrics     string `json:"metrics,omitempty" jsonschema:"Comma-separated metric names from list_metrics (for get_dimensions and query)"`
	Search      string `json:"search,omitempty" jsonschema:"Search term to filter results (for list_metrics and get_dimensions)"`
	GroupBy     string `json:"group_by,omitempty" jsonschema:"Comma-separated dimension qualified_names to group by (for query)"`
	Where       string `json:"where,omitempty" jsonschema:"Optional for query action. MetricFlow filter using {{ Dimension('qualified_name') }} = 'value' syntax. Include a project scope filter to scope results (find the correct project_slug or project_name dimension from list_metrics); may be omitted for global or cross-foundation queries. Example: {{ Dimension('registration_id__project_slug') }} = 'cncf'"`
	OrderBy     string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields, prefix with - for descending (for query)"`
	Limit       int    `json:"limit,omitempty" jsonschema:"Max rows to return, max 500 (for query)"`
}

var lensDescribeTexts = map[string]string{
	"list_metrics": `list_metrics — Discover available LFX Insights metrics.

Returns metric names, descriptions, types, and labels. When <=15 metrics match, each metric also includes its available dimension qualified_names — so you can go straight to a query without calling get_dimensions.

Parameters:
  search (optional): filter term matched against name and description

Example:
  action: "list_metrics", search: "maintainer"
  → returns active_maintainers, total_maintainers, etc. with their dimensions`,

	"get_dimensions": `get_dimensions — Get dimensions available for specified metrics.

Dimensions are attributes you can group by or filter on. The qualified_name in the response is the exact string to use in group_by and where clauses.

Use this when list_metrics returned too many results to include dimensions inline, or when you need full dimension detail (descriptions, types, time granularities).

Parameters:
  metrics (required): comma-separated metric names to get dimensions for
  search (optional): filter dimensions by name

Examples:
  action: "get_dimensions", metrics: "active_maintainers"
  → finds: maintainer_key__account_name, maintainer_key__project_slug, maintainer_key__platform, ...

  action: "get_dimensions", metrics: "current_membership_revenue"
  → finds: asset_id__membership_tier, asset_id__project_slug, asset_id__account_name, ...`,

	"query": lensQueryDescribeShared + lensQueryDescribeImportant,
}

// lensQueryDescribeShared is the part of the "query" describe text shared by
// the staff and non-staff variants; only the final Important paragraph varies.
const lensQueryDescribeShared = `query — Execute a metric query against the Semantic Layer.

Parameters:
  metrics (required): comma-separated metric names to query.
  group_by (optional): comma-separated dimension qualified_names from list_metrics or get_dimensions.
  where (optional): MetricFlow filter expression. Use the qualified_name from dimensions:
    - Categorical: {{ Dimension('qualified_name') }} = 'value'
    - Time: {{ TimeDimension('qualified_name', 'GRAIN') }} >= '2024-01-01'
    - Dates must be yyyy-mm-dd format.
  order_by (optional): comma-separated sort fields. Must also appear in group_by or metrics. Prefix with - for descending.
  limit (optional): max rows to return (max 500). Use 10-20 for "top N" queries, 50-100 for breakdowns.

For lookback queries (e.g. "last 6 months"), prefer order_by descending on a time dimension + limit, rather than complex where filters.

Examples:

"How many active maintainers does CNCF have?"
  project_slug: "cncf"
  action: "query"
  metrics: "active_maintainers"
  where: "{{ Dimension('maintainer_key__project_slug') }} = 'cncf'"

"Membership revenue by tier for CNCF"
  project_slug: "cncf"
  action: "query"
  metrics: "current_membership_revenue"
  group_by: "asset_id__membership_tier"
  where: "{{ Dimension('asset_id__project_slug') }} = 'cncf'"
  order_by: "-current_membership_revenue"

"Top 10 projects by health score"
  project_slug: "cncf"
  action: "query"
  metrics: "avg_project_health_score"
  group_by: "health_metric_key__project_slug, health_metric_key__project_name"
  where: "{{ Dimension('health_metric_key__foundation_slug') }} = 'cncf'"
  order_by: "-avg_project_health_score"
  limit: 10

`

const lensQueryDescribeImportant = `Important: project_slug is always required. The where clause MUST include a project_slug filter — the project_slug parameter is used for authorization only, it does not auto-filter the data.`

const lensQueryDescribeImportantStaff = `Important: project_slug is optional. When provided, where-clause project filters are validated against that foundation's subtree — the where clause does the actual data filtering. Omit project_slug and the project filter for global or cross-foundation queries.`

// lensDescribeText returns the describe text for an action, selecting the
// staff variant of the "query" text when isStaff is true.
func lensDescribeText(action string, isStaff bool) (string, bool) {
	if isStaff && action == "query" {
		return lensQueryDescribeShared + lensQueryDescribeImportantStaff, true
	}
	text, ok := lensDescribeTexts[action]
	return text, ok
}

func handleSemanticLayer(ctx context.Context, _ *mcp.CallToolRequest, args SemanticLayerLFXLensArgs) (*mcp.CallToolResult, any, error) {
	return semanticLayerDispatch(ctx, args, false)
}

func handleSemanticLayerStaff(ctx context.Context, _ *mcp.CallToolRequest, args SemanticLayerStaffLFXLensArgs) (*mcp.CallToolResult, any, error) {
	return semanticLayerDispatch(ctx, SemanticLayerLFXLensArgs(args), true)
}

func semanticLayerDispatch(ctx context.Context, args SemanticLayerLFXLensArgs, isStaff bool) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	if !isStaff && args.ProjectSlug == "" && args.Action != "describe" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: project_slug is required"}},
			IsError: true,
		}, nil, nil
	}

	switch args.Action {
	case "describe":
		return handleLensDescribe(args.Target, isStaff)
	case "list_metrics":
		return handleLensListMetrics(ctx, args)
	case "get_dimensions":
		return handleLensGetDimensions(ctx, args)
	case "query":
		return handleLensQueryMetrics(ctx, args, isStaff)
	default:
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Unknown action %q. Valid actions: describe, list_metrics, get_dimensions, query", args.Action)}},
			IsError: true,
		}, nil, nil
	}
}

func handleLensDescribe(target string, isStaff bool) (*mcp.CallToolResult, any, error) {
	if target == "" {
		var sb strings.Builder
		sb.WriteString("Available actions (use target to get details):\n\n")
		for _, action := range []string{"list_metrics", "get_dimensions", "query"} {
			text, _ := lensDescribeText(action, isStaff)
			lines := strings.SplitN(text, "\n", 2)
			sb.WriteString("  " + lines[0] + "\n")
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
		}, nil, nil
	}

	text, ok := lensDescribeText(target, isStaff)
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
	metrics := parseCSV(args.Metrics)
	if len(metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: metrics parameter is required for get_dimensions"}},
			IsError: true,
		}, nil, nil
	}

	params := url.Values{}
	params.Set("metrics", strings.Join(metrics, ","))
	if args.Search != "" {
		params.Set("search", args.Search)
	}
	return lensDoGet(ctx, "/lfx-lens/semantic-layer/dimensions", params)
}

func handleLensQueryMetrics(ctx context.Context, args SemanticLayerLFXLensArgs, isStaff bool) (*mcp.CallToolResult, any, error) {
	metrics := parseCSV(args.Metrics)
	if len(metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: metrics parameter is required for query"}},
			IsError: true,
		}, nil, nil
	}

	if !isStaff && args.Where == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: where is required for query — must include a project scope filter (e.g. {{ Dimension('entity__project_slug') }} = 'slug')"}},
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
		"metrics": metrics,
		// is_staff is the verified lf_staff signal plumbed from tool
		// registration; lfx-lens keeps full project-scope enforcement
		// unless this is explicitly true (absent defaults to false).
		"is_staff": isStaff,
	}
	if args.ProjectSlug != "" {
		// Omit project_slug entirely when empty: the lens API treats absence
		// (not empty string) as "run without project scope validation".
		reqBody["project_slug"] = args.ProjectSlug
	}
	if groupBy := parseCSV(args.GroupBy); len(groupBy) > 0 {
		reqBody["group_by"] = groupBy
	}
	if args.Where != "" {
		reqBody["where"] = []string{args.Where}
	}
	if orderBy := parseCSV(args.OrderBy); len(orderBy) > 0 {
		reqBody["order_by"] = orderBy
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

// parseCSV splits a comma-separated string into trimmed, non-empty values.
// Also handles JSON-encoded arrays (e.g. `["a","b"]`) that some MCP clients send.
func parseCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// Handle JSON array strings from clients that serialize arrays as strings.
	// The ReplaceAll handles double-encoded strings with escaped quotes (e.g. `[\"a\",\"b\"]`).
	if strings.HasPrefix(s, "[") {
		cleaned := strings.ReplaceAll(s, `\"`, `"`)
		var arr []string
		if err := json.Unmarshal([]byte(cleaned), &arr); err == nil {
			out := make([]string, 0, len(arr))
			for _, p := range arr {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			return out
		}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

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
