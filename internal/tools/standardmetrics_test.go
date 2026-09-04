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

// standardMetricsDescriptionCeiling is far tighter than the hard schema
// budget: the description only ROUTES (the families, read the guidance,
// resolve names first); every grouping, switch, default and caveat lives in
// read_lfx_standard_metrics_guidance, which carries no byte budget. The
// ceiling leaves room for the next family name, not for more prose.
const standardMetricsDescriptionCeiling = 1000

// standardMetricParameters is the whole contract; anything else is not a parameter of
// this tool. since, until and as_of are gone, not aliased.
var standardMetricParameters = []string{
	"metric", "by", "project", "subprojects", "org", "subsidiaries",
	"start_date", "end_date", "period", "order_by", "limit",
}

// standardMetricNames is the whole inventory, in the order the guidance lists
// it: the lens registry exposes exactly these eighteen families (each with
// its own groupings under by), so this list is what the routing surface must
// name — no more, and none of them missing.
var standardMetricNames = []string{
	"memberships",
	"new_members",
	"membership_churn",
	"contributors",
	"contributions",
	"contributing_organizations",
	"participants",
	"maintainers",
	"maintainer_contributions",
	"project_health",
	"software_value",
	"event_registrations",
	"event_sponsorships",
	"speakers",
	"training_enrollments",
	"certifications",
	"social_mentions",
	"social_reach",
}

// standardMetricKinds is each family's kind as the guidance lists it: a
// window family counts between start_date and end_date, an at-date family
// reports the state on end_date. The four at-date families are also named
// on the required metric parameter, where they survive schema compaction.
var standardMetricKinds = map[string]string{
	"memberships":                "at-date",
	"new_members":                "window",
	"membership_churn":           "window",
	"contributors":               "window",
	"contributions":              "window",
	"contributing_organizations": "window",
	"participants":               "window",
	"maintainers":                "at-date",
	"maintainer_contributions":   "window",
	"project_health":             "at-date",
	"software_value":             "at-date",
	"event_registrations":        "window",
	"event_sponsorships":         "window",
	"speakers":                   "window",
	"training_enrollments":       "window",
	"certifications":             "window",
	"social_mentions":            "window",
	"social_reach":               "window",
}

