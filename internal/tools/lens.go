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
- Membership questions (e.g. "current members", "membership revenue by tier", "churn rate"), EXCEPT country/region
  breakdowns, which use query_lfx_semantic_layer
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
	Input       string `json:"input" jsonschema:"Natural language question. Always use for memberships (except country/region breakdowns), maintainer names/trends, open-ended analysis, subproject questions, cross-domain joins, and exploratory questions. Takes 15-30s. (required)"`
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
// explore_lfx_semantic_layer / query_lfx_semantic_layer — structured metrics
// ---------------------------------------------------------------------------

// Both descriptions are truncated at 2048 characters before the model ever sees
// them, so each must stay under that: anything past the cut is silently
// invisible, which is how earlier guidance (the tlf membership caveat, the
// project_name tip) went unread for as long as it did.
// TestSemanticLayerDescriptions_FitSchemaBudget guards the limit.
//
// Discovery and querying are split across two tools so that each gets its own
// budget, and so the query's MetricFlow syntax lives in a tool description
// rather than on an optional parameter — see the note on
// QuerySemanticLayerArgs for why that distinction matters. Anything that still
// does not fit belongs in the help action, whose output is a tool result and
// carries no limit; help is a fallback for a failed query, not a prerequisite.
const exploreSemanticLayerDescription = `Discover what the LFX Insights Semantic Layer can measure, then query it with query_lfx_semantic_layer. Start here whenever you do not already know the exact metric and dimension names.

COVERS (search these words):
- contributions — activity, contributor and org counts, commits, PRs, code lines
- memberships — revenue, counts, churn, discounts, invoices
- events — event, registration, speaker, sponsorship counts and revenue
- education — enrollment and certification counts
- maintainers — total and active maintainer counts
- project health — health scores, software value, cost
- any of the above sliced by country or region — always here, never query_lfx_lens

A metric is the number being measured (total_contributors); a dimension is how you slice, filter or list it (country__lf_region, asset_id__membership_tier). Dimension names are entity__field and the prefix differs per metric, so always copy qualified_names from this tool rather than assembling one by hand — e.g. country__lf_region is a person's country, while activity_project_id__organization_lf_region is an organization's HQ.

ACTIONS
- list_metrics(search): searches metric names and descriptions only, so search a topic word above; a dimension word like "country" matches no metrics. When 15 or fewer match, each comes back with its dimension qualified_names — usually enough to query straight away. If nothing returns, broaden the term.
- get_dimensions(metrics, search): every dimension available to those metrics. Requires at least one metric, so pick a metric first. Passing several returns only the dimensions they share, which is exactly what a cross-domain query can group by.
- help(target): worked query examples. Call it when a query fails or you want a template.

USE query_lfx_lens INSTEAD for questions that do not reduce to a metric above: narrative or "why", subproject exploration, maintainer trends/names, and memberships not sliced by country or region.`

const querySemanticLayerDescription = `Run a query against the LFX Insights Semantic Layer. Covers contributions, memberships, events, education, maintainers and project health — and is always the right tool for anything sliced by country or region. Use explore_lfx_semantic_layer first if you do not know the metric and dimension names; use query_lfx_lens for narrative or "why" questions that do not reduce to a metric.

  metrics (required): comma-separated names. List several to combine them in one result, even across domains — they are joined for you on the dimensions they share. Such a query can only group by dimensions the metrics have in common, and is outer-joined, so a group present in only one domain still appears with NULL for the other.
  group_by: dimension qualified_names, comma-separated, copied verbatim from explore_lfx_semantic_layer. Group by a name dimension to turn a metric into a ranked list of organizations, people or projects. For a trend add metric_time__year, or __quarter, __month, __week, __day.
  where: MetricFlow filter; this does the actual filtering.
    categorical  {{ Dimension('country__lf_region') }} = 'Europe'
    time         {{ TimeDimension('asset_id__install_date', 'DAY') }} >= '2024-01-01'
    Dates are yyyy-mm-dd. Region values are exact strings; group by the dimension with no filter to see them.
  order_by: comma-separated; each field must also appear in group_by or metrics. Prefix - for descending. Pair with limit for top-N.
  limit: ceiling 500. Use 10-20 for top-N, 50-100 for full breakdowns.
  project_slug: optional. Omit it for global or cross-foundation questions — the normal case for country and region questions. When given, the where clause must also carry a project filter, validated against that foundation's subtree.

Many metrics are pre-filtered — current_* is active-only, total_contributors excludes bots — so do not re-filter those. The entities listed with a metric are join keys, not group-by values: grouping by one returns raw IDs, so use the matching name dimension.`

