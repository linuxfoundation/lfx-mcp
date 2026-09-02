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
// query_lfx_kpis — governed KPI recipes
// ---------------------------------------------------------------------------

// A KPI is a saved query defined in lf-dbt's models/semantic_saved_queries.yml
// — the metrics, the grouping and the built-in filters are fixed there, and a
// caller chooses only the SLICE. The client-facing names below are the tool's
// vocabulary; the dbt saved-query names, the entity-qualified dimension names
// and the account/project modelling behind them stay inside this file, so a
// caller never has to know how the warehouse is shaped.
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
const kpisDescription = `Run a governed KPI: fixed metrics and grouping, so the same question re-run gives the same figure. When a KPI matches the question, prefer it over explore + query.

ALWAYS resolve names first: project slugs from search_projects, organization names from search_b2b_orgs. A name those tools returned this session can be reused as is; never pass one that has not come back from them.

PARAMETERS
kpi           required. KPI name; read read_lfx_kpi_guidance for the inventory.
project       slug of ONE project or foundation.
subprojects   none | separate | combined. Default separate. none = that project's own bucket; separate = it plus everything under it, rows as the KPI defines them; combined = those rows folded into one.
org           legal name of ONE organization.
subsidiaries  none | separate | combined. Default none. none = that account only; separate = it plus every account rolling up to it, one row each; combined = those rows folded into one.
since, until  yyyy-mm-dd. FLOW KPIs: rows dated in the window. Omitted = all time.
as_of         yyyy-mm-dd. SNAPSHOT KPIs: the state on that date. Omitted = today.
where         extra MetricFlow filter, one-hop names, ANDed with the above.
order_by      a result column, - prefix for descending.
limit         max rows, 1..500. Omitted returns every row.

A parameter the KPI cannot honour returns an error naming the rule and the fix. Results carry compiled_sql.

Deck or briefing? Also read read_lfx_deck_building_guidance.`

// RegisterKPIs registers the query_lfx_kpis tool.
func RegisterKPIs(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_lfx_kpis",
		Description: kpisDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Query LFX KPIs",
			ReadOnlyHint: true,
		},
	}, handleKPIs)
}

