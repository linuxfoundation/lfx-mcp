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

Use this tool ONLY for:
- Social listening: mentions of a project on social media and the web (X, Bluesky, Reddit, Hacker News, YouTube, LinkedIn, TikTok), sentiment, share of voice, author reach. The semantic layer has no social listening data.
- Membership counts as of a PAST date or at year end by year ("how many members did CNCF have in 2024"): memberships whose install date is on or before the date and whose churn date is after it. The standard metric memberships is a today-only snapshot.

FALLBACK (the only other use): switch here only when the semantic layer genuinely cannot express the question - after discovery (list_metrics, get_dimensions, get_dimension_values), the read_lfx_semantic_layer_guidance recipes, and two differently-formulated queries have failed. Zero rows or an unknown-name error is a discovery failure, not a reason to switch.

Everything else - contributors, activities, memberships, events and sponsorships, registrations, education, maintainer rosters/counts/names, health - belongs to explore_lfx_semantic_layer + query_lfx_semantic_layer. Committee/board rosters: the committee tools.

project_slug is required default context, NOT a scope boundary. Find it via search_projects. For multiple foundations, pass one slug and name the others in input. LF-wide: use project_slug='tlf'.

Runs synchronously; wait 15-30 seconds without retrying. Returns <=200 rows; request explicit pagination ("page 2", or stable ORDER BY with LIMIT/OFFSET). Windows: default trailing 12 months; state concrete yyyy-mm-dd dates or the SQL picks its own.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Query LFX Lens",
			ReadOnlyHint: true,
		},
	}, handleQueryLFXLens)
}

// QueryLFXLensArgs defines the input for query_lfx_lens.
type QueryLFXLensArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Required default context slug from search_projects, not a scope boundary. For multiple foundations, pass one here and name the others in input; use 'tlf' for LF-wide questions."`
	Input       string `json:"input" jsonschema:"Natural language question. Use for social listening (mentions/sentiment/reach), past-date or by-year membership counts, cross-domain joins and shapes no standard metric expresses; the standard metrics already rank people (top contributors, top maintainers). Contributor, activity, membership, event, education and health questions belong to the semantic layer and its standard metrics - read read_lfx_semantic_layer_guidance before falling back here. Takes 15-30s. (required)"`
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
	if req.Extra != nil && req.Extra.TokenInfo != nil && req.Extra.TokenInfo.UserID != "" {
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

// Both descriptions are truncated at 2048 bytes before the model ever sees
// them, so each must stay under that: anything past the cut is silently
// invisible, which is how earlier guidance (the tlf membership caveat, the
// project_name tip) went unread for as long as it did.
// TestSemanticLayerDescriptions_FitSchemaBudget guards the limit.
//
// Discovery and querying are split across two tools so that each gets its own
// budget, and so the query's MetricFlow syntax lives in a tool description
// rather than on an optional parameter — see the note on
// QuerySemanticLayerArgs for why that distinction matters. Anything that does
// not fit belongs in the read_lfx_semantic_layer_guidance tool, whose output
// is a tool result and carries no limit; both descriptions route the model
// there before its first query.
const exploreSemanticLayerDescription = `The LFX Semantic Layer is the query tool for LF data: contributor, contribution, membership, revenue, event, registration, speaker, sponsorship, enrollment, certification, maintainer, health and project metrics, sliceable by country or region. This discovers what can be measured; query_lfx_semantic_layer runs it. Start here unless exact names are known.

If you have not read read_lfx_semantic_layer_guidance yet this session, read it BEFORE using this tool; one read also covers query_lfx_semantic_layer. Common questions: prefer query_lfx_standard_metrics when a standard metric matches.

ACTIONS
- list_metrics(search): search by one topic word from the list above
- get_dimensions(metrics, search): a metric's group_by/filter surface; several metrics return only their shared dimensions
- get_dimension_values(dimension, metrics, search): stored literals - call before filtering on any unseen value; unknowns return zero rows, not an error ('Asia Pacific' not 'APAC')

Names are entity__field with per-metric prefixes - copy qualified_names, never assemble. Resolve project slugs via search_projects, org legal names via search_b2b_orgs. query_lfx_lens is ONLY for social listening, past-date membership counts, cross-domain joins, or guidance-sanctioned fallback. Board/committee/ambassador rosters: committee tools.`

const querySemanticLayerDescription = `Run governed LFX Semantic Layer metric queries: contributions, memberships, events, sponsorships, education, maintainers, health, country/region. ALWAYS explore_lfx_semantic_layer first unless exact names are known; never guess.

If you have not read read_lfx_semantic_layer_guidance yet this session, read it BEFORE querying; one read also covers explore. If a query_lfx_standard_metrics recipe matches the question, prefer it.

SYNTAX: metrics (required), CSV. group_by: dimension qualified_names copied from explore; add metric_time__year (or __quarter, __month) for trends. where is MetricFlow: {{ Dimension('country__lf_region') }} = 'Europe'; {{ TimeDimension('metric_time','DAY') }} >= '2024-01-01'; dates yyyy-mm-dd. limit optional.

SCOPE lives in where (no project parameter). Foundation: {{ Dimension('project__foundation_slug') }} = '<slug>' (resolve via search_projects); NEVER scope a foundation with project_slug - its catch-all bucket, a silent undercount. Org/account filters take FULL LEGAL names - search_b2b_orgs first.

0 rows = misspelled literal or wrong scope: get_dimension_values, then the guidance recipes, BEFORE any query_lfx_lens fallback. State definition and window with every answer.`

// The two semantic layer tools register independently so that LFXMCP_TOOLS can
// select either by name. They are meant to be enabled together — each
// description points at the other — but a shared gate would mean the name
// "explore_lfx_semantic_layer" registered nothing while
// "query_lfx_semantic_layer" silently registered both.
//
// The registration gate in cmd/lfx-mcp-server limits both to staff callers.
// Project scope lives entirely in the query's where clause; there is no
// separate project parameter.
//
// Discovery and querying are separate tools rather than actions on one tool
// because a tool description and its required parameters are the only guidance
// that reaches the model intact — see the note on QuerySemanticLayerArgs.
// Splitting gives the query its own description to hold the MetricFlow syntax,
// and makes metrics genuinely required there rather than optional.

// RegisterExploreSemanticLayer registers the explore_lfx_semantic_layer tool.
func RegisterExploreSemanticLayer(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "explore_lfx_semantic_layer",
		Description: exploreSemanticLayerDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Explore LFX Semantic Layer",
			ReadOnlyHint: true,
		},
	}, handleExploreSemanticLayer)
}