// RegisterSemanticLayer registers the two semantic layer tools. The
// registration gate in cmd/lfx-mcp-server limits both to staff callers, so
// project scoping is optional here; lfx-lens validates any project filters that
// are provided against the requested foundation's subtree.
//
// Discovery and querying are separate tools rather than actions on one tool
// because a tool description and its required parameters are the only guidance
// that reaches the model intact — see the note on SemanticLayerLFXLensArgs.
// Splitting gives the query its own description to hold the MetricFlow syntax,
// and makes metrics genuinely required there rather than optional.
func RegisterSemanticLayer(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "explore_lfx_semantic_layer",
		Description: exploreSemanticLayerDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Explore LFX Semantic Layer",
			ReadOnlyHint: true,
		},
	}, handleExploreSemanticLayer)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_lfx_semantic_layer",
		Description: querySemanticLayerDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Query LFX Semantic Layer",
			ReadOnlyHint: true,
		},
	}, handleQuerySemanticLayer)
}

// ExploreSemanticLayerArgs defines the input for explore_lfx_semantic_layer.
type ExploreSemanticLayerArgs struct {
	Action  string `json:"action" jsonschema:"Required. One of: list_metrics, get_dimensions, help."`
	Search  string `json:"search,omitempty" jsonschema:"For list_metrics, a topic word ('contributor', 'membership', 'event', 'enrollment', 'maintainer', 'health'). For get_dimensions, the slice you are after, e.g. 'region', 'tier', 'name'."`
	Metrics string `json:"metrics,omitempty" jsonschema:"Comma-separated metric names. Required for get_dimensions; pass several to see only the dimensions they share."`
	Target  string `json:"target,omitempty" jsonschema:"For action=help only: which action to get examples for (e.g. 'query'). Omit for an overview."`
}

// QuerySemanticLayerArgs defines the input for query_lfx_semantic_layer.
//
// Only the tool description and REQUIRED parameters reach the model intact.
// Clients that defer tool schemas behind a search index — Claude Desktop does —
// re-serialise the schema and replace optional parameter descriptions with a
// short generated summary. Verified against a live client: a 459-byte where
// description arrived as "Filter conditions." and order_by as "Sort order.".
// Temporarily marking limit required was enough to make its real description
// appear, which is what pinned the cause down.
//
// So Metrics is required here — it carries the multi-metric join rules — and
// anything else the model must not get wrong, above all the MetricFlow filter
// syntax it cannot guess, is repeated in querySemanticLayerDescription. The
// optional descriptions below stay full and accurate for clients that pass them
// through unchanged; they just are not the only copy.
// TestCriticalGuidanceSurvivesSchemaCompaction guards that split.
type QuerySemanticLayerArgs struct {
	Metrics     string `json:"metrics" jsonschema:"Required. Comma-separated metric names from explore_lfx_semantic_layer. List several to combine them in one result, even across domains: they are outer-joined on the dimensions they share, so a group present in only one domain still appears with NULL for the other metric, and you can only group by dimensions they have in common. Many metrics are already filtered — current_* means active-only, total_contributors excludes bots — so do not repeat those conditions in where."`
	GroupBy     string `json:"group_by,omitempty" jsonschema:"Comma-separated dimension qualified_names, copied verbatim from explore_lfx_semantic_layer — they are entity__field and the prefix differs per metric. Group by a name dimension for a ranked list of organizations, people or projects; add metric_time__year (or __quarter, __month, __week, __day) for a trend."`
	Where       string `json:"where,omitempty" jsonschema:"MetricFlow filter; this clause does the actual data filtering. Categorical: {{ Dimension('country__lf_region') }} = 'Europe'. Time: {{ TimeDimension('asset_id__install_date', 'DAY') }} >= '2024-01-01'. Dates are yyyy-mm-dd."`
	OrderBy     string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields. Each must also appear in group_by or metrics. Prefix with - for descending, e.g. -current_membership_revenue."`
	Limit       int    `json:"limit,omitempty" jsonschema:"Maximum rows to return, ceiling 500. Use 10-20 for top-N questions and 50-100 for full breakdowns."`
	ProjectSlug string `json:"project_slug,omitempty" jsonschema:"Optional project slug from search_projects (e.g. 'cncf'). Omit it for global or cross-foundation questions. When provided, the where clause must also carry a project filter, validated against that foundation's subtree."`
}

