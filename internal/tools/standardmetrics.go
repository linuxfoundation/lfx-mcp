// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools implements the MCP tool handlers for the LFX MCP server.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// query_lfx_standard_metrics — governed standard metric recipes
// ---------------------------------------------------------------------------

// A standard metric is a saved query defined in lf-dbt's
// models/semantic_saved_queries.yml — the metrics, the grouping and the
// built-in filters are fixed there, and a caller chooses only the SLICE. The
// client-facing names below are the tool's vocabulary; the dbt saved-query
// names, the entity-qualified dimension names and the account/project
// modelling behind them stay inside this file, so a caller never has to know
// how the warehouse is shaped.
//
// Execution is proxied through the LFX Lens service's saved-query endpoint,
// which passes the name to the dbt Semantic Layer (dbt-sl-sdk: saved_query is
// mutually exclusive with metrics/group_by; where, order_by and limit apply on
// top). Every scope and window parameter therefore expands into a where entry
// rather than into a group_by. The one exception is a "combined" switch, which
// folds rows together by asking lens to drop group-by columns from the
// recipe's own definition.
//
// A recipe missing from the deployed manifest fails with a clear server-side
// error, which the handler returns verbatim — that is the signal the dbt side
// has not deployed it yet, not a reason to retry.
const standardMetricsDescription = `Run a governed standard metric: fixed metrics and grouping, so the same question re-run gives the same figure. When one matches the question, prefer it over explore + query.

METRICS
Memberships: members_and_dues_by_org, membership_tiers, new_members_by_year, membership_churn_by_year
Contributions: contributions_by_org, contributors_by_org, contributors_by_project
Maintainers: maintainers_by_org
Events: event_registrations_by_org
Training: training_enrollments_by_org

read_lfx_standard_metrics_guidance gives what each one answers, its result columns and its caveats.

ALWAYS resolve names first: project slugs from search_projects, organization names from search_b2b_orgs. A name those tools returned this session can be reused as is; never pass one that has not come back from them.

PARAMETERS
metric        required. One of the names above.
project       slug of ONE project or foundation.
subprojects   none | separate | combined. Default separate. none = that project's own bucket; separate = it plus everything under it, rows as the metric defines them; combined = those rows folded into one.
org           legal name of ONE organization.
subsidiaries  none | separate | combined. Default none. none = that account only; separate = it plus every account rolling up to it, one row each; combined = those rows folded into one.
since, until  yyyy-mm-dd. FLOW metrics: rows dated in the window. Omitted = all time.
as_of         yyyy-mm-dd. SNAPSHOT metrics: the state on that date. Omitted = today.
where         extra MetricFlow filter, one-hop names, ANDed with the above.
order_by      a result column, - prefix for descending.
limit         max rows, 1..500. Omitted returns every row.

A parameter the metric cannot honour returns an error naming the rule and the fix. Results carry compiled_sql.

Deck or briefing? Also read read_lfx_deck_building_guidance.`

// RegisterStandardMetrics registers the query_lfx_standard_metrics tool.
func RegisterStandardMetrics(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_lfx_standard_metrics",
		Description: standardMetricsDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Query LFX Standard Metrics",
			ReadOnlyHint: true,
		},
	}, handleStandardMetrics)
}

