// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools implements the MCP tool handlers for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// search_ocg_meetups / list_ocg_meetup_filters — Open Community Groups
// ---------------------------------------------------------------------------

// Open Community Groups (OCG) are the meetup chapters hosted on
// https://ocgroups.dev. Their data lives in the warehouse
// (ANALYTICS.PLATINUM_LFX_ONE.OCG_UPCOMING_MEETUPS and OCG_MEETUPS_FILTERS,
// the same views lfx-one reads) and, like every other warehouse read in this
// server, is reached through the LFX Lens service rather than a direct
// database client. These tools are the thin routing surface for two lens
// endpoints; the lens owns the SQL, the column vocabulary and every
// validation message, which is passed through verbatim.
//
// Wire contract (query parameters map 1:1 to the tool arguments):
//
//	GET /lfx-lens/ocg/meetups
//	    community, q, location, since, until, limit, offset
//	    -> {"data":[{"event_id","event_name","community","starts_at","date",
//	                 "location","group_slug","event_slug","url"}],
//	        "total","limit","offset"}
//
//	GET /lfx-lens/ocg/meetup-filters
//	    -> {"communities":[...],"roles":[...]}
const (
	ocgMeetupsEndpoint       = "/lfx-lens/ocg/meetups"
	ocgMeetupFiltersEndpoint = "/lfx-lens/ocg/meetup-filters"
)

const searchOCGMeetupsDescription = `Search upcoming Open Community Group (OCG) meetups - community-run local chapters and meetup groups hosted on ocgroups.dev (e.g. "CNCF Meetup - San Francisco").

Use this for questions about upcoming meetups, chapters and local community events. NOT for LFX project meetings (search_meetings), formal governance bodies (search_committees), or LF-run conferences like KubeCon (the semantic layer's events metrics).

Filter by community (a foundation's community name as listed by list_ocg_meetup_filters - call it first rather than guessing), free-text q on the event name, location, and a since/until date window on the start time. Results are ordered by start time and paginate with limit/offset; total is the full count.`

const listOCGMeetupFiltersDescription = `List the community names that host meetups, in the exact stored spelling search_ocg_meetups accepts for its community filter - call this before filtering by community rather than guessing. The response also lists the roles a person can hold at a meetup (attendee, organizer, speaker...); those are informational and are NOT a search_ocg_meetups filter.`

// RegisterSearchOCGMeetups registers the search_ocg_meetups tool.
func RegisterSearchOCGMeetups(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_ocg_meetups",
		Description: searchOCGMeetupsDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Open Community Group Meetups",
			ReadOnlyHint: true,
		},
	}, handleSearchOCGMeetups)
}

// RegisterListOCGMeetupFilters registers the list_ocg_meetup_filters tool.
func RegisterListOCGMeetupFilters(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_ocg_meetup_filters",
		Description: listOCGMeetupFiltersDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Open Community Group Meetup Filters",
			ReadOnlyHint: true,
		},
	}, handleListOCGMeetupFilters)
}

// SearchOCGMeetupsArgs defines the input for search_ocg_meetups. Every field
// travels to the lens meetups endpoint as the query parameter of the same
// name; nothing is interpreted here.
type SearchOCGMeetupsArgs struct {
	Community string `json:"community,omitempty" jsonschema:"Optional community that hosts the meetup, in the exact spelling returned by list_ocg_meetup_filters (e.g. CNCF). Omitted = every community."`
	Query     string `json:"q,omitempty" jsonschema:"Optional free-text match on the event name (case-insensitive substring)."`
	Location  string `json:"location,omitempty" jsonschema:"Optional free-text match on the event location (city, country, or Virtual)."`
	Since     string `json:"since,omitempty" jsonschema:"Optional window start on the meetup start time, yyyy-mm-dd inclusive. Omitted = from now."`
	Until     string `json:"until,omitempty" jsonschema:"Optional window end on the meetup start time, yyyy-mm-dd inclusive. Omitted = no upper bound."`
	Limit     *int   `json:"limit,omitempty" jsonschema:"Maximum rows to return (default 10, max 100)."`
	Offset    *int   `json:"offset,omitempty" jsonschema:"Number of rows to skip for pagination (default 0). The response's total says how many rows match in all."`
}

// ListOCGMeetupFiltersArgs defines the (empty) input for list_ocg_meetup_filters.
type ListOCGMeetupFiltersArgs struct{}

// ocgMeetupsQuery maps the tool arguments onto the endpoint's query string.
// Unset arguments are absent rather than sent as empty values the lens would
// have to interpret. limit and offset are pointers so an explicit 0 or a
// negative value is distinguishable from an omitted one and reaches the lens
// for its own validation message, rather than being dropped here.
func ocgMeetupsQuery(args SearchOCGMeetupsArgs) url.Values {
	params := url.Values{}
	for key, value := range map[string]string{
		"community": args.Community,
		"q":         args.Query,
		"location":  args.Location,
		"since":     args.Since,
		"until":     args.Until,
	} {
		if value != "" {
			params.Set(key, value)
		}
	}
	if args.Limit != nil {
		params.Set("limit", strconv.Itoa(*args.Limit))
	}
	if args.Offset != nil {
		params.Set("offset", strconv.Itoa(*args.Offset))
	}
	return params
}

func handleSearchOCGMeetups(ctx context.Context, _ *mcp.CallToolRequest, args SearchOCGMeetupsArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}
	return ocgDoGet(ctx, ocgMeetupsEndpoint, ocgMeetupsQuery(args))
}

func handleListOCGMeetupFilters(ctx context.Context, _ *mcp.CallToolRequest, _ ListOCGMeetupFiltersArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}
	return ocgDoGet(ctx, ocgMeetupFiltersEndpoint, nil)
}

// ocgDoGet is lensDoGet with the standard-metric error contract: the lens
// words every rejection for the caller (a {"detail": ...} body), so that
// message is returned verbatim rather than wrapped in a status line.
func ocgDoGet(ctx context.Context, path string, params url.Values) (*mcp.CallToolResult, any, error) {
	body, statusCode, err := lensConfig.ServiceClient.Get(ctx, path, params)
	if err != nil {
		return nil, nil, fmt.Errorf("API call to %s failed: %w", path, err)
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: standardMetricError(body, statusCode)}},
			IsError: true,
		}, nil, nil
	}
	return lensPrettyJSON(body)
}
