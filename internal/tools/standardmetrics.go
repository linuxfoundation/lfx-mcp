// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools implements the MCP tool handlers for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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
// This tool is the routing surface for it: the description names the
// families and routes to the guidance document, which defines every
// grouping, switch, default and caveat (function, never rationale, in the
// description: it is read on every tools/list), and the handler sends the
// arguments to the lens endpoint unchanged. Nothing here decides
// what a parameter means — one rule, worded once, on the side that owns it —
// so a rejection is the lens's own message, returned verbatim.
const standardMetricsDescription = `Run a governed standard metric over LFX data: one figure or one row per grouping, scoped by project, organization and dates, with the applied scope echoed. Same question, same figure.

STANDARD METRICS memberships, new_members, membership_churn, contributors, contributions, contributing_organizations, participants, maintainers, maintainer_contributions, project_health, software_value, event_registrations, event_sponsorships, speakers, training_enrollments, certifications, social_mentions, social_reach.

Read read_lfx_standard_metrics_guidance BEFORE the first call, and again whenever in doubt: it defines every grouping (by), switch, default and caveat.

ALWAYS resolve names first: project slugs from search_projects, org names from search_b2b_orgs; never pass a name they have not returned.`

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
// caller must not get wrong (fixed metrics and grouping, window vs at-date,
// the three date parameters, resolve names first) is stated there as well as
// in the tool description, never only on an optional parameter.
//
// Limit is a pointer so an explicit 0 is distinguishable from an omitted
// value: omitted means every row, and 0 rows is not a question anyone asks,
// so the lens rejects it rather than silently reading it as "no limit".
type StandardMetricsArgs struct {
	Metric       string `json:"metric" jsonschema:"Required. The family: memberships, new_members, membership_churn, contributors, contributions, contributing_organizations, participants, maintainers, maintainer_contributions, project_health, software_value, event_registrations, event_sponsorships, speakers, training_enrollments, certifications, social_mentions or social_reach. Each is a fixed set of metrics; by picks its grouping - there are no metrics/group_by parameters. Every family takes start_date, end_date and period: a WINDOW family counts what happened between the two dates, an AT-DATE family (memberships, maintainers, project_health, software_value) reports the state on end_date. read_lfx_standard_metrics_guidance lists every grouping, default and caveat."`
	By           string `json:"by,omitempty" jsonschema:"Exactly one grouping from the family's list (read_lfx_standard_metrics_guidance): total = ONE figure for the scope; org, project, tier, country, region, event, course, type, platform, org_region, foundation, category, population, network, sentiment = one row each, as the family offers; contributor, maintainer = people by GitHub identity or the roster. Omitted = the family's first grouping (total). A grouping the family does not offer returns an error naming the valid ones."`
	Project      string `json:"project,omitempty" jsonschema:"Optional project scope: ONE slug from search_projects, exact (e.g. cncf, k8s). Omitted = LF-wide. An unknown slug is rejected with candidate slugs; never guess one."`
	Subprojects  string `json:"subprojects,omitempty" jsonschema:"What the project name covers: combined (default) = the project plus everything under it, any depth, folded into ONE figure; separate = one row each, the breakdown; excluded = that project's own bucket only."`
	Org          string `json:"org,omitempty" jsonschema:"Optional organization scope: ONE stored legal account name from search_b2b_orgs, exact (e.g. Red Hat LLC). A name matching no data-bearing account is rejected with candidates; never guess one. Families whose model carries no account reject org."`
	Subsidiaries string `json:"subsidiaries,omitempty" jsonschema:"What the org name covers: excluded (default) = that account only; separate = the account plus every subsidiary at any depth, one row each; combined = those folded into one row. Without org, combined on by=org is one row per parent organization."`
	StartDate    string `json:"start_date,omitempty" jsonschema:"yyyy-mm-dd, a UTC calendar day. On a window family the first day counted; on an at-date family only with period, the first period. Omitted = the family's window: the trailing 365 days before end_date on the activity, event, training and social families and on any day/week series; all history on new_members and membership_churn. The applied block says which ran."`
	EndDate      string `json:"end_date,omitempty" jsonschema:"yyyy-mm-dd, a UTC calendar day. On a window family the last day counted; on an at-date family the day the state is reported on. Omitted = today (UTC). A future end_date is honoured and flagged. maintainers takes today only unless period is set."`
	Period       string `json:"period,omitempty" jsonschema:"day, week, month, quarter or year: one row per period instead of one figure. Window families bucket their dates; at-date families report the state at each period end from start_date to end_date, the last row partial when end_date falls inside a period. Omitted = one figure."`
	OrderBy      string `json:"order_by,omitempty" jsonschema:"Comma-separated sort fields, prefix with - for descending, e.g. -total_contributors. Only the family's own result columns, as listed in read_lfx_standard_metrics_guidance: its metric name(s), its grouping columns (account, parent_org, project, foundation, event...) and period on a series. A column the call folds away cannot be ordered on."`
	Limit        *int   `json:"limit,omitempty" jsonschema:"Maximum rows to return. Use 10-20 for top-N questions. Omitting it returns EVERY row; the applied block's truncated flag says whether a limit cut rows off."`
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
	StartDate    string   `json:"start_date,omitempty"`
	EndDate      string   `json:"end_date,omitempty"`
	Period       string   `json:"period,omitempty"`
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
		StartDate:    args.StartDate,
		EndDate:      args.EndDate,
		Period:       args.Period,
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
// the model correct the wrong thing. The lens spells detail three ways: a
// string (the builder's rejections), an object with a message and candidates
// (the org and project guards), and a list of validation errors (a parameter
// the contract does not know, each with a msg) — all three are rendered as
// text the model can act on. A body carrying no message reaches the caller
// whole, with its status, rather than as a silent empty failure.
func standardMetricError(body []byte, statusCode int) string {
	var payload struct {
		Detail json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && len(payload.Detail) > 0 {
		if text := detailText(payload.Detail); text != "" {
			return text
		}
	}
	return fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))
}

// detailText renders a FastAPI detail — string, object or list — as text.
func detailText(detail json.RawMessage) string {
	var text string
	if err := json.Unmarshal(detail, &text); err == nil {
		return text
	}
	var guard struct {
		Message    string           `json:"message"`
		Candidates []map[string]any `json:"candidates"`
	}
	if err := json.Unmarshal(detail, &guard); err == nil && guard.Message != "" {
		if len(guard.Candidates) == 0 {
			return guard.Message
		}
		rendered, err := json.MarshalIndent(guard.Candidates, "", "  ")
		if err != nil {
			return guard.Message
		}
		return guard.Message + ":\n" + string(rendered)
	}
	var errors []struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(detail, &errors); err == nil && len(errors) > 0 {
		var msgs []string
		for _, e := range errors {
			if e.Msg != "" {
				msgs = append(msgs, strings.TrimPrefix(e.Msg, "Value error, "))
			}
		}
		return strings.Join(msgs, "\n")
	}
	return ""
}
