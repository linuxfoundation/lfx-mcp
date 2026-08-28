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
- Maintainer names or maintainer+activities data joins, where activities data is the code activities model
  with code contributions, PRs, commits etc (e.g. "top maintainers by contributions", "who maintains Kubernetes?").
  IMPORTANT: activities data (contributors, PRs, code contributions etc) not involving maintainers should use query_lfx_semantic_layer.
- Maintainer time series and trends (the maintainer model lacks good time granularity)
- Event sponsorships (the semantic layer should be used for events and event registration data not related to sponsorships)
- Social listening: mentions of a project on social media and the web (Twitter/X, Bluesky, Reddit, Hacker News, DEV,
  Podcasts, YouTube, LinkedIn, TikTok), sentiment, share of voice by platform, and author reach/followers
  (e.g. "how is Kubernetes trending on social media?", "sentiment split of our mentions last month").
  The semantic layer has no social listening data.

Also use this tool for:
- Open-ended or exploratory analysis (e.g. "which projects need attention?", "contribution overview")
- Questions involving subprojects (e.g. "maintainers per project", "health scores by project")
- Cross-domain joins that the semantic layer cannot do (e.g. maintainers + activities)
- Any question where query_lfx_semantic_layer is struggling or returning errors

Important: contributor, activity and membership questions belong to the semantic layer — explore_lfx_semantic_layer then query_lfx_semantic_layer.

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
	Input       string `json:"input" jsonschema:"Natural language question. Use for maintainer names/trends, social listening (mentions/sentiment/reach), open-ended analysis, subproject questions, cross-domain joins, and exploratory questions. Contributor, activity and membership questions belong to the semantic layer. Takes 15-30s. (required)"`
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

// Both descriptions are truncated at 2048 bytes before the model ever sees
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
const exploreSemanticLayerDescription = `The LFX Insights Semantic Layer is the query and data-exploration tool for Linux Foundation data. This tool discovers what can be measured; query_lfx_semantic_layer runs it. Start here unless exact metric, dimension and value names are already known.

COVERS — search one topic word:
- contributor, contribution — activity/org counts, commits, PRs
- membership, revenue, churn — counts, discounts, invoices
- event, registration, speaker — counts and revenue
- enrollment, certification — education
- maintainer — total and active counts
- health, project — health scores, software value, cost
- any of the above sliced by country or region — always here, never query_lfx_lens

A metric is measured (total_contributors); a dimension slices, filters or lists it (country__lf_region). Names are entity__field and prefixes differ per metric, so copy qualified_names; never assemble them. country__lf_region is a person's country; activity_project_id__organization_lf_region is an organization's HQ.

ACTIONS
- list_metrics(search): searches metric names/descriptions, so use a topic above, not a dimension like "country". At ≤15 matches, each includes dimension qualified_names.
- get_dimensions(metrics, search): available dimensions; needs a metric. Several metrics return only shared dimensions—a cross-domain query's valid group_by set.
- get_dimension_values(dimension, metrics, search): stored literals. Call before filtering on unseen values: an unknown returns zero rows, not an error. Spellings surprise—'Asia Pacific' not 'APAC', 'Viet Nam' not 'Vietnam'.
- help(target): worked query examples after failure.

Project scope lives in query's where clause (no parameter): resolve slugs via search_projects, then see query_lfx_semantic_layer for which dimension scopes each domain.

USE query_lfx_lens INSTEAD for non-metric narrative/"why", subprojects, maintainer trends, event sponsorships.`

