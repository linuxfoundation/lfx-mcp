// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools implements the MCP tool handlers for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// query_lfx_kpis — governed KPI recipes
// ---------------------------------------------------------------------------

// The recipes listed here are saved queries defined in lf-dbt's
// models/semantic_saved_queries.yml; the list below is a routing surface, not
// the source of truth. When a recipe is added or retired there, update this
// description in the same change that updates the prod tool allowlist.
//
// Execution is proxied through the LFX Lens service's saved-query endpoint,
// which passes the name to the dbt Semantic Layer (dbt-sl-sdk: saved_query is
// mutually exclusive with metrics/group_by; where, order_by and limit apply on
// top). The recipe is fixed in lf-dbt and callers choose only the SLICE, so
// every scope and window parameter here expands into a where entry rather than
// into a group_by.
//
// A recipe missing from the deployed manifest fails with a clear server-side
// error, which the handler returns verbatim — that is the signal the dbt side
// has not deployed it yet, not a reason to retry.
const kpisDescription = `Run a governed KPI recipe: fixed metrics and grouping, the same figure every run. When one matches the question, prefer it over explore + query.

Read read_lfx_kpi_guidance once per session first: result columns, time shape and caveats per recipe.

PARAMETERS (identical for every recipe)
saved_query  required. Recipe name from the list below.
foundation   slug from search_projects (e.g. cncf): whole foundation.
project      slug from search_projects (e.g. k8s): one project.
org          legal name from search_b2b_orgs. Matches the PARENT account, so name the TOP parent; rows come back per account. Rejected on a recipe with no organization lens.
since, until yyyy-mm-dd. FLOW recipes: rows dated in the window. Omitted = all time.
as_of        yyyy-mm-dd. SNAPSHOT recipes: state on that date. Omitted = today.
where        extra MetricFlow filter, one-hop names, ANDed with the above.
order_by     a result column, - prefix for descending.
limit        max rows, ceiling 500. Omitted returns every row.

A parameter the recipe cannot honour returns an error naming the recipe's shape. Results carry compiled_sql.

RECIPES: kpi_members_and_dues_by_account, kpi_membership_tier_split, kpi_new_members_by_year, kpi_membership_churn, kpi_contributions_by_org, kpi_contributors_by_org, kpi_contributors_by_project, kpi_maintainers_by_org, kpi_event_registrations_by_org, kpi_training_enrollments_by_org.

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
// SavedQuery is the only required field, so under the schema compaction
// described on QuerySemanticLayerArgs its description is the one that survives
// intact alongside the tool description; the contract a caller must not get
// wrong (fixed recipe, the slice parameters, FLOW vs SNAPSHOT) is stated there
// as well as in the tool description, never only on an optional parameter.
type KPIArgs struct {
	SavedQuery string `json:"saved_query" jsonschema:"Required. The recipe name, exactly as listed in the tool description and read_lfx_kpi_guidance (e.g. kpi_members_and_dues_by_account). The recipe's metrics and grouping are fixed - there are no metrics/group_by parameters; slice it with foundation, project, org, since/until or as_of, and where. FLOW recipes take since/until on their time axis; SNAPSHOT recipes take as_of."`
	Foundation string `json:"foundation,omitempty" jsonschema:"Optional foundation scope: a project slug from search_projects (e.g. cncf). Scopes the whole foundation."`
	Project    string `json:"project,omitempty" jsonschema:"Optional single-project scope: a project slug from search_projects (e.g. k8s)."`
	Org        string `json:"org,omitempty" jsonschema:"Optional organization scope: the legal name from search_b2b_orgs (e.g. International Business Machines Corporation). It matches the PARENT account, so name the TOP parent - a subsidiary's name returns only what rolls up to that subsidiary, excluding the subsidiary's own row. Rows come back per account; a rollup grain is not deployed yet. Rejected on a recipe with no organization lens, such as kpi_maintainers_by_org."`
	Since      string `json:"since,omitempty" jsonschema:"Optional window start, yyyy-mm-dd, on the recipe's own time axis. FLOW recipes only. Omitted = all time."`
	Until      string `json:"until,omitempty" jsonschema:"Optional window end, yyyy-mm-dd, on the recipe's own time axis. FLOW recipes only. Omitted = all time."`
	AsOf       string `json:"as_of,omitempty" jsonschema:"Optional as-of date, yyyy-mm-dd, for SNAPSHOT recipes. Only today's date is available until as-of history is deployed; omitted = today."`
	Where      string `json:"where,omitempty" jsonschema:"Optional MetricFlow filter applied on top of the recipe and the scope parameters, e.g. {{ Dimension('account__account_name') }} = 'Red Hat LLC'. One-hop names only. Dates are yyyy-mm-dd. Check literals with explore_lfx_semantic_layer's get_dimension_values first - an unknown literal returns zero rows, not an error."`
	OrderBy    string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields, prefix with - for descending, e.g. -total_registrations. Each must be one of the recipe's own result columns (its metrics or group-by fields)."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum rows to return, ceiling 500. Use 10-20 for top-N questions. Omitting it returns EVERY row - set a limit unless you need the complete set."`
}

