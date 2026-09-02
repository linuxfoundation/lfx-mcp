// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/serviceapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func listStandardMetricsTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return listRegisteredTool(t, "query_lfx_standard_metrics", RegisterStandardMetrics)
}

// metricLimit builds the pointer the limit parameter takes, so a test can say
// "limit 0 was sent" as distinct from "limit was omitted".
func metricLimit(value int) *int { return &value }

// ---------------------------------------------------------------------------
// Description and schema
// ---------------------------------------------------------------------------

// standardMetricsDescriptionCeiling is tighter than the hard schema budget:
// this description is read on every tools/list alongside ten siblings, so it
// is held to the space the contract, the metric inventory and the two
// defaults actually need, with headroom for the next metric name rather than
// for more prose.
const standardMetricsDescriptionCeiling = 1950

// standardMetricParameters is the whole contract; anything else is not a parameter of
// this tool.
var standardMetricParameters = []string{
	"metric", "project", "subprojects", "org", "subsidiaries",
	"since", "until", "as_of", "order_by", "limit",
}

// standardMetricNames is the advertised inventory, in the order the guidance
// lists it. The lens registry holds further recipes that stay callable and
// unadvertised, so this list is what the routing surface must name — no more,
// and none of them missing.
var standardMetricNames = []string{
	"members_and_dues_by_org",
	"membership_tiers",
	"new_members_by_year",
	"membership_churn_by_year",
	"contributors",
	"contributions",
	"contributions_by_org",
	"contributors_by_org",
	"contributors_by_project",
	"maintainers",
	"maintainers_by_org",
	"maintainers_by_project",
	"maintainer_roster",
}

// TestStandardMetricsDescription_FitsSchemaBudget holds the tool to the same budget as
// its siblings, and to the tighter ceiling above: everything past the limit is
// invisible to the model, and this description IS the parameter contract.
func TestStandardMetricsDescription_FitsSchemaBudget(t *testing.T) {
	if got := len(standardMetricsDescription); got > schemaDescriptionBudget {
		t.Errorf("query_lfx_standard_metrics description is %d bytes; everything past %d is invisible to the model",
			got, schemaDescriptionBudget)
	}
	if got := len(standardMetricsDescription); got > standardMetricsDescriptionCeiling {
		t.Errorf("query_lfx_standard_metrics description is %d bytes; the ceiling is %d - tighten the prose, keep the parameter lines",
			got, standardMetricsDescriptionCeiling)
	}
	for _, property := range standardMetricParameters {
		if got := len(schemaPropertyDescription(t, listStandardMetricsTool(t), property)); got > schemaDescriptionBudget {
			t.Errorf("standardmetrics.%s description is %d bytes; limit %d", property, got, schemaDescriptionBudget)
		}
	}
}

// TestStandardMetricsDescription_ListsTheInventoryByDomain pins the domain
// inventory: a caller cannot guess a metric name, and the domains tell it
// whether loading the guidance is worth it at all. The grouping sits near the
// top of the description so it survives schema compaction.
func TestStandardMetricsDescription_ListsTheInventoryByDomain(t *testing.T) {
	for _, want := range []string{
		"Memberships: members_and_dues_by_org, membership_tiers, new_members_by_year, membership_churn_by_year",
		"Contributions: contributors, contributions, contributions_by_org, contributors_by_org, contributors_by_project",
		"Maintainers: maintainers, maintainers_by_org, maintainers_by_project, maintainer_roster",
	} {
		if !strings.Contains(standardMetricsDescription, want) {
			t.Errorf("description missing inventory line %q", want)
		}
	}
	// Every advertised name is in the inventory, and the inventory comes
	// before the line that routes to the guidance: a client that truncates
	// the description keeps the names.
	inventoryEnd := strings.Index(standardMetricsDescription, "read_lfx_standard_metrics_guidance")
	if inventoryEnd < 0 {
		t.Fatal("description does not route to read_lfx_standard_metrics_guidance")
	}
	inventory := standardMetricsDescription[:inventoryEnd]
	for _, name := range standardMetricNames {
		if !strings.Contains(inventory, name) {
			t.Errorf("metric %q is not listed above the read-the-guidance line", name)
		}
	}
}

// TestStandardMetricsDescription_CarriesTheContract pins the contract the description is
// the only carrier of: every parameter line, both switch defaults, the
// resolve-first rule, the shape rule, and the routing to the guidance tools.
func TestStandardMetricsDescription_CarriesTheContract(t *testing.T) {
	for _, want := range []string{
		"prefer it over explore + query",
		"read_lfx_standard_metrics_guidance",
		"ALWAYS resolve names first",
		"search_projects",
		"search_b2b_orgs",
		"never pass one that has not come back from them",
		"metric ",
		"project ",
		"subprojects",
		"excluded | separate | combined",
		"Default combined",
		"subsidiaries",
		"Default excluded",
		"since, until",
		"trailing 365 days",
		"applied block",
		"as_of",
		"no free filter",
		"order_by",
		"1..500",
		"yyyy-mm-dd",
		"FLOW",
		"SNAPSHOT",
		"naming the rule and the fix",
		"compiled_sql",
		"read_lfx_deck_building_guidance",
	} {
		if !strings.Contains(standardMetricsDescription, want) {
			t.Errorf("description missing contract fragment %q", want)
		}
	}
}