// standardMetricGroupings is each family's groupings as the GUIDANCE lists
// them, in the order the lens offers them, the first being the default the
// lens applies when by is omitted. The description no longer carries them;
// TestStandardMetricsGuidanceContent derives the inventory rows from this
// map, so it is the single source.
var standardMetricGroupings = map[string]string{
	"memberships":                "total, org, tier, project, country, region",
	"new_members":                "total, org, project",
	"membership_churn":           "total, org, project",
	"contributors":               "total, org, project, country, region",
	"contributions":              "total, org, project, contributor, type, platform, org_region",
	"contributing_organizations": "total, project",
	"participants":               "total, org, project",
	"maintainers":                "total, org, project, maintainer",
	"maintainer_contributions":   "total, org, project, maintainer",
	"project_health":             "total, foundation, category, population",
	"software_value":             "total, foundation, population",
	"event_registrations":        "total, event, org",
	"event_sponsorships":         "total, org, event",
	"speakers":                   "total, event",
	"training_enrollments":       "total, org, course",
	"certifications":             "total, org",
	"social_mentions":            "total, project, network, sentiment",
	"social_reach":               "total, project",
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

// TestStandardMetricsDescription_ListsTheInventory pins the inventory line:
// a caller cannot guess a metric name, so every family is named in the
// description, in one line, ABOVE the line that routes to the guidance — a
// client that truncates the description keeps the names.
func TestStandardMetricsDescription_ListsTheInventory(t *testing.T) {
	want := "STANDARD METRICS " + strings.Join(standardMetricNames, ", ") + "."
	if !strings.Contains(standardMetricsDescription, want) {
		t.Errorf("description missing inventory line %q", want)
	}
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

// TestStandardMetricsDescription_OnlyRoutes pins the product decision on the
// description: function, never rationale. It says what the tool does, names
// the families, routes to the guidance and states the one rule a caller must
// not get wrong before reading anything (resolve names first). Groupings,
// switches, defaults, shapes and caveats live in the guidance, so their
// vocabulary must NOT be in the description.
func TestStandardMetricsDescription_OnlyRoutes(t *testing.T) {
	for _, want := range []string{
		"Run a governed standard metric",
		"one figure or one row per grouping",
		"scoped by project, organization and dates",
		"applied scope echoed",
		"STANDARD METRICS ",
		"Read read_lfx_standard_metrics_guidance BEFORE the first call",
		"again whenever in doubt",
		"every grouping (by), switch, default and caveat",
		"ALWAYS resolve names first",
		"search_projects",
		"search_b2b_orgs",
		"never pass a name they have not returned",
	} {
		if !strings.Contains(standardMetricsDescription, want) {
			t.Errorf("description missing routing fragment %q", want)
		}
	}
	for _, gone := range []string{
		"FLOW", "SNAPSHOT", "since", "until", "as_of", "excluded | separate | combined",
		"trailing 365", "yyyy-mm-dd", "compiled_sql", "No free filter", "PARAMETERS",
		"window family", "at-date family",
	} {
		if strings.Contains(standardMetricsDescription, gone) {
			t.Errorf("description carries %q; that is guidance, not routing", gone)
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
	// The removed date parameters are gone from the argument struct, not
	// aliased: a caller that sends one gets the lens rejection naming the
	// new word, and nothing here translates it silently.
	for _, field := range []string{"SavedQuery", "Foundation", "Where", "Since", "Until", "AsOf"} {
		if _, ok := reflect.TypeOf(StandardMetricsArgs{}).FieldByName(field); ok {
			t.Errorf("StandardMetricsArgs still carries %s; it is not part of the contract", field)
		}
	}
}

// TestStandardMetrics_RequiredParamSurvivesCompaction mirrors the compaction contract on
// the other semantic layer tools: only the tool description and REQUIRED
// parameter descriptions reliably reach the model, so the inventory of standard metric
// names, the fixed-recipe rule, the three date parameters and the
// window/at-date split must be stated on metric, not only on the optional
// parameters that carry them.
func TestStandardMetrics_RequiredParamSurvivesCompaction(t *testing.T) {
	desc := schemaPropertyDescription(t, listStandardMetricsTool(t), "metric")
	var atDate []string
	for _, name := range standardMetricNames {
		if standardMetricKinds[name] == "at-date" {
			atDate = append(atDate, name)
		}
	}
	for _, want := range append([]string{
		"metrics/group_by", "read_lfx_standard_metrics_guidance", "start_date, end_date and period",
		"WINDOW", "AT-DATE family (" + strings.Join(atDate, ", ") + ")", "state on end_date",
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
		Metric:       "contributors",
		By:           "org",
		Project:      "cncf",
		Subprojects:  "excluded",
		Org:          "International Business Machines Corporation",
		Subsidiaries: "combined",
		StartDate:    "2025-09-01",
		EndDate:      "2026-09-01",
		Period:       "month",
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

	want := `{"metric":"contributors","by":"org","project":"cncf","subprojects":"excluded",` +
		`"org":"International Business Machines Corporation","subsidiaries":"combined",` +
		`"start_date":"2025-09-01","end_date":"2026-09-01","period":"month",` +
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
		Metric:  "contributors",
		By:      "org",
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
		Metric: "maintainers",
		By:     "maintainer",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(captured.Body), `{"metric":"maintainers","by":"maintainer"}`; got != want {
		t.Errorf("request body = %s, want %s", got, want)
	}

	if _, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{
		Metric: "maintainers",
		Limit:  metricLimit(0),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(captured.Body), `{"metric":"maintainers","limit":0}`; got != want {
		t.Errorf("request body = %s, want %s", got, want)
	}
}

// The lens returns the result in the client vocabulary already; the tool
// pretty-prints it and changes nothing, compiled_sql included.
func TestStandardMetrics_ReturnsTheLensBody(t *testing.T) {
	setupLensResponseTest(t, `{"columns":["account","parent_org","total_contributors"],"data":[{"account":"Red Hat LLC","parent_org":"International Business Machines Corporation","total_contributors":310}],"compiled_sql":"SELECT 1 WHERE x >= 2"}`)

	res, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{Metric: "contributors", By: "org"})
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
	rejection := "start_date needs period for an at-date metric; the state on a single day is end_date alone."
	setupLensErrorTest(t, http.StatusBadRequest, `{"detail":`+mustJSONString(t, rejection)+`}`)

	res, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{
		Metric:    "memberships",
		StartDate: "2025-01-01",
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

// The org and project guards reject with an object: a message and the
// candidates the caller should pick from. Both reach the model; the
// candidates are the fix, so dropping them would leave the caller guessing.
func TestStandardMetrics_RendersGuardCandidates(t *testing.T) {
	setupLensErrorTest(t, http.StatusBadRequest, `{"detail":{"message":"no data-bearing account named 'IBM'. org takes the stored legal name; pick one of these","candidates":[{"account_name":"International Business Machines Corporation","account_rollup_name":"International Business Machines Corporation","active_memberships":23,"trailing_year_contributions":229350}]}}`)

	res, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{Metric: "contributions", Org: "IBM"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	text := resultText(t, res)
	for _, want := range []string{"no data-bearing account named 'IBM'", "pick one of these", "International Business Machines Corporation", "229350"} {
		if !strings.Contains(text, want) {
			t.Errorf("tool error = %q, want it to carry %q", text, want)
		}
	}
}

// A parameter the contract does not know (since, until, as_of, a typo) is a
// 422 whose detail is a LIST of validation errors; the message inside names
// the word to use, and that is what the model must read.
func TestStandardMetrics_RendersValidationErrors(t *testing.T) {
	setupLensErrorTest(t, http.StatusUnprocessableEntity, `{"detail":[{"type":"value_error","loc":["body"],"msg":"Value error, since is now start_date. Every family takes start_date, end_date and period."}]}`)

	res, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{Metric: "contributors"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := resultText(t, res), "since is now start_date. Every family takes start_date, end_date and period."; got != want {
		t.Errorf("tool error = %q, want %q", got, want)
	}
}

// TestDetailText pins every shape of lens detail the tool renders, and the
// fallback to nothing (so standardMetricError shows the whole body) when the
// detail is none of them.
func TestDetailText(t *testing.T) {
	for name, tc := range map[string]struct {
		detail string
		want   string
	}{
		"string": {`"limit must be at least 1, got 0."`, "limit must be at least 1, got 0."},
		"guard without candidates": {
			`{"message":"no project with slug 'zzz'. project takes the stored slug; resolve it with search_projects first","candidates":[]}`,
			"no project with slug 'zzz'. project takes the stored slug; resolve it with search_projects first",
		},
		"guard with no candidates key": {`{"message":"no data-bearing account named 'x'"}`, "no data-bearing account named 'x'"},
		"guard with candidates": {
			`{"message":"pick one of these","candidates":[{"slug":"k8s","name":"Kubernetes"}]}`,
			"pick one of these:\n[\n  {\n    \"name\": \"Kubernetes\",\n    \"slug\": \"k8s\"\n  }\n]",
		},
		"validation list on the body": {
			`[{"type":"value_error","loc":["body"],"msg":"Value error, since is now start_date."}]`,
			"since is now start_date.",
		},
		"validation list on a field": {
			`[{"type":"int_parsing","loc":["body","limit"],"msg":"Input should be a valid integer"}]`,
			"limit: Input should be a valid integer",
		},
		"several validation errors": {
			`[{"loc":["body","limit"],"msg":"Input should be a valid integer"},{"loc":["body","order_by"],"msg":"Input should be a valid list"}]`,
			"limit: Input should be a valid integer\norder_by: Input should be a valid list",
		},
		"number":       {`42`, ""},
		"empty object": {`{}`, ""},
		"empty list":   {`[]`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := detailText(json.RawMessage(tc.detail)); got != tc.want {
				t.Errorf("detailText(%s) = %q, want %q", tc.detail, got, tc.want)
			}
		})
	}
	// An unrenderable detail falls back to the status and the whole body.
	if got := standardMetricError([]byte(`{"detail":42}`), 400); got != `Error (HTTP 400): {"detail":42}` {
		t.Errorf("standardMetricError fallback = %q", got)
	}
}

// A failure that carries no message still reaches the caller whole, with its
// status: a silent empty error reads as an empty result.
func TestStandardMetrics_PassesUnstructuredErrorsThrough(t *testing.T) {
	setupLensErrorTest(t, http.StatusBadGateway, "upstream timed out")

	res, _, err := handleStandardMetrics(context.Background(), &mcp.CallToolRequest{}, StandardMetricsArgs{Metric: "contributors", By: "org"})
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