// kpiShape says how a recipe is measured in time: a FLOW recipe counts rows
// whose date falls in a window (since/until); a SNAPSHOT recipe reports the
// state on a date (as_of). Passing the wrong one is rejected rather than
// silently ignored, because a silently ignored window returns a confident
// wrong figure.
type kpiShape string

const (
	kpiFlow     kpiShape = "FLOW"
	kpiSnapshot kpiShape = "SNAPSHOT"
)

// kpiRecipe is the per-recipe half of the expansion: everything the uniform
// parameters need that differs between recipes.
type kpiRecipe struct {
	shape kpiShape
	// timeAxis is the MetricFlow time dimension since/until filter on.
	timeAxis string
	// orgDimension is the dimension the org parameter filters on. The
	// sentinel kpiNoOrgLens means the recipe has no parent-organization
	// lens at all, so org is rejected instead of expanded — silently
	// filtering the wrong dimension returns zero rows, not an error.
	orgDimension string
	// accountNameDimension is the exact account-name dimension a recipe
	// without an org lens must be filtered on with where instead. Only
	// recipes marked kpiNoOrgLens set it.
	accountNameDimension string
}

const (
	// defaultKPITimeAxis and defaultKPIOrgDimension are what a recipe absent
	// from kpiRecipes gets, so a recipe deployed in lf-dbt before this table
	// learns about it still honours every parameter.
	defaultKPITimeAxis     = "metric_time"
	defaultKPIOrgDimension = "account__account_rollup_name"

	// kpiNoOrgLens marks a recipe with no parent-organization dimension.
	// Its only employer column is an exact account name, which is a
	// different question from "this parent and everything under it", so org
	// is rejected rather than quietly answered per account.
	kpiNoOrgLens = "none"
)

// kpiRecipes carries only what differs from the defaults. Verified live
// 2026-09-02; recipes not listed here are FLOW on metric_time with the
// account rollup as their organization lens.
var kpiRecipes = map[string]kpiRecipe{
	"kpi_members_and_dues_by_account": {shape: kpiSnapshot},
	"kpi_membership_tier_split":       {shape: kpiSnapshot},
	"kpi_new_members_by_year":         {shape: kpiFlow},
	"kpi_membership_churn":            {shape: kpiFlow},
	"kpi_contributions_by_org":        {shape: kpiFlow},
	"kpi_contributors_by_org":         {shape: kpiFlow},
	"kpi_contributors_by_project":     {shape: kpiFlow},
	// Maintainers have no usable start date, so the roster is only readable
	// as a snapshot. There is no account entity on silver_dim_maintainers
	// yet, so the recipe has NO parent-organization lens: its only employer
	// column is the maintainer's own exact account name.
	"kpi_maintainers_by_org": {shape: kpiSnapshot, orgDimension: kpiNoOrgLens, accountNameDimension: "maintainer_key__account_name"},
	// Registrations are windowed on the EVENT start date, so a window means
	// "events in the window" and matches the recipe's event-year grouping.
	"kpi_event_registrations_by_org":  {shape: kpiFlow, timeAxis: "registration_id__event_start_date"},
	"kpi_training_enrollments_by_org": {shape: kpiFlow},
	// Callable but not advertised in the description: one metric and one name
	// dimension each, no trap worth a routing slot.
	"kpi_event_registrations":  {shape: kpiFlow, timeAxis: "registration_id__event_start_date"},
	"kpi_training_enrollments": {shape: kpiFlow},
}