// lensHelpTexts back the help action. These are tool results, so they carry no
// character budget — but they are a fallback, not a prerequisite: everything
// needed to compose a first query lives in semanticLayerDescription and the
// per-parameter descriptions.
var lensHelpTexts = map[string]string{
	"list_metrics": `list_metrics — discover metrics. Always the first call.

  search (optional): matches metric NAMES and DESCRIPTIONS only.

Search by topic, not by the slice you want: "contributor", "membership",
"event", "enrollment", "maintainer", "health". Words that name a dimension —
"country", "region", "tier" — match no metrics at all.

When 15 or fewer metrics match, each comes back with its dimension
qualified_names, which is usually enough to go straight to query.

Each metric also lists its entities. Those are the keys that link domains, not
things to group by: they are why two metrics can be combined (both
total_contributors and current_membership_revenue carry country). To find what
you can actually group a multi-metric query by, call get_dimensions with both
metrics.

Nothing returned? Broaden the topic or drop to a single word. An unknown metric
name is rejected with ranked suggestions — use them rather than guessing again.`,

	"get_dimensions": `get_dimensions — list the dimensions available to a set of metrics.

  metrics (required): comma-separated metric names. Dimensions cannot be
    searched without a metric, so choose a metric first.
  search (optional): filters by name and description, e.g. "region".

Use each returned qualified_name verbatim in group_by and where.

Passing several metrics returns only the dimensions they SHARE, and that set is
much smaller than either metric's own. Those shared dimensions are what a
cross-domain query can group by.`,

	"query": lensQueryHelp,
}

// lensHelpOverview is returned by help with no target.
const lensHelpOverview = `LFX Insights Semantic Layer — how to use it

Workflow: list_metrics(search) → get_dimensions (only if you need more) → query.

  metric     the number being measured
  dimension  an attribute you group, filter or list by
  entity     the key that links domains — country, project, event, organization

Because domains share entities, one query can span them: contribution metrics
and membership metrics both reach the country dimensions, so they can be
compared side by side in a single result. You never write a join — list several
metrics and group by a dimension they share, and the join path is derived from
the shared entity.

Dimension qualified_names are entity__field. The prefix is the primary key of
the metric's own table, so it differs from metric to metric. Always copy the
name from list_metrics or get_dimensions.

help targets: query, list_metrics, get_dimensions`

