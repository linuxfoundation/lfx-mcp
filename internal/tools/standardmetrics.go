// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools implements the MCP tool handlers for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// query_lfx_standard_metrics — governed standard metric recipes
// ---------------------------------------------------------------------------

// A standard metric is a governed query shape: which metrics, which grouping,
// which built-in filter, and how a caller's project, organization and time
// scope become semantic layer filters. All of that lives in the LFX Lens
// service, which owns the recipe registry, builds the query and returns the
// result in the client vocabulary (account, parent_org, project, year...).
//
// This tool is the routing surface for it: the description and the guidance
// documents say which recipes exist and how to slice one, and the handler
// sends the arguments to the lens endpoint unchanged. Nothing here decides
// what a parameter means — one rule, worded once, on the side that owns it —
// so a rejection is the lens's own message, returned verbatim.
const standardMetricsDescription = `Run a governed standard metric: fixed metrics and grouping, so the same question re-run gives the same figure. When one fits, prefer it over explore + query.

METRICS, with the groupings each offers (by)
memberships (SNAPSHOT): total | org | tier
new_members (FLOW, install date): year
membership_churn (FLOW, churn date): year
contributors (FLOW): total | org | project
contributions (FLOW): total | org | project | contributor
maintainers (SNAPSHOT): total | org | project | maintainer
maintainer_contributions (FLOW): total | org | project | maintainer

read_lfx_standard_metrics_guidance: what each answers, columns, defaults, caveats.

ALWAYS resolve names first: project slugs from search_projects, org names from search_b2b_orgs; never pass a name they have not returned.

PARAMETERS
metric        required. One of the names above.
by            total = ONE figure; org, project, tier, year = one row each; contributor, maintainer = people by name. Default = the first listed.
project       slug of ONE project or foundation.
subprojects   excluded | separate | combined. Default combined = the project and everything under it as ONE figure; separate = one row each; excluded = its own bucket only.
org           legal name of ONE organization.
subsidiaries  excluded | separate | combined. Default excluded = that account only; separate = it plus every subsidiary at any depth, one row each; combined = those folded into one.
since, until  yyyy-mm-dd. FLOW metrics only. Omitted: contribution metrics read the trailing 365 days, the rest all time.
as_of         yyyy-mm-dd. SNAPSHOT metrics only. Omitted = today.
order_by      a result column; - prefix = descending.
limit         1..500. Omitted = every row.

No free filter: a slice these cannot express is an explore + query question. Errors name the fix. Results carry compiled_sql and an applied block (scope and window used).

Decks: also read read_lfx_deck_building_guidance.`

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

// StandardMetricsArgs defines the input for query_lfx_standard_metrics. Every
// field travels to the lens standard-metric endpoint unchanged in meaning;
// standardMetricRequest below is the body it becomes. There is deliberately
// no free-form filter: the scope switches and the window are the only ways
// to slice a governed figure, so a result is always the recipe as defined.
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
// so the lens rejects it rather than silently reading it as "no limit".
type StandardMetricsArgs struct {
	Metric       string `json:"metric" jsonschema:"Required. One of: memberships, new_members, membership_churn, contributors, contributions, maintainers, maintainer_contributions. Each is a fixed set of metrics, and by picks its grouping (total, org, project, tier, year, contributor or maintainer, as the metric offers) - there are no metrics/group_by parameters; slice it with project, org, their subprojects/subsidiaries switches, and since/until or as_of. FLOW metrics take since/until on their time axis; SNAPSHOT metrics take as_of. read_lfx_standard_metrics_guidance lists what each one answers, its result columns and its caveats."`
	By           string `json:"by,omitempty" jsonschema:"How the figure is grouped, as the metric offers: total = ONE figure for the scope; org = one row per organization (account, with parent_org where the metric carries it); project = one row per project; tier = per membership tier; year = per year; contributor = one row per person by display name (contributions); maintainer = the roster (maintainers) or one row per maintainer by display name (maintainer_contributions). Omitted = the metric's first grouping (total, or year on the yearly metrics). A grouping the metric does not offer returns an error naming the valid ones."`
	Project      string `json:"project,omitempty" jsonschema:"Optional project scope: ONE slug from search_projects, of a project or of a foundation (e.g. cncf, k8s). Stored slugs are not everyday names - resolve it, never guess it."`
	Subprojects  string `json:"subprojects,omitempty" jsonschema:"What happens to the projects under project: combined (default) = the project plus everything under it folded into ONE figure (drops every project column the metric groups by); separate = the project plus everything under it, one row each, for a breakdown; excluded = that project's own bucket only."`
	Org          string `json:"org,omitempty" jsonschema:"Optional organization scope: ONE legal name from search_b2b_orgs, in its stored spelling (e.g. Red Hat LLC). Resolve it, never guess it."`
	Subsidiaries string `json:"subsidiaries,omitempty" jsonschema:"What happens to the accounts under org: excluded (default) = that account only; separate = the account plus every subsidiary at any depth, one row each; combined = the account plus those folded into one row. Without org, combined returns one row per parent organization, resolved to the top of each chain."`
	Since        string `json:"since,omitempty" jsonschema:"Optional window start, yyyy-mm-dd, on the metric's own time axis. FLOW metrics only. Omitted: the contribution metrics read the trailing 365 days, every other metric all time; the response's applied block says which window ran."`
	Until        string `json:"until,omitempty" jsonschema:"Optional window end, yyyy-mm-dd, on the metric's own time axis. FLOW metrics only. Omitted = no upper bound."`
	AsOf         string `json:"as_of,omitempty" jsonschema:"Optional as-of date, yyyy-mm-dd, for SNAPSHOT metrics. Only today's date is available until as-of history exists; omitted = today."`
	OrderBy      string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields, prefix with - for descending, e.g. -total_contributors. Only the metric's own result columns, as listed in read_lfx_standard_metrics_guidance: its metric name(s) plus its grouping columns (account, parent_org, project, foundation, year...). A column the call folds away (project columns under subprojects=combined, org columns under subsidiaries=combined) cannot be ordered on."`
	Limit        *int   `json:"limit,omitempty" jsonschema:"Maximum rows to return, 1..500. Use 10-20 for top-N questions. Omitting it returns EVERY row - set a limit unless you need the complete set."`
}

