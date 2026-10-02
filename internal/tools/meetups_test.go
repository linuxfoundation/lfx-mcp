// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func meetupInt(v int) *int { return &v }

func TestMeetupTools_RegisterReadOnlyWithinBudget(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"search_lfx_meetups", RegisterSearchMeetups},
		{"query_lfx_meetup_analytics", RegisterMeetupAnalytics},
	} {
		tool := listRegisteredTool(t, tc.name, tc.register)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s must carry ReadOnlyHint", tc.name)
		}
		if got := len(tool.Description); got > schemaDescriptionBudget {
			t.Errorf("%s description is %d bytes; everything past %d is invisible to the model", tc.name, got, schemaDescriptionBudget)
		}
		if required := schemaRequired(t, tool); len(required) != 0 {
			t.Errorf("%s must have no required parameter, got %v", tc.name, required)
		}
	}
}

// Nothing a caller cannot guess may live only on an optional parameter.
func TestMeetupTools_DescriptionsCarryTheContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
		wants    []string
	}{
		{
			name:     "search_lfx_meetups",
			register: RegisterSearchMeetups,
			wants: []string{
				"ocgroups.dev", "one row per event", "Upcoming unless start_date is set",
				"not an LFX project slug", "ISO 3166-1 alpha-2",
				"order_by starts_at (default) or -starts_at", "limit default 10, max 100",
				"rejected with the closest candidates", "search_meetings", "query_lfx_meetup_analytics",
			},
		},
		{
			name:     "query_lfx_meetup_analytics",
			register: RegisterMeetupAnalytics,
			wants: []string{
				"meetups, registrants and checked_in",
				"by: total (default), community, group, city, country or region",
				"LF region of the venue country", "period adds month, quarter or year",
				"not an LFX project slug", "trailing 365 days",
				"by=group includes active groups with no meetups as 0",
				"checked_in 0, not low attendance", "no Bevy-era history",
				"rejected with candidates", "search_lfx_meetups",
			},
		},
	} {
		tool := listRegisteredTool(t, tc.name, tc.register)
		for _, want := range tc.wants {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("%s description missing %q", tc.name, want)
			}
		}
	}
}

// The argument struct is the request body: set fields as given, unset
// fields absent, zero and negative limit/offset forwarded for the lens to
// reject.
func TestSearchMeetups_SendsTheArgumentsAsGiven(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleSearchMeetups(context.Background(), &mcp.CallToolRequest{}, SearchMeetupsArgs{
		Community: "CNCF",
		Group:     "cncf-austin",
		Country:   "US",
		StartDate: "2026-10-01",
		EndDate:   "2026-12-31",
		Query:     "kubernetes",
		OrderBy:   "-starts_at",
		Limit:     meetupInt(0),
		Offset:    meetupInt(-5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if captured.Method != http.MethodPost || captured.Path != "/lfx-lens/meetups/search" {
		t.Errorf("unexpected request: %s %s", captured.Method, captured.Path)
	}
	want := `{"community":"CNCF","group":"cncf-austin","country":"US","start_date":"2026-10-01","end_date":"2026-12-31",` +
		`"q":"kubernetes","order_by":"-starts_at","limit":0,"offset":-5}`
	if got := string(captured.Body); got != want {
		t.Errorf("request body =\n%s\nwant\n%s", got, want)
	}
}

func TestSearchMeetups_OmitsUnsetArguments(t *testing.T) {
	captured := setupLensTest(t)
	if _, _, err := handleSearchMeetups(context.Background(), &mcp.CallToolRequest{}, SearchMeetupsArgs{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(captured.Body); got != `{}` {
		t.Errorf("request body = %s, want {}", got)
	}
}

func TestMeetupAnalytics_SendsTheArgumentsAsGiven(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleMeetupAnalytics(context.Background(), &mcp.CallToolRequest{}, MeetupAnalyticsArgs{
		Community: "CNCF",
		Group:     "cncf-austin",
		Country:   "US",
		StartDate: "2025-01-01",
		EndDate:   "2025-12-31",
		By:        "region",
		Period:    "year",
		OrderBy:   "-meetups",
		Limit:     meetupInt(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if captured.Method != http.MethodPost || captured.Path != "/lfx-lens/meetups/analytics" {
		t.Errorf("unexpected request: %s %s", captured.Method, captured.Path)
	}
	want := `{"community":"CNCF","group":"cncf-austin","country":"US","start_date":"2025-01-01","end_date":"2025-12-31",` +
		`"by":"region","period":"year","order_by":"-meetups","limit":0}`
	if got := string(captured.Body); got != want {
		t.Errorf("request body =\n%s\nwant\n%s", got, want)
	}
}

func TestMeetupAnalytics_OmitsUnsetArguments(t *testing.T) {
	captured := setupLensTest(t)
	if _, _, err := handleMeetupAnalytics(context.Background(), &mcp.CallToolRequest{}, MeetupAnalyticsArgs{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(captured.Body); got != `{}` {
		t.Errorf("request body = %s, want {}", got)
	}
}

// A 2xx envelope renders like a standard-metric result: applied pretty, data
// compact; the lens's own detail text comes back verbatim on anything else.
func TestMeetupAnalytics_RendersEnvelopeAndPassesDetailThrough(t *testing.T) {
	setupLensResponseTest(t, `{"applied":{"by":"region","window":"2025-01-01..2025-12-31"},"columns":["region","meetups","registrants","checked_in"],"data":[{"region":"North America","meetups":41,"registrants":1200,"checked_in":640}],"row_count":1,"truncated":false}`)
	res, _, err := handleMeetupAnalytics(context.Background(), &mcp.CallToolRequest{}, MeetupAnalyticsArgs{By: "region"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, res)
	for _, want := range []string{`"applied"`, `"North America"`, `"checked_in":640`} {
		if !strings.Contains(text, want) {
			t.Errorf("result missing %q:\n%s", want, text)
		}
	}

	setupFakeLens(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"unknown community 'cncf'; did you mean CNCF?"}`))
	})
	for name, call := range map[string]func() (*mcp.CallToolResult, any, error){
		"search_lfx_meetups": func() (*mcp.CallToolResult, any, error) {
			return handleSearchMeetups(context.Background(), &mcp.CallToolRequest{}, SearchMeetupsArgs{Community: "cncf"})
		},
		"query_lfx_meetup_analytics": func() (*mcp.CallToolResult, any, error) {
			return handleMeetupAnalytics(context.Background(), &mcp.CallToolRequest{}, MeetupAnalyticsArgs{Community: "cncf"})
		},
	} {
		res, _, err := call()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if !res.IsError {
			t.Fatalf("%s: expected an error result", name)
		}
		if got, want := resultText(t, res), "unknown community 'cncf'; did you mean CNCF?"; got != want {
			t.Errorf("%s: error text = %q, want %q", name, got, want)
		}
	}
}

func TestMeetupTools_UnconfiguredLensIsAnError(t *testing.T) {
	prev := lensConfig
	lensConfig = nil
	t.Cleanup(func() { lensConfig = prev })

	if _, _, err := handleSearchMeetups(context.Background(), &mcp.CallToolRequest{}, SearchMeetupsArgs{}); err == nil {
		t.Error("search_lfx_meetups must fail when lens is not configured")
	}
	if _, _, err := handleMeetupAnalytics(context.Background(), &mcp.CallToolRequest{}, MeetupAnalyticsArgs{}); err == nil {
		t.Error("query_lfx_meetup_analytics must fail when lens is not configured")
	}
}