const lensQueryHelp = `query — run a metric query.

  metrics   (required) comma-separated metric names.
  group_by  (optional) dimension qualified_names, comma-separated.
  where     (optional) MetricFlow filter:
              categorical  {{ Dimension('country__lf_region') }} = 'Europe'
              time         {{ TimeDimension('asset_id__install_date', 'DAY') }} >= '2024-01-01'
              dates yyyy-mm-dd.
  order_by  (optional) must also appear in group_by or metrics; - for descending.
  limit     (optional) ceiling 500. 10-20 for top-N, 50-100 for breakdowns.

Trends: add metric_time__year (or __quarter, __month, __week, __day) to
group_by rather than writing date ranges by hand.

Ranked lists: group by a name dimension, order by the metric descending, and
set a limit.

Combining metrics from different domains outer-joins them, so a group with data
in only one domain still appears, with NULL for the other metric.

Pre-filtered metrics: current_* is already active-only and total_contributors
already excludes bots. Do not add those conditions again.

project_slug is optional. Supply it and the where clause must carry a project
filter, validated against that foundation's subtree. Omit both for global or
cross-foundation questions.

Examples

  Active maintainers in CNCF
    project_slug  cncf
    metrics       active_maintainers
    where         {{ Dimension('maintainer_key__project_slug') }} = 'cncf'

  Membership revenue by tier, CNCF
    project_slug  cncf
    metrics       current_membership_revenue
    group_by      asset_id__membership_tier
    where         {{ Dimension('asset_id__project_slug') }} = 'cncf'
    order_by      -current_membership_revenue

  Top 10 organizations by contribution in a region
    metrics       total_contributors
    group_by      activity_project_id__organization_name
    where         {{ Dimension('activity_project_id__organization_lf_region') }} = 'Asia Pacific'
    order_by      -total_contributors
    limit         10

  Region values are exact strings — group by the dimension with no filter first
  to see them. lf_region is one of: North America, Europe, China, India, Japan,
  Asia Pacific, Middle East & Africa, Latin America, Other.

  Contribution against financial involvement, by region, globally
    metrics       total_contributors, total_contributing_organizations, current_membership_revenue
    group_by      country__lf_region
    order_by      -current_membership_revenue

  European membership revenue trend by year
    metrics       current_membership_revenue
    group_by      country__lf_region, metric_time__year
    where         {{ Dimension('country__lf_region') }} = 'Europe'
    limit         100`

func handleExploreSemanticLayer(ctx context.Context, _ *mcp.CallToolRequest, args ExploreSemanticLayerArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	switch args.Action {
	// "describe" is the pre-rename name for help, kept so a caller working
	// from a cached schema does not get an Unknown action error.
	case "help", "describe":
		return handleLensHelp(args.Target)
	case "list_metrics":
		return handleLensListMetrics(ctx, args.Search)
	case "get_dimensions":
		return handleLensGetDimensions(ctx, args.Metrics, args.Search)
	case "query":
		// Querying moved to its own tool; a caller on a cached schema would
		// otherwise get a bare "unknown action" with nowhere to go.
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Querying moved to the query_lfx_semantic_layer tool. Call it directly with metrics, group_by, where, order_by and limit."}},
			IsError: true,
		}, nil, nil
	default:
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Unknown action %q. Valid actions: list_metrics, get_dimensions, help. To run a query, use the query_lfx_semantic_layer tool.", args.Action)}},
			IsError: true,
		}, nil, nil
	}
}

func handleLensHelp(target string) (*mcp.CallToolResult, any, error) {
	if target == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: lensHelpOverview}},
		}, nil, nil
	}

	text, ok := lensHelpTexts[target]
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

func handleLensListMetrics(ctx context.Context, search string) (*mcp.CallToolResult, any, error) {
	params := url.Values{}
	if search != "" {
		params.Set("search", search)
	}
	return lensDoGet(ctx, "/lfx-lens/semantic-layer/metrics", params)
}

func handleLensGetDimensions(ctx context.Context, metricsArg, search string) (*mcp.CallToolResult, any, error) {
	metrics := parseCSV(metricsArg)
	if len(metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: metrics parameter is required for get_dimensions"}},
			IsError: true,
		}, nil, nil
	}

	params := url.Values{}
	params.Set("metrics", strings.Join(metrics, ","))
	if search != "" {
		params.Set("search", search)
	}
	return lensDoGet(ctx, "/lfx-lens/semantic-layer/dimensions", params)
}

func handleQuerySemanticLayer(ctx context.Context, _ *mcp.CallToolRequest, args QuerySemanticLayerArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	metrics := parseCSV(args.Metrics)
	if len(metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: metrics is required. Use explore_lfx_semantic_layer with action=list_metrics to find metric names."}},
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