// RegisterQuerySemanticLayer registers the query_lfx_semantic_layer tool.
func RegisterQuerySemanticLayer(server *mcp.Server) {
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
//
// Action is the only required field, so under the schema compaction described
// on QuerySemanticLayerArgs it is the one parameter description that survives
// intact — hence the full action list lives there rather than being split
// across the optional fields.
type ExploreSemanticLayerArgs struct {
	Action    string `json:"action" jsonschema:"Required. One of: list_metrics, get_dimensions, get_dimension_values. Use get_dimension_values before filtering on any value you have not seen in output: a where clause with a real dimension but an unknown literal returns zero rows instead of an error, so a wrong guess looks exactly like missing data. Recipes and how-to guidance moved to the read_lfx_semantic_layer_guidance tool."`
	Search    string `json:"search,omitempty" jsonschema:"For list_metrics, a topic word ('contributor', 'membership', 'event', 'sponsorship', 'enrollment', 'maintainer', 'health'). For get_dimensions, the slice you are after, e.g. 'region', 'tier', 'name'. For get_dimension_values, a fragment of the value — keep it short, since the stored spelling often differs from the everyday one."`
	Metrics   string `json:"metrics,omitempty" jsonschema:"Comma-separated metric names. Required for get_dimensions and get_dimension_values; pass several to get_dimensions to see only the dimensions they share."`
	Dimension string `json:"dimension,omitempty" jsonschema:"For action=get_dimension_values only: one dimension qualified_name, copied from get_dimensions (e.g. 'country__lf_region')."`
	Target    string `json:"target,omitempty" jsonschema:"Deprecated and ignored: the help action's recipes moved to the read_lfx_semantic_layer_guidance tool. Kept so callers on a cached schema do not fail validation."`
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
	Metrics string `json:"metrics" jsonschema:"Required. Comma-separated metric names taken from explore_lfx_semantic_layer — never guessed. List several to combine them in one result, even across domains: they are outer-joined on the dimensions they share, so a group present in only one domain still appears with NULL for the other metric, and you can only group by dimensions they have in common. Many metrics are already filtered — current_* means active-only, total_contributors excludes bots — so do not repeat those conditions in where. Group by dimension names, never bare entities - entities return raw IDs."`
	GroupBy string `json:"group_by,omitempty" jsonschema:"Comma-separated dimension qualified_names, copied verbatim from explore_lfx_semantic_layer — they are entity__field and the prefix differs per metric. Group by a name dimension for a ranked list of organizations, people or projects; add metric_time__year (or __quarter, __month, __week, __day) for a trend."`
	Where   string `json:"where,omitempty" jsonschema:"MetricFlow filter; this clause does the actual data filtering. Categorical: {{ Dimension('country__lf_region') }} = 'Europe'. Time: {{ TimeDimension('metric_time','DAY') }} >= '2024-01-01'. Dates are yyyy-mm-dd."`
	OrderBy string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields. Each must also appear in group_by or metrics. Prefix with - for descending, e.g. -current_membership_revenue. In combined-metric results NULL rows sort first on a descending metric - re-sort client-side before reading a top-N."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum rows to return. Use 10-20 for top-N questions and 50-100 for full breakdowns. Omitting it returns EVERY row - set a limit unless you need the complete set."`
}

func handleExploreSemanticLayer(ctx context.Context, _ *mcp.CallToolRequest, args ExploreSemanticLayerArgs) (*mcp.CallToolResult, any, error) {
	// The help action's content moved to the read_lfx_semantic_layer_guidance
	// tool. "describe" is the pre-rename name for help. Callers on a cached
	// schema still send both, so they get the full guidance rather than an
	// error — a failed help call would push the model toward guessing or a
	// premature query_lfx_lens fallback, the exact behaviors the guidance
	// exists to stop. The target argument is ignored: the guidance is one
	// document now. Served before the config check: the content is embedded,
	// so help works even when the Lens backend is not configured.
	if args.Action == "help" || args.Action == "describe" {
		return lensHelpResult(semanticLayerGuidance)
	}

	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	switch args.Action {
	case "list_metrics":
		return handleLensListMetrics(ctx, args.Search)
	case "get_dimensions":
		return handleLensGetDimensions(ctx, args.Metrics, args.Search)
	case "get_dimension_values":
		return handleLensGetDimensionValues(ctx, args.Dimension, args.Metrics, args.Search)
	case "query":
		// Reachable only from a caller that already has this tool and reused
		// the old action word; a caller still on the pre-split schema is
		// addressing query_lfx_semantic_layer and never lands here. See the
		// note on the describe alias above.
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Querying moved to the query_lfx_semantic_layer tool. Call it directly with metrics, group_by, where, order_by and limit."}},
			IsError: true,
		}, nil, nil
	default:
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Unknown action %q. Valid actions: list_metrics, get_dimensions, get_dimension_values. To run a query, use the query_lfx_semantic_layer tool; for recipes and how-to guidance, call read_lfx_semantic_layer_guidance.", args.Action)}},
			IsError: true,
		}, nil, nil
	}
}

func lensHelpResult(text string) (*mcp.CallToolResult, any, error) {
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

// handleLensGetDimensionValues lists the literals a dimension can hold.
//
// A where clause with a real dimension but an unknown value succeeds and
// returns no rows, so a wrong guess is indistinguishable from an empty result
// and gets read as "no such data". Seen live against 'APAC' (the value is
// 'Asia Pacific') and 'Vietnam' (it is 'Viet Nam').
func handleLensGetDimensionValues(ctx context.Context, dimension, metricsArg, search string) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(dimension) == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: dimension is required for get_dimension_values. Pass a qualified_name from get_dimensions, e.g. country__lf_region."}},
			IsError: true,
		}, nil, nil
	}

	metrics := parseCSV(metricsArg)
	if len(metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: metrics is required for get_dimension_values — it is what the dimension is checked against. Pass the metric you intend to query."}},
			IsError: true,
		}, nil, nil
	}

	params := url.Values{}
	params.Set("dimension", strings.TrimSpace(dimension))
	params.Set("metrics", strings.Join(metrics, ","))
	if search != "" {
		params.Set("search", search)
	}
	return lensDoGet(ctx, "/lfx-lens/semantic-layer/dimension-values", params)
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

	reqBody := map[string]any{
		"metrics": metrics,
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
