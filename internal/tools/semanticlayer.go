// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/dbtsl"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SemanticLayerConfig holds configuration for the dbt Semantic Layer tools.
//
// These tools talk to the dbt Semantic Layer directly. They are deliberately
// independent of the LFX Lens client, which now backs only query_lfx_lens.
type SemanticLayerConfig struct {
	Client *dbtsl.Client
}

var semanticLayerConfig *SemanticLayerConfig

// SetSemanticLayerConfig sets the configuration for the semantic layer tools.
func SetSemanticLayerConfig(cfg *SemanticLayerConfig) {
	semanticLayerConfig = cfg
}

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
const exploreSemanticLayerDescription = `The LFX Insights Semantic Layer is the query and data-exploration tool for Linux Foundation data. This half discovers what can be measured; query_lfx_semantic_layer runs it. Start here whenever you do not already know the exact metric, dimension and value names.

COVERS — search one of these topic words:
- contributor, contribution — activity and org counts, commits, PRs
- membership, revenue, churn — counts, discounts, invoices
- event, registration, speaker — counts and revenue
- enrollment, certification — education
- maintainer — total and active counts
- health, project — health scores, software value, cost
- any of the above sliced by country or region — always here, never query_lfx_lens

A metric is the number measured (total_contributors); a dimension is how you slice, filter or list it (country__lf_region). Names are entity__field and the prefix differs per metric, so copy qualified_names from this tool rather than assembling one — country__lf_region is a person's country, activity_project_id__organization_lf_region an organization's HQ.

ACTIONS
- list_metrics(search): searches metric names and descriptions only, so search a topic word above, not a dimension word like "country". When 15 or fewer match, each returns its dimension qualified_names — usually enough to query.
- get_dimensions(metrics, search): dimensions available to those metrics; needs at least one. Passing several returns only the ones they share — what a cross-domain query can group by.
- get_dimension_values(dimension, metrics, search): the literals a dimension holds. Call it before filtering on any value not already seen in output: an unknown literal returns zero rows, not an error, so a wrong guess reads as missing data. Spellings surprise — 'Asia Pacific' not 'APAC', 'Viet Nam' not 'Vietnam'.
- help(target): worked query examples, for when a query fails.

USE query_lfx_lens INSTEAD for questions that do not reduce to a metric above: narrative or "why", subprojects, maintainer trends, event sponsorships.`

const querySemanticLayerDescription = `The LFX Insights Semantic Layer is the query and data-exploration tool for Linux Foundation data; this half runs the query. Covers contributions, memberships, events, education, maintainers and project health — and anything sliced by country or region. ALWAYS call explore_lfx_semantic_layer first unless you already have the exact metric, dimension and entity names — never guess or assemble one: a wrong name errors, and a wrong filter value returns no rows rather than an error, so confirm literals with get_dimension_values. Use query_lfx_lens for narrative or "why" questions that do not reduce to a metric.

  metrics (required): comma-separated names. List several to combine them in one result, even across domains — they are joined on the dimensions they have in common, the only set such a query can group by. The join is outer, so a group in only one domain still appears with NULL for the other.
  group_by: dimension qualified_names, comma-separated, copied verbatim. Group by a name dimension for a ranked list of organizations, people or projects. For a trend add metric_time__year, or __quarter, __month, __week, __day.
  where: MetricFlow filter; this does the filtering, including by project or foundation.
    categorical  {{ Dimension('country__lf_region') }} = 'Europe'
    time         {{ TimeDimension('asset_id__install_date', 'DAY') }} >= '2024-01-01'
    Dates are yyyy-mm-dd.
  order_by: comma-separated; each field must also appear in group_by or metrics. Prefix - for descending.
  limit: ceiling 500. Use 10-20 for top-N, 50-100 for full breakdowns.

Queries are global by default. To restrict one to a project or foundation, put that filter in where — there is no separate scope parameter.

Many metrics are pre-filtered — current_* is active-only, total_contributors excludes bots — so do not re-filter those. Entities listed with a metric are join keys, not group-by values: grouping by one returns raw IDs, so use the name dimension.`