// standardMetricRequest is the body of a standard-metric call. It mirrors
// StandardMetricsArgs field for field, with the one difference the caller
// never sees: the endpoint reads order_by as a LIST, the same spelling the
// ad-hoc query endpoint takes. The omitempty tags are the rest of
// the contract — an argument the caller left out is absent from the body
// rather than sent as an empty scope the lens would have to interpret.
type standardMetricRequest struct {
	Metric       string   `json:"metric"`
	By           string   `json:"by,omitempty"`
	Project      string   `json:"project,omitempty"`
	Subprojects  string   `json:"subprojects,omitempty"`
	Org          string   `json:"org,omitempty"`
	Subsidiaries string   `json:"subsidiaries,omitempty"`
	Since        string   `json:"since,omitempty"`
	Until        string   `json:"until,omitempty"`
	AsOf         string   `json:"as_of,omitempty"`
	OrderBy      []string `json:"order_by,omitempty"`
	Limit        *int     `json:"limit,omitempty"`
}

// newStandardMetricRequest maps the tool arguments onto the wire body.
// order_by is a comma-separated list of fields, split the way
// handleQuerySemanticLayer splits it. Nothing else is interpreted here: the
// lens validates the values.
func newStandardMetricRequest(args StandardMetricsArgs) standardMetricRequest {
	return standardMetricRequest{
		Metric:       args.Metric,
		By:           args.By,
		Project:      args.Project,
		Subprojects:  args.Subprojects,
		Org:          args.Org,
		Subsidiaries: args.Subsidiaries,
		Since:        args.Since,
		Until:        args.Until,
		AsOf:         args.AsOf,
		OrderBy:      parseCSV(args.OrderBy),
		Limit:        args.Limit,
	}
}

// standardMetricEndpoint runs one recipe from the lens registry.
const standardMetricEndpoint = "/lfx-lens/semantic-layer/standard-metric"

func handleStandardMetrics(ctx context.Context, _ *mcp.CallToolRequest, args StandardMetricsArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	body, statusCode, err := lensConfig.ServiceClient.PostJSON(ctx, standardMetricEndpoint, newStandardMetricRequest(args))
	if err != nil {
		return nil, nil, fmt.Errorf("standard metric call failed: %w", err)
	}
	// Any 2xx carries a result; everything else is a failure to report.
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: standardMetricError(body, statusCode)}},
			IsError: true,
		}, nil, nil
	}

	return lensPrettyJSON(body)
}

// standardMetricError is the text a failed call returns. The lens words every
// rejection for the caller — the rule it broke and the fix — so that message
// is passed through verbatim; an invented friendlier wording here would have
// the model correct the wrong thing. A body carrying no message reaches the
// caller whole, with its status, rather than as a silent empty failure.
func standardMetricError(body []byte, statusCode int) string {
	var payload struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Detail != "" {
		return payload.Detail
	}
	return fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))
}