// TestStandardMetricsSurface_NamesNoWarehouseRecipe pins the client vocabulary:
// the recipes are named in plain words on this side, and the dbt saved-query
// names behind the old contract (kpi_*) are gone from every surface a caller
// reads. It also pins that the unadvertised recipes stay unnamed here, and
// that the argument struct carries no field from the earlier contract.
func TestStandardMetricsSurface_NamesNoWarehouseRecipe(t *testing.T) {
	tool := listStandardMetricsTool(t)
	surface := map[string]string{
		"tool description":         standardMetricsDescription,
		"metric parameter":         schemaPropertyDescription(t, tool, "metric"),
		"standard metric guidance": standardMetricsGuidance,
		"semantic layer guidance":  semanticLayerGuidance,
		"deck building guidance":   deckBuildingGuidance,
	}
	dbtName := regexp.MustCompile(`\bkpi_[a-z0-9_]+`)
	for where, text := range surface {
		for _, named := range dbtName.FindAllString(text, -1) {
			t.Errorf("%s names the dbt saved query %q; recipes are named in client words here", where, named)
		}
		for _, gone := range []string{"saved_query", "saved query", "drop_group_bys"} {
			if strings.Contains(text, gone) {
				t.Errorf("%s carries %q, which is not part of the contract", where, gone)
			}
		}
	}
	// The events and training recipes stay callable through the lens
	// registry, and unnamed here: only the thirteen advertised names route.
	for _, unadvertised := range []string{"event_registrations", "training_enrollments"} {
		if strings.Contains(standardMetricsDescription, unadvertised) {
			t.Errorf("tool description names the unadvertised recipe family %q", unadvertised)
		}
		if strings.Contains(standardMetricsGuidance, unadvertised) {
			t.Errorf("standard metric guidance names the unadvertised recipe family %q", unadvertised)
		}
	}
	for _, field := range []string{"SavedQuery", "Foundation", "By", "Where"} {
		if _, ok := reflect.TypeOf(StandardMetricsArgs{}).FieldByName(field); ok {
			t.Errorf("StandardMetricsArgs still carries %s; it is not part of the contract", field)
		}
	}
}

// TestStandardMetrics_RequiredParamSurvivesCompaction mirrors the compaction contract on
// the other semantic layer tools: only the tool description and REQUIRED
// parameter descriptions reliably reach the model, so the inventory of standard metric
// names, the fixed-recipe rule and the FLOW/SNAPSHOT split must be stated on
// metric, not only on the optional parameters that carry them.
func TestStandardMetrics_RequiredParamSurvivesCompaction(t *testing.T) {
	desc := schemaPropertyDescription(t, listStandardMetricsTool(t), "metric")
	for _, want := range append([]string{
		"metrics", "group_by", "read_lfx_standard_metrics_guidance", "since/until", "as_of", "FLOW", "SNAPSHOT",
	}, standardMetricNames...) {
		if !strings.Contains(desc, want) {
			t.Errorf("metric description does not mention %q — the contract must survive schema compaction", want)
		}
	}
}

func TestStandardMetrics_RegistersReadOnly(t *testing.T) {
	tool := listStandardMetricsTool(t)
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Error("query_lfx_standard_metrics must carry ReadOnlyHint")
	}
	required := schemaRequired(t, tool)
	if !reflect.DeepEqual(required, []string{"metric"}) {
		t.Errorf("metric must be the only required field, got %v", required)
	}
}