// The two semantic layer tools register independently so that LFXMCP_TOOLS can
// select either by name. They are meant to be enabled together — each
// description points at the other — but a shared gate would mean the name
// "explore_lfx_semantic_layer" registered nothing while
// "query_lfx_semantic_layer" silently registered both.
//
// The registration gate in cmd/lfx-mcp-server limits both to staff callers.
// There is no per-query project scoping: a caller restricts a query by writing
// the filter into the where clause like any other.
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
	Action    string `json:"action" jsonschema:"Required. One of: list_metrics, get_dimensions, get_dimension_values, help. Use get_dimension_values before filtering on any value you have not seen in output: a where clause with a real dimension but an unknown literal returns zero rows instead of an error, so a wrong guess looks exactly like missing data."`
	Search    string `json:"search,omitempty" jsonschema:"For list_metrics, a topic word ('contributor', 'membership', 'event', 'enrollment', 'maintainer', 'health'). For get_dimensions, the slice you are after, e.g. 'region', 'tier', 'name'. For get_dimension_values, a fragment of the value — keep it short, since the stored spelling often differs from the everyday one."`
	Metrics   string `json:"metrics,omitempty" jsonschema:"Comma-separated metric names. Required for get_dimensions and get_dimension_values; pass several to get_dimensions to see only the dimensions they share."`
	Dimension string `json:"dimension,omitempty" jsonschema:"For action=get_dimension_values only: one dimension qualified_name, copied from get_dimensions (e.g. 'country__lf_region')."`
	Target    string `json:"target,omitempty" jsonschema:"For action=help only: which action to get examples for (e.g. 'query'). Omit for an overview."`
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
	Metrics string `json:"metrics" jsonschema:"Required. Comma-separated metric names taken from explore_lfx_semantic_layer — never guessed. List several to combine them in one result, even across domains: they are outer-joined on the dimensions they share, so a group present in only one domain still appears with NULL for the other metric, and you can only group by dimensions they have in common. Many metrics are already filtered — current_* means active-only, total_contributors excludes bots — so do not repeat those conditions in where."`
	GroupBy string `json:"group_by,omitempty" jsonschema:"Comma-separated dimension qualified_names, copied verbatim from explore_lfx_semantic_layer — they are entity__field and the prefix differs per metric. Group by a name dimension for a ranked list of organizations, people or projects; add metric_time__year (or __quarter, __month, __week, __day) for a trend."`
	Where   string `json:"where,omitempty" jsonschema:"MetricFlow filter; this clause does the actual data filtering, including restricting a query to a project or foundation. Categorical: {{ Dimension('country__lf_region') }} = 'Europe'. Time: {{ TimeDimension('asset_id__install_date', 'DAY') }} >= '2024-01-01'. Dates are yyyy-mm-dd."`
	OrderBy string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields. Each must also appear in group_by or metrics. Prefix with - for descending, e.g. -current_membership_revenue."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum rows to return, ceiling 500. Use 10-20 for top-N questions and 50-100 for full breakdowns."`
}

// semanticLayerHelpTexts back the help action. These are tool results, so they carry no
// character budget — but they are a fallback, not a prerequisite: everything
// needed to compose a first query lives in exploreSemanticLayerDescription,
// querySemanticLayerDescription and the per-parameter descriptions.
var semanticLayerHelpTexts = map[string]string{
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

	"get_dimension_values": `get_dimension_values — list the literals a dimension can hold.

  dimension (required): one qualified_name from get_dimensions.
  metrics   (required): the metric you intend to query. The dimension is
    checked against it, so the two must go together.
  search    (optional): case-insensitive substring. Keep it short — a fragment
    like "viet" finds a value however it is spelled.

Call this before filtering on any value you have not already seen in output.
An unknown literal is not an error: the query succeeds and returns zero rows,
which is indistinguishable from the data genuinely being empty.

Stored spellings are not the everyday ones:
  lf_region     'Asia Pacific', never 'APAC'
  country_name  'Viet Nam', 'Korea, Republic of', 'Türkiye' — ISO spellings

Values come from the dimension's full domain, not just rows carrying the
metric, so a value listed here can still return no rows once other filters are
applied.

Prefer the country__* dimensions over asset_id__billing_country, which is
unnormalized free text and holds both 'Viet Nam' and 'Vietnam' alongside
entries like 'na', 'US' and 'Untied States'. Filtering on it drops members
filed under a different spelling.`,

	"query": semanticLayerQueryHelp,
}

// semanticLayerHelpOverview is returned by help with no target.
const semanticLayerHelpOverview = `LFX Insights Semantic Layer — how to use it

Workflow: list_metrics(search) → get_dimensions (only if you need more) →
get_dimension_values (before filtering on an unseen value) → query.

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

help targets: query, list_metrics, get_dimensions, get_dimension_values`

const semanticLayerQueryHelp = `query — run a metric query.

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