// StandardMetricsArgs defines the input for query_lfx_standard_metrics.
//
// Metric is the only required field, so under the schema compaction described
// on QuerySemanticLayerArgs its description is the one that survives intact
// alongside the tool description; the inventory of names and the contract a
// caller must not get wrong (fixed metrics and grouping, FLOW vs SNAPSHOT,
// resolve names first) is stated there as well as in the tool description,
// never only on an optional parameter.
//
// Limit is a pointer so an explicit 0 is distinguishable from an omitted
// value: omitted means every row, and 0 rows is not a question anyone asks,
// so it is rejected rather than silently read as "no limit".
type StandardMetricsArgs struct {
	Metric       string `json:"metric" jsonschema:"Required. One of: members_and_dues_by_org, membership_tiers, new_members_by_year, membership_churn_by_year, contributions_by_org, contributors_by_org, contributors_by_project, maintainers_by_org, event_registrations_by_org, training_enrollments_by_org. Each metric's metrics and grouping are fixed - there are no metrics/group_by parameters; slice it with project, org, their subprojects/subsidiaries switches, since/until or as_of, and where. FLOW metrics take since/until on their time axis; SNAPSHOT metrics take as_of. read_lfx_standard_metrics_guidance lists what each one answers, its result columns and its caveats."`
	Project      string `json:"project,omitempty" jsonschema:"Optional project scope: ONE slug from search_projects, of a project or of a foundation (e.g. cncf, k8s). Stored slugs are not everyday names - resolve it, never guess it."`
	Subprojects  string `json:"subprojects,omitempty" jsonschema:"How to treat what sits under project: none = that project's own bucket only; separate (default) = the project plus everything under it, rows as the metric defines them; combined = the project plus everything under it folded into one row (drops every project column the metric groups by)."`
	Org          string `json:"org,omitempty" jsonschema:"Optional organization scope: ONE legal name from search_b2b_orgs, in its stored spelling (e.g. Red Hat LLC). Resolve it, never guess it."`
	Subsidiaries string `json:"subsidiaries,omitempty" jsonschema:"How to treat the accounts that roll up to org: none (default) = that account only; separate = the account plus every account rolling up to it, one row each; combined = the account plus those accounts folded into one row. Without org, combined returns one row per parent organization. The parent link is one hop, so a combined figure covers direct subsidiaries, not their own acquisitions - disclose that next to it."`
	Since        string `json:"since,omitempty" jsonschema:"Optional window start, yyyy-mm-dd, on the metric's own time axis. FLOW metrics only. Omitted = all time."`
	Until        string `json:"until,omitempty" jsonschema:"Optional window end, yyyy-mm-dd, on the metric's own time axis. FLOW metrics only. Omitted = all time."`
	AsOf         string `json:"as_of,omitempty" jsonschema:"Optional as-of date, yyyy-mm-dd, for SNAPSHOT metrics. Only today's date is available until as-of history is deployed; omitted = today."`
	Where        string `json:"where,omitempty" jsonschema:"Optional MetricFlow filter applied on top of the metric and the scope parameters, e.g. {{ Dimension('account__account_name') }} = 'Red Hat LLC'. One-hop names only. Dates are yyyy-mm-dd. Check literals with explore_lfx_semantic_layer's get_dimension_values first - an unknown literal returns zero rows, not an error."`
	OrderBy      string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields, prefix with - for descending, e.g. -total_registrations. Use the result columns as they come back (account, parent_org, project, foundation, or a metric name)."`
	Limit        *int   `json:"limit,omitempty" jsonschema:"Maximum rows to return, 1..500. Use 10-20 for top-N questions. Omitting it returns EVERY row - set a limit unless you need the complete set."`
}

// metricShape says how a standard metric is measured in time: a FLOW metric
// counts rows whose date falls in a window (since/until); a SNAPSHOT metric
// reports the state on a date (as_of). Passing the wrong one is rejected
// rather than silently ignored, because a silently ignored window returns a
// confident wrong figure.
type metricShape string

const (
	metricFlow     metricShape = "FLOW"
	metricSnapshot metricShape = "SNAPSHOT"
)

// The three values the subprojects and subsidiaries switches take.
const (
	metricScopeNone     = "none"
	metricScopeSeparate = "separate"
	metricScopeCombined = "combined"
)

const (
	// defaultMetricTimeAxis is the axis since/until filter on unless the recipe
	// declares another one.
	defaultMetricTimeAxis = "metric_time"

	// The account lens most recipes carry: the account that holds the record
	// and its parent company.
	defaultMetricAccountDimension = "account__account_name"
	defaultMetricParentDimension  = "account__account_rollup_name"

	// metricNoParentLens marks a recipe whose model does not declare the account
	// entity, so it has no parent-company dimension at all. Its subsidiaries
	// switch is limited to none.
	metricNoParentLens = "none"
)