const querySemanticLayerDescription = `Metrics: contributions, memberships, events, education, maintainers, health, country/region. ALWAYS use explore_lfx_semantic_layer first unless exact names are known; never guess. query_lfx_lens: narrative/"why"/carve-outs.

SYNTAX: metrics (required), CSV. Multiple metrics are outer-joined on shared dimensions; group_by only those; absent sides NULL. group_by qualified names: names for ranked lists; metric_time__year/quarter/month/week/day for trends; entities give raw IDs. where is MetricFlow: {{ Dimension('country__lf_region') }} = 'Europe'; {{ TimeDimension('metric_time','DAY') }} >= '2024-01-01'. Dates yyyy-mm-dd. order_by selected fields; - means descending. limit ceiling 500. current_* is active-only; total_contributors excludes bots—do not re-filter.

SCOPE: resolve slugs via search_projects first—stored slugs differ (Kubernetes='k8s', kernel='korg', PyTorch segment='ptproject'). Activities/contributions: {{ Dimension('activity_project_id__project_spine_slug') }} = '<slug>' selects project+descendants. It is the ONLY foundation scope (spine 'cncf'=58M activities; project_slug 'cncf'=1.4M) and REQUIRED for sums: insertions/deletions inflate 2–4x under any other filter. Memberships: asset_id__project_slug. Event registrations: registration_id__project_slug. Events/speakers/sponsorships have NO slug dimension: use event_id__project_name and the EXACT display name from get_dimension_values (e.g. 'Cloud Native Computing Foundation (CNCF)'). NEVER slice sponsorship metrics by asset_id__project_slug: one NULL row with all sponsorships. Maintainers: maintainer_key__cm_project_grandparents_slug; add is_lf_project=true to exclude non-LF. Health: health_metric_key__foundation_slug. Comparing: IN (...) + group_by the same dimension; never total across spine groups. 0 rows = misspelled literal—confirm with get_dimension_values.

WINDOWS: “last 12 months” = prior 365 complete UTC days: {{ TimeDimension('metric_time','DAY') }} >= start AND < today. YTD: >= 'YYYY-01-01'. Always state concrete dates used.`

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
	Where   string `json:"where,omitempty" jsonschema:"MetricFlow filter; this clause does the actual data filtering. Categorical: {{ Dimension('country__lf_region') }} = 'Europe'. Time: {{ TimeDimension('metric_time','DAY') }} >= '2024-01-01'. Dates are yyyy-mm-dd."`
	OrderBy string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields. Each must also appear in group_by or metrics. Prefix with - for descending, e.g. -current_membership_revenue."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum rows to return, ceiling 500. Use 10-20 for top-N questions and 50-100 for full breakdowns."`
}

// lensHelpTexts back the help action. These are tool results, so they carry no
// character budget — but they are a fallback, not a prerequisite: everything
// needed to compose a first query lives in exploreSemanticLayerDescription,
// querySemanticLayerDescription and the per-parameter descriptions.
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

	"query": lensQueryHelp,
}

// lensHelpOverview is returned by help with no target.
const lensHelpOverview = `LFX Insights Semantic Layer — how to use it

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