// KPIArgs defines the input for query_lfx_kpis.
//
// KPI is the only required field, so under the schema compaction described on
// QuerySemanticLayerArgs its description is the one that survives intact
// alongside the tool description; the inventory of names and the contract a
// caller must not get wrong (fixed metrics and grouping, FLOW vs SNAPSHOT,
// resolve names first) is stated there as well as in the tool description,
// never only on an optional parameter.
type KPIArgs struct {
	KPI          string `json:"kpi" jsonschema:"Required. One of: members_and_dues_by_org, membership_tiers, new_members_by_year, membership_churn_by_year, contributions_by_org, contributors_by_org, contributors_by_project, maintainers_by_org, event_registrations_by_org, training_enrollments_by_org. Each KPI's metrics and grouping are fixed - there are no metrics/group_by parameters; slice it with project, org, their subprojects/subsidiaries switches, since/until or as_of, and where. FLOW KPIs take since/until on their time axis; SNAPSHOT KPIs take as_of. read_lfx_kpi_guidance lists what each one answers, its result columns and its caveats."`
	Project      string `json:"project,omitempty" jsonschema:"Optional project scope: ONE slug from search_projects, of a project or of a foundation (e.g. cncf, k8s). Stored slugs are not everyday names - resolve it, never guess it."`
	Subprojects  string `json:"subprojects,omitempty" jsonschema:"How to treat what sits under project: none = that project's own bucket only; separate (default) = the project plus everything under it, rows as the KPI defines them; combined = the project plus everything under it folded into one row (changes the shape only of per-project KPIs)."`
	Org          string `json:"org,omitempty" jsonschema:"Optional organization scope: ONE legal name from search_b2b_orgs, in its stored spelling (e.g. Red Hat LLC). Resolve it, never guess it."`
	Subsidiaries string `json:"subsidiaries,omitempty" jsonschema:"How to treat the accounts that roll up to org: none (default) = that account only; separate = the account plus every account rolling up to it, one row each; combined = the account plus those accounts folded into one row. Without org, combined returns one row per parent organization. The parent link is one hop, so a combined figure covers direct subsidiaries, not their own acquisitions - disclose that next to it."`
	Since        string `json:"since,omitempty" jsonschema:"Optional window start, yyyy-mm-dd, on the KPI's own time axis. FLOW KPIs only. Omitted = all time."`
	Until        string `json:"until,omitempty" jsonschema:"Optional window end, yyyy-mm-dd, on the KPI's own time axis. FLOW KPIs only. Omitted = all time."`
	AsOf         string `json:"as_of,omitempty" jsonschema:"Optional as-of date, yyyy-mm-dd, for SNAPSHOT KPIs. Only today's date is available until as-of history is deployed; omitted = today."`
	Where        string `json:"where,omitempty" jsonschema:"Optional MetricFlow filter applied on top of the KPI and the scope parameters, e.g. {{ Dimension('account__account_name') }} = 'Red Hat LLC'. One-hop names only. Dates are yyyy-mm-dd. Check literals with explore_lfx_semantic_layer's get_dimension_values first - an unknown literal returns zero rows, not an error."`
	OrderBy      string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields, prefix with - for descending, e.g. -total_registrations. Use the result columns as they come back (account, parent_org, project, foundation, or a metric name)."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum rows to return, 1..500. Use 10-20 for top-N questions. Omitting it returns EVERY row - set a limit unless you need the complete set."`
}

// kpiShape says how a KPI is measured in time: a FLOW KPI counts rows whose
// date falls in a window (since/until); a SNAPSHOT KPI reports the state on a
// date (as_of). Passing the wrong one is rejected rather than silently
// ignored, because a silently ignored window returns a confident wrong figure.
type kpiShape string

const (
	kpiFlow     kpiShape = "FLOW"
	kpiSnapshot kpiShape = "SNAPSHOT"
)

// The three values the subprojects and subsidiaries switches take.
const (
	kpiScopeNone     = "none"
	kpiScopeSeparate = "separate"
	kpiScopeCombined = "combined"
)

const (
	// defaultKPITimeAxis is the axis since/until filter on unless the recipe
	// declares another one.
	defaultKPITimeAxis = "metric_time"

	// The account lens most recipes carry: the account that holds the record
	// and its parent company.
	defaultKPIAccountDimension = "account__account_name"
	defaultKPIParentDimension  = "account__account_rollup_name"

	// kpiNoParentLens marks a recipe whose model does not declare the account
	// entity, so it has no parent-company dimension at all. Its subsidiaries
	// switch is limited to none.
	kpiNoParentLens = "none"
)

// kpiRecipe is the per-KPI half of the expansion: the dbt saved query behind
// the client-facing name, and everything the uniform parameters need that
// differs between recipes.
//
// groupBys is copied from lf-dbt models/semantic_saved_queries.yml (the
// deployed definitions) so a "combined" switch only ever asks lens to drop a
// column the recipe actually groups by.
type kpiRecipe struct {
	// savedQuery is the dbt saved-query name sent to lens.
	savedQuery string
	shape      kpiShape
	// timeAxis is the MetricFlow time dimension since/until filter on.
	timeAxis string
	// groupBys are the recipe's own group-by columns, verbatim from the yml.
	groupBys []string
	// accountDimension is the dimension a single-account filter uses.
	accountDimension string
	// parentDimension is the parent-company dimension the subsidiaries
	// switch widens to. The sentinel kpiNoParentLens means the recipe has
	// none, so subsidiaries other than none is rejected instead of silently
	// answered per account.
	parentDimension string
	// projectGrain marks a KPI whose rows are per project, so that
	// subprojects=combined folds them by dropping its project group-bys.
	projectGrain bool
}

// kpiRecipes maps the client-facing KPI names to the DEPLOYED dbt saved
// queries. Group-bys, metrics and built-in filters come from lf-dbt
// models/semantic_saved_queries.yml on main; time axes verified live
// 2026-09-02.
//
// The last two entries are keyed by their dbt name: they stay callable but are
// off the routing surface (one metric and one name dimension each), and the
// semantic layer guidance documents them as two-line recipes.
var kpiRecipes = map[string]kpiRecipe{
	"members_and_dues_by_org": {
		savedQuery: "kpi_members_and_dues_by_account",
		shape:      kpiSnapshot,
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"membership_tiers": {
		savedQuery: "kpi_membership_tier_split",
		shape:      kpiSnapshot,
		groupBys:   []string{"asset_id__membership_tier"},
	},
	"new_members_by_year": {
		savedQuery: "kpi_new_members_by_year",
		shape:      kpiFlow,
		groupBys:   []string{"metric_time"},
	},
	"membership_churn_by_year": {
		savedQuery: "kpi_membership_churn",
		shape:      kpiFlow,
		groupBys:   []string{"metric_time"},
	},
	"contributions_by_org": {
		savedQuery: "kpi_contributions_by_org",
		shape:      kpiFlow,
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"contributors_by_org": {
		savedQuery: "kpi_contributors_by_org",
		shape:      kpiFlow,
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"contributors_by_project": {
		savedQuery:   "kpi_contributors_by_project",
		shape:        kpiFlow,
		groupBys:     []string{"project__foundation_slug", "project__foundation_name", "project__slug", "project__name"},
		projectGrain: true,
	},
	// Maintainers have no usable start date, so the roster is only readable
	// as a snapshot. silver_dim_maintainers does not declare the account
	// entity, so this recipe has NO parent-company lens: its only employer
	// column is the maintainer's own exact account name.
	"maintainers_by_org": {
		savedQuery:       "kpi_maintainers_by_org",
		shape:            kpiSnapshot,
		groupBys:         []string{"maintainer_key__account_name"},
		accountDimension: "maintainer_key__account_name",
		parentDimension:  kpiNoParentLens,
	},
	// Registrations are windowed on the EVENT start date, so a window means
	// "events in the window" and matches the recipe's event-year grouping.
	"event_registrations_by_org": {
		savedQuery: "kpi_event_registrations_by_org",
		shape:      kpiFlow,
		timeAxis:   "registration_id__event_start_date",
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"training_enrollments_by_org": {
		savedQuery: "kpi_training_enrollments_by_org",
		shape:      kpiFlow,
		groupBys:   []string{"account__account_name", "account__account_rollup_name"},
	},
	"kpi_event_registrations": {
		savedQuery: "kpi_event_registrations",
		shape:      kpiFlow,
		timeAxis:   "registration_id__event_start_date",
		groupBys:   []string{"event_id__event_name", "event_id__event_start_date", "project__foundation_slug"},
	},
	"kpi_training_enrollments": {
		savedQuery: "kpi_training_enrollments",
		shape:      kpiFlow,
		groupBys:   []string{"enrollment_id__course_name", "enrollment_id__product_type", "enrollment_id__project_foundation_slug"},
	},
}

// kpiNames is the advertised inventory, in the order the guidance lists it. It
// is what an unknown name is answered with: the dbt saved-query names are an
// implementation detail a caller is never asked to learn.
var kpiNames = []string{
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

// kpiRecipeFor resolves a caller's kpi value — a client-facing name, or the
// dbt saved-query name of any deployed recipe — into its expansion entry with
// the defaults filled in. An unknown name resolves to nothing: guessing a
// shape for it would send a window or an as-of to a recipe that cannot honour
// it and get back a confident wrong figure.
func kpiRecipeFor(name string) (kpiRecipe, bool) {
	recipe, ok := kpiRecipes[name]
	if !ok {
		for _, candidate := range kpiRecipes {
			if candidate.savedQuery == name {
				recipe, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return kpiRecipe{}, false
	}
	if recipe.timeAxis == "" {
		recipe.timeAxis = defaultKPITimeAxis
	}
	if recipe.accountDimension == "" {
		recipe.accountDimension = defaultKPIAccountDimension
	}
	if recipe.parentDimension == "" {
		recipe.parentDimension = defaultKPIParentDimension
	}
	return recipe, true
}

// groupsBy reports whether the recipe's deployed definition carries this
// group-by column, so a drop is never sent for a column that is not there.
func (r kpiRecipe) groupsBy(name string) bool {
	for _, groupBy := range r.groupBys {
		if groupBy == name {
			return true
		}
	}
	return false
}

// kpiRequest is the lens payload the uniform parameters expand into.
type kpiRequest struct {
	savedQuery string
	where      []string
	// dropGroupBys names the recipe group-by columns lens removes before it
	// runs the query: that is how a "combined" switch folds rows together
	// without a second saved query.
	dropGroupBys []string
	orderBy      []string
	limit        int
}

// kpiDateLayout is the only date format the tool accepts; anything else is
// rejected rather than passed through, because MetricFlow reads an
// unparseable literal as zero rows.
const kpiDateLayout = "2006-01-02"

// kpiLiteral escapes a value for a single-quoted MetricFlow literal.
func kpiLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// kpiDimensionFilter renders an equality filter on a categorical dimension.
func kpiDimensionFilter(dimension, value string) string {
	return fmt.Sprintf("{{ Dimension('%s') }} = '%s'", dimension, kpiLiteral(value))
}

// kpiAnyDimensionFilter renders an OR over several dimensions holding the same
// value — one where entry, so it still ANDs with the rest of the list.
func kpiAnyDimensionFilter(value string, dimensions ...string) string {
	clauses := make([]string, 0, len(dimensions))
	for _, dimension := range dimensions {
		clauses = append(clauses, kpiDimensionFilter(dimension, value))
	}
	return "(" + strings.Join(clauses, " OR ") + ")"
}

// kpiTimeFilter renders a day-grain bound on a recipe's time axis.
func kpiTimeFilter(axis, operator, date string) string {
	return fmt.Sprintf("{{ TimeDimension('%s','DAY') }} %s '%s'", axis, operator, kpiLiteral(date))
}

// kpiScope validates a subprojects/subsidiaries switch and applies its
// default. An unknown value is rejected: silently treating it as the default
// answers a different question from the one asked.
func kpiScope(label, raw, fallback string) (string, string) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "":
		return fallback, ""
	case kpiScopeNone, kpiScopeSeparate, kpiScopeCombined:
		return value, ""
	default:
		return "", fmt.Sprintf("Error: %s must be none, separate or combined, got %q. Omit it for the default (%s).", label, raw, fallback)
	}
}

// buildKPIRequest expands the uniform parameters into the lens payload, or
// returns the rejection message the caller gets instead. today is passed in so
// the as-of rule is testable; callers pass time.Now().UTC().
//
// Scope and window become separate entries in the where list (lens ANDs the
// list) rather than a group_by: the recipe's grouping is fixed in lf-dbt and
// dbt-sl-sdk forbids group_by alongside saved_query. A combined switch instead
// names the group-by columns lens drops from the recipe's own definition.
func buildKPIRequest(args KPIArgs, today time.Time) (kpiRequest, string) {
	name := strings.TrimSpace(args.KPI)
	if name == "" {
		return kpiRequest{}, fmt.Sprintf("Error: kpi is required. Pick one of: %s.", strings.Join(kpiNames, ", "))
	}
	recipe, known := kpiRecipeFor(name)
	if !known {
		return kpiRequest{}, fmt.Sprintf("Error: unknown kpi %q. Valid names: %s. read_lfx_kpi_guidance says what each one answers.", name, strings.Join(kpiNames, ", "))
	}
	if args.Limit < 0 || args.Limit > 500 {
		return kpiRequest{}, fmt.Sprintf("Error: limit must be between 1 and 500, got %d. Omit it entirely to return every row.", args.Limit)
	}

	subprojects, rejection := kpiScope("subprojects", args.Subprojects, kpiScopeSeparate)
	if rejection != "" {
		return kpiRequest{}, rejection
	}
	subsidiaries, rejection := kpiScope("subsidiaries", args.Subsidiaries, kpiScopeNone)
	if rejection != "" {
		return kpiRequest{}, rejection
	}
	if subsidiaries != kpiScopeNone && recipe.parentDimension == kpiNoParentLens {
		return kpiRequest{}, fmt.Sprintf("Error: %s has no parent-company lens yet, so subsidiaries must be none. Name one employer with org, or filter where {{ Dimension('%s') }} = '<exact account name>', and present the figure as per-account, not per parent.", name, recipe.accountDimension)
	}

	since := strings.TrimSpace(args.Since)
	until := strings.TrimSpace(args.Until)
	asOf := strings.TrimSpace(args.AsOf)

	if recipe.shape == kpiSnapshot && (since != "" || until != "") {
		return kpiRequest{}, fmt.Sprintf("Error: %s is a SNAPSHOT KPI: it reports the state on a date, so since/until do not apply. Use as_of, or pick a FLOW KPI to count rows in a window.", name)
	}
	if recipe.shape == kpiFlow && asOf != "" {
		return kpiRequest{}, fmt.Sprintf("Error: %s is a FLOW KPI measured on %s: it counts rows in a window, so as_of does not apply. Use since/until.", name, recipe.timeAxis)
	}
	for _, field := range []struct{ label, value string }{{"since", since}, {"until", until}, {"as_of", asOf}} {
		if field.value == "" {
			continue
		}
		if _, err := time.Parse(kpiDateLayout, field.value); err != nil {
			return kpiRequest{}, fmt.Sprintf("Error: %s must be a yyyy-mm-dd date, got %q.", field.label, field.value)
		}
	}
	if asOf != "" && asOf != today.Format(kpiDateLayout) {
		return kpiRequest{}, fmt.Sprintf("Error: %s is a SNAPSHOT KPI and as-of history is not deployed yet; one call per period once it is. Only today (%s) can be read, so omit as_of or pass today's date.", name, today.Format(kpiDateLayout))
	}

	req := kpiRequest{savedQuery: recipe.savedQuery, limit: args.Limit}

	if project := strings.TrimSpace(args.Project); project != "" {
		if subprojects == kpiScopeNone {
			req.where = append(req.where, kpiDimensionFilter("project__slug", project))
		} else {
			// One slug can name a project, a foundation or an umbrella
			// node, and the caller is not asked to know which: the OR
			// covers the node's own bucket, a whole foundation, and an
			// umbrella's direct children.
			req.where = append(req.where, kpiAnyDimensionFilter(project,
				"project__slug", "project__foundation_slug", "project__parent_project_slug"))
		}
	}
	if org := strings.TrimSpace(args.Org); org != "" {
		if subsidiaries == kpiScopeNone {
			req.where = append(req.where, kpiDimensionFilter(recipe.accountDimension, org))
		} else {
			// The account's own row sits under its parent, so widening to
			// subsidiaries needs both columns: the parent's own row and
			// every row whose parent is the named account.
			req.where = append(req.where, kpiAnyDimensionFilter(org,
				recipe.accountDimension, recipe.parentDimension))
		}
	}
	if since != "" {
		req.where = append(req.where, kpiTimeFilter(recipe.timeAxis, ">=", since))
	}
	if until != "" {
		req.where = append(req.where, kpiTimeFilter(recipe.timeAxis, "<=", until))
	}
	if where := strings.TrimSpace(args.Where); where != "" {
		req.where = append(req.where, where)
	}

	req.dropGroupBys = kpiDropGroupBys(recipe, args, subprojects, subsidiaries)
	req.orderBy = kpiOrderBy(recipe, req.dropGroupBys, args.OrderBy)

	return req, ""
}

// kpiDropGroupBys names the group-by columns a combined switch folds away.
// Only columns the deployed recipe actually groups by are sent: dropping a
// column a recipe does not have is a request lens cannot honour.
func kpiDropGroupBys(recipe kpiRecipe, args KPIArgs, subprojects, subsidiaries string) []string {
	var drops []string
	if subsidiaries == kpiScopeCombined {
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
	if subprojects == kpiScopeCombined && recipe.projectGrain {
		// Only a per-project KPI changes shape: everywhere else the rows
		// are already one per organization, tier or year, and combined is
		// the filter alone.
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

// kpiColumnLabels renames the entity-qualified result columns to the words the
// tool's own parameters use. The entity prefix is warehouse modelling; a
// caller asked for an organization and a project and gets back columns with
// those names.
var kpiColumnLabels = map[string]string{
	"account__account_name":        "account",
	"account__account_rollup_name": "parent_org",
	"project__slug":                "project",
	"project__foundation_slug":     "foundation",
	"project__name":                "project_name",
	"project__foundation_name":     "foundation_name",
}

// kpiClientColumn is the client vocabulary for one result column: the explicit
// label if there is one, else the column with its entity prefix stripped.
// Anything else is left exactly as it came back.
func kpiClientColumn(name string) string {
	if label, ok := kpiColumnLabels[name]; ok {
		return label
	}
	for _, prefix := range []string{"account__", "project__"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	return name
}

// kpiRelabelKeys maps the keys of one JSON object to the client vocabulary,
// dropping any rename whose label would collide with another key of the same
// object: an ambiguous label hides which column a value came from, so both
// columns keep their qualified names.
func kpiRelabelKeys(keys []string) map[string]string {
	counts := make(map[string]int, len(keys))
	for _, key := range keys {
		counts[kpiClientColumn(key)]++
	}
	labels := make(map[string]string, len(keys))
	for _, key := range keys {
		label := kpiClientColumn(key)
		if label != key && counts[label] > 1 {
			label = key
		}
		labels[key] = label
	}
	return labels
}

// kpiOrderBy maps the caller's sort fields back to the names the semantic
// layer knows, so order_by takes the same column names the results carry. A
// field that is not one of the recipe's surviving group-by columns — a metric
// name, or a qualified name the caller already knew — is passed through
// untouched.
func kpiOrderBy(recipe kpiRecipe, dropped []string, orderBy string) []string {
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
	for column, label := range kpiRelabelKeys(remaining) {
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

// kpiRelabelJSON rewrites the qualified column names in a lens response to the
// client vocabulary, leaving every value — compiled_sql included — untouched
// and the document's key order intact. A body that is not JSON, or that this
// cannot rewrite, is returned exactly as it arrived.
func kpiRelabelJSON(body []byte) []byte {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var out bytes.Buffer
	if err := kpiRewriteJSONValue(decoder, &out); err != nil {
		return body
	}
	return out.Bytes()
}

// kpiRewriteJSONValue copies one JSON value from the decoder to out, renaming
// object keys and any string that is itself a column name (a response that
// lists its columns names them the same way a row keys them).
func kpiRewriteJSONValue(decoder *json.Decoder, out *bytes.Buffer) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delim, isDelim := token.(json.Delim)
	if !isDelim {
		if text, isString := token.(string); isString {
			return kpiWriteJSONToken(kpiClientColumn(text), out)
		}
		return kpiWriteJSONToken(token, out)
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
			if err := kpiRewriteJSONValue(decoder, &value); err != nil {
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
		labels := kpiRelabelKeys(keys)

		out.WriteByte('{')
		for i, m := range members {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := kpiWriteJSONToken(labels[m.key], out); err != nil {
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
			if err := kpiRewriteJSONValue(decoder, out); err != nil {
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

// kpiWriteJSONToken encodes one scalar (or object key) without HTML escaping,
// so SQL comparison operators inside compiled_sql stay readable.
func kpiWriteJSONToken(value any, out *bytes.Buffer) error {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	out.Write(bytes.TrimRight(encoded.Bytes(), "\n"))
	return nil
}

func handleKPIs(ctx context.Context, _ *mcp.CallToolRequest, args KPIArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	req, rejection := buildKPIRequest(args, time.Now().UTC())
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
		return nil, nil, fmt.Errorf("KPI call failed: %w", err)
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

	return lensPrettyJSON(kpiRelabelJSON(body))
}