// metricRecipe is the per-metric half of the expansion: the dbt saved query
// behind the client-facing name, and everything the uniform parameters need
// that differs between recipes.
//
// groupBys is copied from lf-dbt models/semantic_saved_queries.yml (the
// deployed definitions) so a "combined" switch only ever asks lens to drop a
// column the recipe actually groups by.
type metricRecipe struct {
	// savedQuery is the dbt saved-query name sent to lens.
	savedQuery string
	shape      metricShape
	// timeAxis is the MetricFlow time dimension since/until filter on.
	timeAxis string
	// groupBys are the recipe's own group-by columns, as the yml declares
	// them and as they come back in the result: a TimeDimension group-by
	// carries its grain suffix (metric_time__year), so the name lens is
	// asked to drop is the name the recipe actually produces.
	groupBys []string
	// accountDimension is the dimension a single-account filter uses.
	accountDimension string
	// parentDimension is the parent-company dimension the subsidiaries
	// switch widens to. The sentinel metricNoParentLens means the recipe has
	// none, so subsidiaries other than none is rejected instead of silently
	// answered per account.
	parentDimension string
}

// metricRecipes maps the client-facing metric names to the DEPLOYED dbt saved
// queries. Group-bys, metrics and built-in filters come from lf-dbt
// models/semantic_saved_queries.yml on main; time axes verified live
// 2026-09-02.
//
// The last two entries are keyed by their dbt name: they stay callable but are
// off the routing surface (one metric and one name dimension each), and the
// semantic layer guidance documents them as two-line recipes.
var metricRecipes = map[string]metricRecipe{
	"members_and_dues_by_org": {
		savedQuery: "kpi_members_and_dues_by_account",
		shape:      metricSnapshot,
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"membership_tiers": {
		savedQuery: "kpi_membership_tier_split",
		shape:      metricSnapshot,
		groupBys:   []string{"asset_id__membership_tier"},
	},
	"new_members_by_year": {
		savedQuery: "kpi_new_members_by_year",
		shape:      metricFlow,
		groupBys:   []string{"metric_time__year"},
	},
	"membership_churn_by_year": {
		savedQuery: "kpi_membership_churn",
		shape:      metricFlow,
		groupBys:   []string{"metric_time__year"},
	},
	"contributions_by_org": {
		savedQuery: "kpi_contributions_by_org",
		shape:      metricFlow,
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"contributors_by_org": {
		savedQuery: "kpi_contributors_by_org",
		shape:      metricFlow,
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"contributors_by_project": {
		savedQuery: "kpi_contributors_by_project",
		shape:      metricFlow,
		groupBys:   []string{"project__foundation_slug", "project__foundation_name", "project__slug", "project__name"},
	},
	// Maintainers have no usable start date, so the roster is only readable
	// as a snapshot. silver_dim_maintainers does not declare the account
	// entity, so this recipe has NO parent-company lens: its only employer
	// column is the maintainer's own exact account name.
	"maintainers_by_org": {
		savedQuery:       "kpi_maintainers_by_org",
		shape:            metricSnapshot,
		groupBys:         []string{"maintainer_key__account_name"},
		accountDimension: "maintainer_key__account_name",
		parentDimension:  metricNoParentLens,
	},
	// Registrations are windowed on the EVENT start date, so a window means
	// "events in the window" and matches the recipe's event-year grouping.
	"event_registrations_by_org": {
		savedQuery: "kpi_event_registrations_by_org",
		shape:      metricFlow,
		timeAxis:   "registration_id__event_start_date",
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"training_enrollments_by_org": {
		savedQuery: "kpi_training_enrollments_by_org",
		shape:      metricFlow,
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"kpi_event_registrations": {
		savedQuery: "kpi_event_registrations",
		shape:      metricFlow,
		timeAxis:   "registration_id__event_start_date",
		groupBys:   []string{"event_id__event_name", "event_id__event_start_date__year", "project__foundation_slug"},
	},
	"kpi_training_enrollments": {
		savedQuery: "kpi_training_enrollments",
		shape:      metricFlow,
		groupBys:   []string{"enrollment_id__course_name", "enrollment_id__product_type", "enrollment_id__project_foundation_slug"},
	},
}

// standardMetricNames is the advertised inventory, in the order the guidance lists it. It
// is what an unknown name is answered with: the dbt saved-query names are an
// implementation detail a caller is never asked to learn.
var standardMetricNames = []string{
	"members_and_dues_by_org",
	"membership_tiers",
	"new_members_by_year",
	"membership_churn_by_year",
	"contributions_by_org",
	"contributors_by_org",
	"contributors_by_project",
	"maintainers_by_org",
	"event_registrations_by_org",
	"training_enrollments_by_org",
}

// metricRecipeFor resolves a caller's metric value — a client-facing name, or the
// dbt saved-query name of any deployed recipe — into its expansion entry with
// the defaults filled in. An unknown name resolves to nothing: guessing a
// shape for it would send a window or an as-of to a recipe that cannot honour
// it and get back a confident wrong figure.
func metricRecipeFor(name string) (metricRecipe, bool) {
	recipe, ok := metricRecipes[name]
	if !ok {
		for _, candidate := range metricRecipes {
			if candidate.savedQuery == name {
				recipe, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return metricRecipe{}, false
	}
	if recipe.timeAxis == "" {
		recipe.timeAxis = defaultMetricTimeAxis
	}
	if recipe.accountDimension == "" {
		recipe.accountDimension = defaultMetricAccountDimension
	}
	if recipe.parentDimension == "" {
		recipe.parentDimension = defaultMetricParentDimension
	}
	return recipe, true
}

// groupsBy reports whether the recipe's deployed definition carries this
// group-by column, so a drop is never sent for a column that is not there.
func (r metricRecipe) groupsBy(name string) bool {
	for _, groupBy := range r.groupBys {
		if groupBy == name {
			return true
		}
	}
	return false
}

// metricRequest is the lens payload the uniform parameters expand into.
type metricRequest struct {
	savedQuery string
	where      []string
	// dropGroupBys names the recipe group-by columns lens removes before it
	// runs the query: that is how a "combined" switch folds rows together
	// without a second saved query.
	dropGroupBys []string
	orderBy      []string
	limit        int
}

// metricDateLayout is the only date format the tool accepts; anything else is
// rejected rather than passed through, because MetricFlow reads an
// unparseable literal as zero rows.
const metricDateLayout = "2006-01-02"

// metricLiteral escapes a value for a single-quoted MetricFlow literal.
func metricLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// metricDimensionFilter renders an equality filter on a categorical dimension.
func metricDimensionFilter(dimension, value string) string {
	return fmt.Sprintf("{{ Dimension('%s') }} = '%s'", dimension, metricLiteral(value))
}

// metricAnyDimensionFilter renders an OR over several dimensions holding the same
// value — one where entry, so it still ANDs with the rest of the list.
func metricAnyDimensionFilter(value string, dimensions ...string) string {
	clauses := make([]string, 0, len(dimensions))
	for _, dimension := range dimensions {
		clauses = append(clauses, metricDimensionFilter(dimension, value))
	}
	return "(" + strings.Join(clauses, " OR ") + ")"
}

// metricTimeFilter renders a day-grain bound on a recipe's time axis.
func metricTimeFilter(axis, operator, date string) string {
	return fmt.Sprintf("{{ TimeDimension('%s','DAY') }} %s '%s'", axis, operator, metricLiteral(date))
}

// metricScope validates a subprojects/subsidiaries switch and applies its
// default. An unknown value is rejected: silently treating it as the default
// answers a different question from the one asked.
func metricScope(label, raw, fallback string) (string, string) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "":
		return fallback, ""
	case metricScopeNone, metricScopeSeparate, metricScopeCombined:
		return value, ""
	default:
		return "", fmt.Sprintf("Error: %s must be none, separate or combined, got %q. Omit it for the default (%s).", label, raw, fallback)
	}
}

// buildMetricRequest expands the uniform parameters into the lens payload, or
// returns the rejection message the caller gets instead. today is passed in so
// the as-of rule is testable; callers pass time.Now().UTC().
//
// Scope and window become separate entries in the where list (lens ANDs the
// list) rather than a group_by: the recipe's grouping is fixed in lf-dbt and
// dbt-sl-sdk forbids group_by alongside saved_query. A combined switch instead
// names the group-by columns lens drops from the recipe's own definition.
func buildMetricRequest(args StandardMetricsArgs, today time.Time) (metricRequest, string) {
	name := strings.TrimSpace(args.Metric)
	if name == "" {
		return metricRequest{}, fmt.Sprintf("Error: metric is required. Pick one of: %s.", strings.Join(standardMetricNames, ", "))
	}
	recipe, known := metricRecipeFor(name)
	if !known {
		return metricRequest{}, fmt.Sprintf("Error: unknown metric %q. Valid names: %s. read_lfx_standard_metrics_guidance says what each one answers.", name, strings.Join(standardMetricNames, ", "))
	}
	// A limit that was sent is validated; only an omitted one means "every
	// row". limit=0 is a value the caller chose and cannot have meant, so it
	// is rejected rather than read as the omitted case.
	if args.Limit != nil && (*args.Limit < 1 || *args.Limit > 500) {
		return metricRequest{}, fmt.Sprintf("Error: limit must be between 1 and 500, got %d. Omit it entirely to return every row.", *args.Limit)
	}

	subprojects, rejection := metricScope("subprojects", args.Subprojects, metricScopeSeparate)
	if rejection != "" {
		return metricRequest{}, rejection
	}
	subsidiaries, rejection := metricScope("subsidiaries", args.Subsidiaries, metricScopeNone)
	if rejection != "" {
		return metricRequest{}, rejection
	}
	if subsidiaries != metricScopeNone && recipe.parentDimension == metricNoParentLens {
		return metricRequest{}, fmt.Sprintf("Error: %s has no parent-company lens yet, so subsidiaries must be none. Name one employer with org, or filter where {{ Dimension('%s') }} = '<exact account name>', and present the figure as per-account, not per parent.", name, recipe.accountDimension)
	}

	since := strings.TrimSpace(args.Since)
	until := strings.TrimSpace(args.Until)
	asOf := strings.TrimSpace(args.AsOf)

	if recipe.shape == metricSnapshot && (since != "" || until != "") {
		return metricRequest{}, fmt.Sprintf("Error: %s is a SNAPSHOT metric: it reports the state on a date, so since/until do not apply. Use as_of, or pick a FLOW metric to count rows in a window.", name)
	}
	if recipe.shape == metricFlow && asOf != "" {
		return metricRequest{}, fmt.Sprintf("Error: %s is a FLOW metric measured on %s: it counts rows in a window, so as_of does not apply. Use since/until.", name, recipe.timeAxis)
	}
	for _, field := range []struct{ label, value string }{{"since", since}, {"until", until}, {"as_of", asOf}} {
		if field.value == "" {
			continue
		}
		if _, err := time.Parse(metricDateLayout, field.value); err != nil {
			return metricRequest{}, fmt.Sprintf("Error: %s must be a yyyy-mm-dd date, got %q.", field.label, field.value)
		}
	}
	if asOf != "" && asOf != today.Format(metricDateLayout) {
		return metricRequest{}, fmt.Sprintf("Error: %s is a SNAPSHOT metric and as-of history is not deployed yet; one call per period once it is. Only today (%s) can be read, so omit as_of or pass today's date.", name, today.Format(metricDateLayout))
	}

	req := metricRequest{savedQuery: recipe.savedQuery}
	if args.Limit != nil {
		req.limit = *args.Limit
	}

	if project := strings.TrimSpace(args.Project); project != "" {
		if subprojects == metricScopeNone {
			req.where = append(req.where, metricDimensionFilter("project__slug", project))
		} else {
			// One slug can name a project, a foundation or an umbrella
			// node, and the caller is not asked to know which: the OR
			// covers the node's own bucket, a whole foundation, and an
			// umbrella's direct children.
			req.where = append(req.where, metricAnyDimensionFilter(project,
				"project__slug", "project__foundation_slug", "project__parent_project_slug"))
		}
	}
	if org := strings.TrimSpace(args.Org); org != "" {
		if subsidiaries == metricScopeNone {
			req.where = append(req.where, metricDimensionFilter(recipe.accountDimension, org))
		} else {
			// The account's own row sits under its parent, so widening to
			// subsidiaries needs both columns: the parent's own row and
			// every row whose parent is the named account.
			req.where = append(req.where, metricAnyDimensionFilter(org,
				recipe.accountDimension, recipe.parentDimension))
		}
	}
	if since != "" {
		req.where = append(req.where, metricTimeFilter(recipe.timeAxis, ">=", since))
	}
	if until != "" {
		req.where = append(req.where, metricTimeFilter(recipe.timeAxis, "<=", until))
	}
	if where := strings.TrimSpace(args.Where); where != "" {
		req.where = append(req.where, where)
	}

	req.dropGroupBys = metricDropGroupBys(recipe, args, subprojects, subsidiaries)
	req.orderBy = metricOrderBy(recipe, req.dropGroupBys, args.OrderBy)

	return req, ""
}

// metricDropGroupBys names the group-by columns a combined switch folds away.
// Only columns the deployed recipe actually groups by are sent: dropping a
// column a recipe does not have is a request lens cannot honour.
func metricDropGroupBys(recipe metricRecipe, args StandardMetricsArgs, subprojects, subsidiaries string) []string {
	var drops []string
	if subsidiaries == metricScopeCombined {
		// With org, both organization columns go and the folded rows are
		// that organization with its subsidiaries. Without org, only the
		// account column goes, which leaves one row per parent company.
		candidates := []string{recipe.accountDimension}
		if strings.TrimSpace(args.Org) != "" {
			candidates = append(candidates, recipe.parentDimension)
		}
		for _, candidate := range candidates {
			if recipe.groupsBy(candidate) {
				drops = append(drops, candidate)
			}
		}
	}
	if subprojects == metricScopeCombined {
		// Every project column the recipe groups by goes, whichever they
		// are: a recipe grouped only by project__foundation_slug folds
		// that away just as the per-project one folds its four. A recipe
		// with no project column is already one row per organization,
		// tier or year, and combined is the filter alone.
		for _, groupBy := range recipe.groupBys {
			if strings.HasPrefix(groupBy, "project__") {
				drops = append(drops, groupBy)
			}
		}
	}
	return drops
}

// ---------------------------------------------------------------------------
// Result vocabulary
// ---------------------------------------------------------------------------

// metricColumnLabels renames the entity-qualified result columns to the words
// the tool's own parameters use. The entity prefix is warehouse modelling; a
// caller asked for an organization and a project and gets back columns with
// those names.
//
// maintainer_key__account_name is the maintainer model's own employer column,
// carried by a different entity because that model does not declare the
// account one — but it answers the same question, so every *_by_org metric
// returns the organization column as `account`.
var metricColumnLabels = map[string]string{
	"account__account_name":        "account",
	"account__account_rollup_name": "parent_org",
	"maintainer_key__account_name": "account",
	"project__slug":                "project",
	"project__foundation_slug":     "foundation",
	"project__name":                "project_name",
	"project__foundation_name":     "foundation_name",
}

// metricClientColumn is the client vocabulary for one result column: the explicit
// label if there is one, else the column with its entity prefix stripped.
// Anything else is left exactly as it came back.
func metricClientColumn(name string) string {
	if label, ok := metricColumnLabels[name]; ok {
		return label
	}
	for _, prefix := range []string{"account__", "project__"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	return name
}

// metricRelabelKeys maps the keys of one JSON object to the client vocabulary,
// dropping any rename whose label would collide with another key of the same
// object: an ambiguous label hides which column a value came from, so both
// columns keep their qualified names.
func metricRelabelKeys(keys []string) map[string]string {
	counts := make(map[string]int, len(keys))
	for _, key := range keys {
		counts[metricClientColumn(key)]++
	}
	labels := make(map[string]string, len(keys))
	for _, key := range keys {
		label := metricClientColumn(key)
		if label != key && counts[label] > 1 {
			label = key
		}
		labels[key] = label
	}
	return labels
}

// metricOrderBy maps the caller's sort fields back to the names the semantic
// layer knows, so order_by takes the same column names the results carry. A
// field that is not one of the recipe's surviving group-by columns — a metric
// name, or a qualified name the caller already knew — is passed through
// untouched.
func metricOrderBy(recipe metricRecipe, dropped []string, orderBy string) []string {
	fields := parseCSV(orderBy)
	if len(fields) == 0 {
		return nil
	}

	remaining := make([]string, 0, len(recipe.groupBys))
	for _, groupBy := range recipe.groupBys {
		isDropped := false
		for _, drop := range dropped {
			if drop == groupBy {
				isDropped = true
				break
			}
		}
		if !isDropped {
			remaining = append(remaining, groupBy)
		}
	}
	qualified := make(map[string]string, len(remaining))
	for column, label := range metricRelabelKeys(remaining) {
		if label == column {
			continue
		}
		if _, clash := qualified[label]; clash {
			continue
		}
		qualified[label] = column
	}

	out := make([]string, 0, len(fields))
	for _, field := range fields {
		descending := strings.HasPrefix(field, "-")
		bare := strings.TrimPrefix(field, "-")
		if column, ok := qualified[bare]; ok {
			bare = column
		}
		if descending {
			bare = "-" + bare
		}
		out = append(out, bare)
	}
	return out
}

// metricRelabelJSON rewrites the qualified column names in a lens response to the
// client vocabulary, leaving every value — compiled_sql included — untouched
// and the document's key order intact. A body that is not JSON, or that this
// cannot rewrite, is returned exactly as it arrived.
func metricRelabelJSON(body []byte) []byte {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var out bytes.Buffer
	if err := metricRewriteJSONValue(decoder, &out); err != nil {
		return body
	}
	return out.Bytes()
}

// metricRewriteJSONValue copies one JSON value from the decoder to out, renaming
// object keys and any string that is itself a column name (a response that
// lists its columns names them the same way a row keys them).
func metricRewriteJSONValue(decoder *json.Decoder, out *bytes.Buffer) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delim, isDelim := token.(json.Delim)
	if !isDelim {
		if text, isString := token.(string); isString {
			return metricWriteJSONToken(metricClientColumn(text), out)
		}
		return metricWriteJSONToken(token, out)
	}

	switch delim {
	case '{':
		type member struct {
			key   string
			value []byte
		}
		var members []member
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, isString := keyToken.(string)
			if !isString {
				return fmt.Errorf("unexpected object key %v", keyToken)
			}
			var value bytes.Buffer
			if err := metricRewriteJSONValue(decoder, &value); err != nil {
				return err
			}
			members = append(members, member{key: key, value: value.Bytes()})
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}

		keys := make([]string, 0, len(members))
		for _, m := range members {
			keys = append(keys, m.key)
		}
		labels := metricRelabelKeys(keys)

		out.WriteByte('{')
		for i, m := range members {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := metricWriteJSONToken(labels[m.key], out); err != nil {
				return err
			}
			out.WriteByte(':')
			out.Write(m.value)
		}
		out.WriteByte('}')
		return nil
	case '[':
		out.WriteByte('[')
		for i := 0; decoder.More(); i++ {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := metricRewriteJSONValue(decoder, out); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
		out.WriteByte(']')
		return nil
	default:
		return fmt.Errorf("unexpected delimiter %v", delim)
	}
}

// metricWriteJSONToken encodes one scalar (or object key) without HTML escaping,
// so SQL comparison operators inside compiled_sql stay readable.
func metricWriteJSONToken(value any, out *bytes.Buffer) error {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	out.Write(bytes.TrimRight(encoded.Bytes(), "\n"))
	return nil
}

func handleStandardMetrics(ctx context.Context, _ *mcp.CallToolRequest, args StandardMetricsArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	req, rejection := buildMetricRequest(args, time.Now().UTC())
	if rejection != "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: rejection}},
			IsError: true,
		}, nil, nil
	}

	reqBody := map[string]any{
		"saved_query": req.savedQuery,
	}
	if len(req.where) > 0 {
		reqBody["where"] = req.where
	}
	if len(req.dropGroupBys) > 0 {
		reqBody["drop_group_bys"] = req.dropGroupBys
	}
	if len(req.orderBy) > 0 {
		reqBody["order_by"] = req.orderBy
	}
	if req.limit > 0 {
		reqBody["limit"] = req.limit
	}

	body, statusCode, err := lensConfig.ServiceClient.PostJSON(ctx, "/lfx-lens/semantic-layer/saved-query", reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("standard metric call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		// Lens errors reach the caller verbatim: an invented friendlier
		// message would have the model correct the wrong thing, and a
		// recipe the server does not know is the deployment state, not a
		// caller mistake.
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))}},
			IsError: true,
		}, nil, nil
	}

	return lensPrettyJSON(metricRelabelJSON(body))
}