const lensQueryHelp = `query — run a governed metric query.

Parameters

  metrics   (required) comma-separated metric names.
  group_by  (optional) dimension qualified_names, comma-separated.
  where     (optional) one MetricFlow filter expression:
              categorical  {{ Dimension('country__lf_region') }} = 'Europe'
              time         {{ TimeDimension('metric_time','DAY') }} >= '2024-01-01'
              dates yyyy-mm-dd.
  order_by  (optional) selected group_by or metric fields; - for descending.
  limit     (optional) ceiling 500. Use 10-20 for top-N, 50-100 for breakdowns.

Multiple metrics outer-join on their shared dimensions, the only dimensions the
query may group by. A group present in only one domain has NULL for the other.
Use a name dimension for ranked lists; entities themselves return raw IDs. Add
metric_time__year (or __quarter, __month, __week, __day) for trends.

Pre-filtered metrics: current_* is active-only and total_contributors excludes
bots. Do not repeat those conditions.

SCOPE

Resolve slugs with search_projects first. Stored slugs are not everyday names:
Kubernetes is 'k8s', kernel is 'korg', and the PyTorch segment is 'ptproject'.
There is no separate project parameter; scope lives in where.

Activities and contributions: filter

  {{ Dimension('activity_project_id__project_spine_slug') }} = '<slug>'

This selects the project and everything under it. It is the ONLY correct scope
for foundations: the same 'cncf' literal represents 58M activities through the
spine but 1.4M through project_slug. It is also REQUIRED for sum metrics;
insertions and deletions inflate 2-4x under any other filter.

Domain-specific scope dimensions:

  memberships          asset_id__project_slug
  event registrations  registration_id__project_slug
  maintainers           maintainer_key__cm_project_grandparents_slug
  health                health_metric_key__foundation_slug

For maintainers, also add is_lf_project = true to exclude non-LF projects.

Events, speakers and sponsorships have NO slug dimension. Filter
  event_id__project_name with the EXACT display name returned by
get_dimension_values, for example 'Cloud Native Computing Foundation (CNCF)'.
NEVER slice sponsorship metrics by asset_id__project_slug: that returns one
NULL row containing all sponsorships.

To compare several projects or foundations, filter the correct scope dimension
with IN (...) and group_by that same dimension. Never report a total across
spine groups. Zero rows means the literal is probably misspelled; confirm it
with get_dimension_values.

WINDOWS

"Last 12 months" means the prior 365 complete UTC days: filter
{{ TimeDimension('metric_time','DAY') }} >= the start date AND
{{ TimeDimension('metric_time','DAY') }} < today's UTC date. YTD means
>= 'YYYY-01-01'. Always state the concrete dates used in the answer.

Worked examples

  CNCF contributors, last 12 months
    metrics   total_contributors
    where     {{ Dimension('activity_project_id__project_spine_slug') }} = 'cncf'
              AND {{ TimeDimension('metric_time','DAY') }} >= '<start YYYY-MM-DD>'
              AND {{ TimeDimension('metric_time','DAY') }} < '<today UTC YYYY-MM-DD>'

  Kubernetes code volume (slug resolved with search_projects)
    metrics   total_code_insertions
    where     {{ Dimension('activity_project_id__project_spine_slug') }} = 'k8s'

  Compare three foundations
    metrics   total_contributors
    group_by activity_project_id__project_spine_slug
    where    {{ Dimension('activity_project_id__project_spine_slug') }} IN ('cncf','lf-ai-foundation','openssf')

  CNCF membership count
    metrics   current_membership_count
    where     {{ Dimension('asset_id__project_slug') }} = 'cncf'

  Foundation to its projects (walk-down)
    metrics   total_contributors
    group_by activity_project_id__project_slug
    where    {{ Dimension('activity_project_id__project_spine_slug') }} = 'lf-ai-foundation'

The walk-down is flattened: all depths appear at leaf granularity, while an
intermediate node such as cncf shows only its directly attached activity. Use
this for counts only, never sums.

SURFACE RECONCILIATION

__project_slug and __segment_slug match what an Insights project page shows for
that slug: Insights scopes every page to one segment and never walks
hierarchies. __project_spine_slug matches PCC-style foundation rollups used in
executive reporting. Pick the scope by which surface the caller must reconcile
against.

Some foundations have twin Salesforce entities:

  risc-v-international / riscv
  cff / cloud-foundry
  opensearch-foundation / opensearch-project

If a total looks low, group_by the slug dimension and check for a twin.

"Direct children of X" and "sub-foundations of X" are not expressible today.
Say so rather than guessing.`

func handleExploreSemanticLayer(ctx context.Context, _ *mcp.CallToolRequest, args ExploreSemanticLayerArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
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
		return handleLensHelp(args.Target)
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
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Unknown action %q. Valid actions: list_metrics, get_dimensions, get_dimension_values, help. To run a query, use the query_lfx_semantic_layer tool.", args.Action)}},
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
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Unknown help target %q. Valid targets: list_metrics, get_dimensions, get_dimension_values, query", target)}},
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

	if args.Limit > 500 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: limit must be 500 or less"}},
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