Queries are global by default. To restrict one to a project or foundation, put
that filter in the where clause like any other; there is no scope parameter.

Examples

  Active maintainers in CNCF
    metrics       active_maintainers
    where         {{ Dimension('maintainer_key__project_slug') }} = 'cncf'

  Membership revenue by tier, CNCF
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

  Filter values are exact strings. lf_region is one of: North America, Europe,
  China, India, Japan, Asia Pacific, Middle East & Africa, Latin America, Other.
  For any other dimension use explore_lfx_semantic_layer's get_dimension_values
  rather than guessing — a wrong literal returns zero rows, not an error.

  Contribution against financial involvement, by region, globally
    metrics       total_contributors, total_contributing_organizations, current_membership_revenue
    group_by      country__lf_region
    order_by      -current_membership_revenue

  European membership revenue trend by year
    metrics       current_membership_revenue
    group_by      country__lf_region, metric_time__year
    where         {{ Dimension('country__lf_region') }} = 'Europe'
    limit         100`

// maxQueryLimit is the largest number of rows a single query may return.
const maxQueryLimit = 500

func handleExploreSemanticLayer(ctx context.Context, _ *mcp.CallToolRequest, args ExploreSemanticLayerArgs) (*mcp.CallToolResult, any, error) {
	if semanticLayerConfig == nil || semanticLayerConfig.Client == nil {
		return nil, nil, fmt.Errorf("semantic layer tools not configured")
	}

	switch args.Action {
	// "describe" is the pre-rename name for help. It only helps a caller that
	// has this tool but reuses the old action word — a caller still on the
	// pre-split schema is addressing query_lfx_semantic_layer, which no longer
	// takes an action at all and cannot reach here. Restoring that path would
	// mean making metrics optional again on the query tool, which is exactly
	// the compaction protection the split exists to get, so the stale-schema
	// case is left to resolve itself when the client refreshes its tool list.
	case "help", "describe":
		return handleSemanticLayerHelp(args.Target)
	case "list_metrics":
		return handleSLListMetrics(ctx, args.Search)
	case "get_dimensions":
		return handleSLGetDimensions(ctx, args.Metrics, args.Search)
	case "get_dimension_values":
		return handleSLGetDimensionValues(ctx, args.Dimension, args.Metrics, args.Search)
	case "query":
		// Reachable only from a caller that already has this tool and reused
		// the old action word; a caller still on the pre-split schema is
		// addressing query_lfx_semantic_layer and never lands here. See the
		// note on the describe alias above.
		return toolError("Querying moved to the query_lfx_semantic_layer tool. Call it directly with metrics, group_by, where, order_by and limit.")
	default:
		return toolError(fmt.Sprintf("Unknown action %q. Valid actions: list_metrics, get_dimensions, get_dimension_values, help. To run a query, use the query_lfx_semantic_layer tool.", args.Action))
	}
}

func handleSemanticLayerHelp(target string) (*mcp.CallToolResult, any, error) {
	if target == "" {
		return toolText(semanticLayerHelpOverview)
	}

	text, ok := semanticLayerHelpTexts[target]
	if !ok {
		return toolError(fmt.Sprintf("Unknown help target %q. Valid targets: list_metrics, get_dimensions, get_dimension_values, query", target))
	}
	return toolText(text)
}

func handleSLListMetrics(ctx context.Context, search string) (*mcp.CallToolResult, any, error) {
	metrics, err := semanticLayerConfig.Client.FetchAllowedMetrics(ctx, search)
	if err != nil {
		return toolError(fmt.Sprintf("Metrics lookup failed: %v", err))
	}

	// An empty result for a search is a dead end, so name the topic words that
	// do match rather than returning [] and leaving the caller to guess again.
	if len(metrics) == 0 && strings.TrimSpace(search) != "" {
		return toolError(dbtsl.NoMetricsDetail(search))
	}
	return toolJSON(metrics)
}

func handleSLGetDimensions(ctx context.Context, metricsArg, search string) (*mcp.CallToolResult, any, error) {
	metrics := parseCSV(metricsArg)
	if len(metrics) == 0 {
		return toolError("Error: metrics parameter is required for get_dimensions")
	}
	if disallowed := dbtsl.ValidateMetrics(metrics); len(disallowed) > 0 {
		return toolError(dbtsl.UnknownMetricsDetail(disallowed))
	}

	dimensions, err := semanticLayerConfig.Client.FetchDimensions(ctx, metrics)
	if err != nil {
		return toolError(fmt.Sprintf("Dimensions lookup failed: %v", err))
	}

	// The upstream API has no dimension search, so narrowing happens here,
	// across the name and the description.
	if term := strings.ToLower(strings.TrimSpace(search)); term != "" {
		filtered := make([]dbtsl.DimensionInfo, 0, len(dimensions))
		for _, d := range dimensions {
			if strings.Contains(strings.ToLower(d.Name+" "+d.Description), term) {
				filtered = append(filtered, d)
			}
		}
		dimensions = filtered
	}
	return toolJSON(dimensions)
}

// handleSLGetDimensionValues lists the literals a dimension can hold.
//
// A where clause with a real dimension but an unknown value succeeds and
// returns no rows, so a wrong guess is indistinguishable from an empty result
// and gets read as "no such data". Seen live against 'APAC' (the value is
// 'Asia Pacific') and 'Vietnam' (it is 'Viet Nam').
func handleSLGetDimensionValues(ctx context.Context, dimension, metricsArg, search string) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(dimension) == "" {
		return toolError("Error: dimension is required for get_dimension_values. Pass a qualified_name from get_dimensions, e.g. country__lf_region.")
	}

	metrics := parseCSV(metricsArg)
	if len(metrics) == 0 {
		return toolError("Error: metrics is required for get_dimension_values — it is what the dimension is checked against. Pass the metric you intend to query.")
	}

	values, err := semanticLayerConfig.Client.FetchDimensionValues(ctx, dimension, metrics, search, 100)
	if err != nil {
		var unknown *dbtsl.UnknownDimensionError
		if errors.As(err, &unknown) {
			return toolError(unknown.Message)
		}
		var failed *dbtsl.QueryFailedError
		if errors.As(err, &failed) {
			return toolError(fmt.Sprintf("Query failed: %s", failed.Message))
		}
		return toolError(fmt.Sprintf("Dimension values query failed: %v", err))
	}

	// An empty list reads as "this data does not exist". It usually means the
	// stored spelling is not the everyday one, so say that instead.
	if values.ValueCount == 0 {
		return toolError(dbtsl.NoDimensionValuesDetail(dimension, strings.TrimSpace(search)))
	}
	return toolJSON(values)
}

func handleQuerySemanticLayer(ctx context.Context, _ *mcp.CallToolRequest, args QuerySemanticLayerArgs) (*mcp.CallToolResult, any, error) {
	if semanticLayerConfig == nil || semanticLayerConfig.Client == nil {
		return nil, nil, fmt.Errorf("semantic layer tools not configured")
	}

	metrics := parseCSV(args.Metrics)
	if len(metrics) == 0 {
		return toolError("Error: metrics is required. Use explore_lfx_semantic_layer with action=list_metrics to find metric names.")
	}
	if args.Limit > maxQueryLimit {
		return toolError(fmt.Sprintf("Error: limit must be %d or less", maxQueryLimit))
	}
	// An omitted or negative limit means "no limit" to the API, which returns
	// one result page and reports its length as the row count: 1024 rows
	// presented as the whole answer. Default to the ceiling this tool
	// advertises so the documented cap is the one that actually applies.
	if args.Limit < 1 {
		args.Limit = maxQueryLimit
	}
	if disallowed := dbtsl.ValidateMetrics(metrics); len(disallowed) > 0 {
		return toolError(dbtsl.UnknownMetricsDetail(disallowed))
	}

	queryArgs := dbtsl.QueryArgs{
		Metrics: metrics,
		GroupBy: parseCSV(args.GroupBy),
		OrderBy: parseCSV(args.OrderBy),
		Limit:   args.Limit,
	}
	if args.Where != "" {
		queryArgs.Where = []string{args.Where}
	}

	result, err := semanticLayerConfig.Client.Query(ctx, queryArgs)
	if err != nil {
		var failed *dbtsl.QueryFailedError
		if errors.As(err, &failed) {
			return toolError(fmt.Sprintf("Query failed: %s", failed.Message))
		}
		return toolError(fmt.Sprintf("Query failed: %v", err))
	}
	return toolJSON(result)
}

// ---------------------------------------------------------------------------
// Result helpers
// ---------------------------------------------------------------------------

func toolText(text string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, nil, nil
}

func toolError(text string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}, nil, nil
}

// toolJSON renders a value as indented JSON for the model to read.
func toolJSON(value any) (*mcp.CallToolResult, any, error) {
	pretty, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode semantic layer result: %w", err)
	}
	return toolText(string(pretty))
}
