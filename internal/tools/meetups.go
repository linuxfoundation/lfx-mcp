// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools implements the MCP tool handlers for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// search_lfx_meetups / query_lfx_meetup_analytics — Open Community Groups
// ---------------------------------------------------------------------------

// Meetups are the community-run events on ocgroups.dev. Two tools cover them,
// both thin pass-throughs to LFX Lens routes that own every definition (what
// counts as a meetup, registrants, checked-in, region) and every validation
// message: one lists events, one aggregates. New analytics arrive as new
// groupings on the analytics tool, never as new tools, so the tool list the
// model reads on every request does not grow.
//
// The argument struct is the request body, as on query_lfx_standard_metrics:
// json tags are the field names, omitempty on every optional field, *int for
// limit and offset so an explicit 0 or a negative value reaches the lens for
// its own rejection, and a non-2xx response returns its detail verbatim.
const (
	meetupSearchEndpoint    = "/lfx-lens/meetups/search"
	meetupAnalyticsEndpoint = "/lfx-lens/meetups/analytics"
)

// Neither tool has a required parameter, and clients summarise optional
// parameter descriptions away, so what a caller cannot guess (the by values,
// the defaults, the routing) lives in the tool descriptions.
const searchMeetupsDescription = `Community-run meetup events from Open Community Groups (ocgroups.dev), one row per event: name, community, group, start time, venue city and country, and its ocgroups.dev link. Upcoming unless start_date is set; filter by community (ocgroups.dev community name, exact - not an LFX project slug), group (slug), country (ISO 3166-1 alpha-2 of the venue) or q (title words). order_by starts_at (default) or -starts_at; limit default 10, max 100; offset default 0. An unknown community or group is rejected with the closest candidates. Not LFX project meetings (search_meetings). For counts and attendance use query_lfx_meetup_analytics.`

const meetupAnalyticsDescription = `Meetup analytics for Open Community Groups (ocgroups.dev): meetups, registrants and checked_in per grouping. by: total (default), community, group, city, country or region (the LF region of the venue country); period adds month, quarter or year. Filter by community (ocgroups.dev name, exact - not an LFX project slug), group (slug), country (ISO code) and start_date/end_date on the event start date; the window defaults to the trailing 365 days. by=community lists every community; by=group includes active groups with no meetups as 0, so order_by=meetups is "least active". registrants and checked_in are distinct people; an event whose organisers do not check people in shows checked_in 0, not low attendance. Data is ocgroups.dev only (no Bevy-era history). An unknown community or group is rejected with candidates. For individual events use search_lfx_meetups.`

// RegisterSearchMeetups registers the search_lfx_meetups tool.
func RegisterSearchMeetups(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_lfx_meetups",
		Description: searchMeetupsDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search LFX Meetups",
			ReadOnlyHint: true,
		},
	}, handleSearchMeetups)
}

// RegisterMeetupAnalytics registers the query_lfx_meetup_analytics tool.
func RegisterMeetupAnalytics(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_lfx_meetup_analytics",
		Description: meetupAnalyticsDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Query LFX Meetup Analytics",
			ReadOnlyHint: true,
		},
	}, handleMeetupAnalytics)
}

// SearchMeetupsArgs is the request body of search_lfx_meetups.
type SearchMeetupsArgs struct {
	Community string `json:"community,omitempty" jsonschema:"ocgroups.dev community name, exact (e.g. CNCF). Not an LFX project slug."`
	Group     string `json:"group,omitempty" jsonschema:"Group slug, exact (e.g. cncf-austin)."`
	Country   string `json:"country,omitempty" jsonschema:"ISO 3166-1 alpha-2 code of the venue country (e.g. US)."`
	StartDate string `json:"start_date,omitempty" jsonschema:"yyyy-mm-dd, inclusive, on the event start date. Omitted = today, so the default is upcoming events."`
	EndDate   string `json:"end_date,omitempty" jsonschema:"yyyy-mm-dd, inclusive, on the event start date. Omitted = no upper bound."`
	Query     string `json:"q,omitempty" jsonschema:"Words in the event title, case-insensitive."`
	OrderBy   string `json:"order_by,omitempty" jsonschema:"starts_at (default) or -starts_at."`
	Limit     *int   `json:"limit,omitempty" jsonschema:"Rows per page, default 10, max 100."`
	Offset    *int   `json:"offset,omitempty" jsonschema:"Rows to skip, default 0. The response's total says how many match in all."`
}

// MeetupAnalyticsArgs is the request body of query_lfx_meetup_analytics.
type MeetupAnalyticsArgs struct {
	Community string `json:"community,omitempty" jsonschema:"ocgroups.dev community name, exact (e.g. CNCF). Not an LFX project slug."`
	Group     string `json:"group,omitempty" jsonschema:"Group slug, exact (e.g. cncf-austin)."`
	Country   string `json:"country,omitempty" jsonschema:"ISO 3166-1 alpha-2 code of the venue country (e.g. US)."`
	StartDate string `json:"start_date,omitempty" jsonschema:"yyyy-mm-dd, inclusive, on the event start date. Omitted = the trailing 365 days before end_date."`
	EndDate   string `json:"end_date,omitempty" jsonschema:"yyyy-mm-dd, inclusive, on the event start date. Omitted = today."`
	By        string `json:"by,omitempty" jsonschema:"Grouping: total (default), community, group, city, country or region (LF region of the venue country)."`
	Period    string `json:"period,omitempty" jsonschema:"month, quarter or year: adds a time column to by."`
	OrderBy   string `json:"order_by,omitempty" jsonschema:"A result column, - prefix for descending (e.g. -meetups, -registrants)."`
	Limit     *int   `json:"limit,omitempty" jsonschema:"Maximum rows. Omitted = every row."`
}

func handleSearchMeetups(ctx context.Context, _ *mcp.CallToolRequest, args SearchMeetupsArgs) (*mcp.CallToolResult, any, error) {
	return meetupPost(ctx, "meetup search", meetupSearchEndpoint, args)
}

func handleMeetupAnalytics(ctx context.Context, _ *mcp.CallToolRequest, args MeetupAnalyticsArgs) (*mcp.CallToolResult, any, error) {
	return meetupPost(ctx, "meetup analytics", meetupAnalyticsEndpoint, args)
}

// meetupPost sends the argument struct as the request body and renders the
// response under the standard-metrics contract: a 2xx envelope with its data
// rows compacted one per line, anything else as the lens's own detail text.
func meetupPost(ctx context.Context, op, path string, body any) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}
	resp, statusCode, err := lensConfig.ServiceClient.PostJSON(ctx, path, body)
	if err != nil {
		return nil, nil, fmt.Errorf("%s call failed: %w", op, err)
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: standardMetricError(resp, statusCode)}},
			IsError: true,
		}, nil, nil
	}
	return standardMetricResult(resp)
}