// kpiRecipeFor returns the recipe's expansion entry with the defaults filled
// in. An unknown name is treated as FLOW on metric_time so a newly deployed
// recipe works before this table is updated.
func kpiRecipeFor(name string) kpiRecipe {
	recipe := kpiRecipes[name]
	if recipe.shape == "" {
		recipe.shape = kpiFlow
	}
	if recipe.timeAxis == "" {
		recipe.timeAxis = defaultKPITimeAxis
	}
	if recipe.orgDimension == "" {
		recipe.orgDimension = defaultKPIOrgDimension
	}
	return recipe
}

// kpiRequest is the lens payload the uniform parameters expand into.
type kpiRequest struct {
	savedQuery string
	where      []string
	orderBy    []string
	limit      int
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

// kpiTimeFilter renders a day-grain bound on a recipe's time axis.
func kpiTimeFilter(axis, operator, date string) string {
	return fmt.Sprintf("{{ TimeDimension('%s','DAY') }} %s '%s'", axis, operator, kpiLiteral(date))
}

// buildKPIRequest expands the uniform parameters into the lens payload, or
// returns the rejection message the caller gets instead. today is passed in so
// the as-of rule is testable; callers pass time.Now().UTC().
//
// Scope and window become separate entries in the where list (lens ANDs the
// list) rather than a group_by: the recipe's grouping is fixed in lf-dbt and
// dbt-sl-sdk forbids group_by alongside saved_query.
func buildKPIRequest(args KPIArgs, today time.Time) (kpiRequest, string) {
	name := strings.TrimSpace(args.SavedQuery)
	if name == "" {
		return kpiRequest{}, "Error: saved_query is required. Pick a recipe name from the tool description, e.g. kpi_members_and_dues_by_account."
	}
	if args.Limit < 0 || args.Limit > 500 {
		return kpiRequest{}, fmt.Sprintf("Error: limit must be between 1 and 500, got %d. Omit it entirely to return every row.", args.Limit)
	}

	recipe := kpiRecipeFor(name)

	if org := strings.TrimSpace(args.Org); org != "" && recipe.orgDimension == kpiNoOrgLens {
		return kpiRequest{}, fmt.Sprintf("Error: %s has no parent-organization lens yet: org is not accepted. Filter with where on %s = '<exact account name>' and present the figure as per-account, not per parent.", name, recipe.accountNameDimension)
	}

	since := strings.TrimSpace(args.Since)
	until := strings.TrimSpace(args.Until)
	asOf := strings.TrimSpace(args.AsOf)

	if recipe.shape == kpiSnapshot && (since != "" || until != "") {
		return kpiRequest{}, fmt.Sprintf("Error: %s is a SNAPSHOT recipe: it reports the state on a date, so since/until do not apply. Use as_of, or pick a FLOW recipe to count rows in a window.", name)
	}
	if recipe.shape == kpiFlow && asOf != "" {
		return kpiRequest{}, fmt.Sprintf("Error: %s is a FLOW recipe measured on %s: it counts rows in a window, so as_of does not apply. Use since/until.", name, recipe.timeAxis)
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
		return kpiRequest{}, fmt.Sprintf("Error: %s is a SNAPSHOT recipe and as-of history is not deployed yet; one call per period once it is. Only today (%s) can be read, so omit as_of or pass today's date.", name, today.Format(kpiDateLayout))
	}

	req := kpiRequest{savedQuery: name, limit: args.Limit}

	if foundation := strings.TrimSpace(args.Foundation); foundation != "" {
		req.where = append(req.where, kpiDimensionFilter("project__foundation_slug", foundation))
	}
	if project := strings.TrimSpace(args.Project); project != "" {
		req.where = append(req.where, kpiDimensionFilter("project__slug", project))
	}
	if org := strings.TrimSpace(args.Org); org != "" {
		req.where = append(req.where, kpiDimensionFilter(recipe.orgDimension, org))
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
	req.orderBy = parseCSV(args.OrderBy)

	return req, ""
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
	if len(req.orderBy) > 0 {
		reqBody["order_by"] = req.orderBy
	}
	if req.limit > 0 {
		reqBody["limit"] = req.limit
	}

	body, statusCode, err := lensConfig.ServiceClient.PostJSON(ctx, "/lfx-lens/semantic-layer/saved-query", reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("KPI recipe call failed: %w", err)
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

	return lensPrettyJSON(body)
}