// TestStandardMetrics_SchemaIsTheParameterContract pins the parameter set
// itself: every argument is a field of the lens request body, so a field added
// or dropped here changes the wire contract, not just the documentation.
func TestStandardMetrics_SchemaIsTheParameterContract(t *testing.T) {
	got := schemaProperties(t, listStandardMetricsTool(t))
	want := append([]string(nil), standardMetricParameters...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("input schema properties = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// The arguments reach the lens with their values untouched: the recipes, the
// scope filters and the rejections all live there, so anything this tool
// rewrote on the way would be a second, divergent copy of the contract. Only
// the shape changes, where the endpoint takes a list.
func TestStandardMetrics_SendsTheArgumentsAsGiven(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{
		Metric:       "contributors_by_org",
		Project:      "cncf",
		Subprojects:  "excluded",
		Org:          "International Business Machines Corporation",
		Subsidiaries: "combined",
		Since:        "2025-09-01",
		Until:        "2026-09-01",
		OrderBy:      "-total_contributors",
		Limit:        metricLimit(10),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if captured.Method != http.MethodPost || captured.Path != "/lfx-lens/semantic-layer/standard-metric" {
		t.Errorf("unexpected request: %s %s", captured.Method, captured.Path)
	}

	want := `{"metric":"contributors_by_org","project":"cncf","subprojects":"excluded",` +
		`"org":"International Business Machines Corporation","subsidiaries":"combined",` +
		`"since":"2025-09-01","until":"2026-09-01",` +
		`"order_by":["-total_contributors"],"limit":10}`
	if got := string(captured.Body); got != want {
		t.Errorf("request body =\n%s\nwant\n%s", got, want)
	}
}

// order_by is the one parameter whose wire spelling is not the tool's own:
// the endpoint reads a list, the tool takes a comma-separated string, so the
// split is pinned here rather than trusted.
func TestStandardMetrics_SendsOrderByAsAList(t *testing.T) {
	captured := setupLensTest(t)

	_, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{
		Metric:  "contributors_by_org",
		OrderBy: "-total_contributors, account",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var body struct {
		OrderBy []string `json:"order_by"`
	}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if want := []string{"-total_contributors", "account"}; !reflect.DeepEqual(body.OrderBy, want) {
		t.Errorf("order_by = %#v, want %#v", body.OrderBy, want)
	}
}

func TestStandardMetrics_OmitsUnsetArguments(t *testing.T) {
	captured := setupLensTest(t)

	if _, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{
		Metric: "maintainer_roster",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(captured.Body), `{"metric":"maintainer_roster"}`; got != want {
		t.Errorf("request body = %s, want %s", got, want)
	}

	if _, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{
		Metric: "maintainer_roster",
		Limit:  metricLimit(0),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(captured.Body), `{"metric":"maintainer_roster","limit":0}`; got != want {
		t.Errorf("request body = %s, want %s", got, want)
	}
}

// The lens returns the result in the client vocabulary already; the tool
// pretty-prints it and changes nothing, compiled_sql included.
func TestStandardMetrics_ReturnsTheLensBody(t *testing.T) {
	setupLensResponseTest(t, `{"columns":["account","parent_org","total_contributors"],"data":[{"account":"Red Hat LLC","parent_org":"International Business Machines Corporation","total_contributors":310}],"compiled_sql":"SELECT 1 WHERE x >= 2"}`)

	res, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{Metric: "contributors_by_org"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	text := resultText(t, res)
	var got struct {
		Columns     []string `json:"columns"`
		CompiledSQL string   `json:"compiled_sql"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if !reflect.DeepEqual(got.Columns, []string{"account", "parent_org", "total_contributors"}) {
		t.Errorf("columns = %v, want them passed through", got.Columns)
	}
	if got.CompiledSQL != "SELECT 1 WHERE x >= 2" {
		t.Errorf("compiled_sql = %q, want it passed through", got.CompiledSQL)
	}
}

// Lens rejections ARE the contract: each names the rule the call broke and
// the fix, so the caller reads that message and not a wrapper invented here.
func TestStandardMetrics_PassesLensRejectionsThrough(t *testing.T) {
	rejection := "members_and_dues_by_org is a SNAPSHOT metric: it reports the state on a date, so since/until do not apply. Use as_of, or pick a FLOW metric."
	setupLensErrorTest(t, http.StatusBadRequest, `{"detail":`+mustJSONString(t, rejection)+`}`)

	res, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{
		Metric: "members_and_dues_by_org",
		Since:  "2025-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if got := resultText(t, res); got != rejection {
		t.Errorf("tool error = %q, want the lens rejection verbatim: %q", got, rejection)
	}
}

// A failure that carries no message still reaches the caller whole, with its
// status: a silent empty error reads as an empty result.
func TestStandardMetrics_PassesUnstructuredErrorsThrough(t *testing.T) {
	setupLensErrorTest(t, http.StatusBadGateway, "upstream timed out")

	res, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{Metric: "contributors_by_org"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "upstream timed out") || !strings.Contains(text, "502") {
		t.Errorf("tool error = %q, want the status and the body", text)
	}
}

// mustJSONString encodes a string as a JSON literal, so a rejection message
// can be embedded in a fixture body without hand-escaping it.
func mustJSONString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("failed to encode %q: %v", value, err)
	}
	return string(encoded)
}

// ---------------------------------------------------------------------------
// Fake lens
// ---------------------------------------------------------------------------

// setupLensResponseTest points lensConfig at a stub lens API that returns the
// given body with 200.
func setupLensResponseTest(t *testing.T, response string) {
	t.Helper()
	setupFakeLens(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	})
}

// setupLensErrorTest points lensConfig at a stub lens API that always fails
// with the given status and body.
func setupLensErrorTest(t *testing.T, status int, body string) {
	t.Helper()
	setupFakeLens(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// setupFakeLens serves the given handler as the lens API for one test and
// restores the previous config afterwards. Tests using it must not run in
// parallel because lensConfig is a package-level global.
func setupFakeLens(t *testing.T, handler http.HandlerFunc) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := serviceapi.NewClient(serviceapi.Config{
		BaseURL:     srv.URL,
		TokenSource: stubTokenSource{},
	})
	if err != nil {
		t.Fatalf("failed to create service API client: %v", err)
	}
	prev := lensConfig
	SetLensConfig(&LensConfig{ServiceClient: client})
	t.Cleanup(func() { lensConfig = prev })
}
