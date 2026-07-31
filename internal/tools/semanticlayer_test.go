// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/dbtsl"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// capturedGraphQL records the GraphQL requests the stub semantic layer saw.
type capturedGraphQL struct {
	Operations []string
	Variables  []map[string]any
}

// operation returns the variables sent for the named GraphQL operation, and
// whether it was called at all.
func (c *capturedGraphQL) operation(name string) (map[string]any, bool) {
	for i, op := range c.Operations {
		if op == name {
			return c.Variables[i], true
		}
	}
	return nil, false
}

// setupSemanticLayerTest points the shared semanticLayerConfig at a stub dbt
// Semantic Layer that answers every operation with a small fixed payload. The
// previous config is restored on cleanup. Tests using this must not run in
// parallel, because semanticLayerConfig is a package-level global.
func setupSemanticLayerTest(t *testing.T) *capturedGraphQL {
	t.Helper()

	captured := &capturedGraphQL{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)

		op := graphQLOperation(req.Query)
		captured.Operations = append(captured.Operations, op)
		captured.Variables = append(captured.Variables, req.Variables)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stubResponses[op]))
	}))
	t.Cleanup(srv.Close)

	client, err := dbtsl.NewClient(dbtsl.Config{
		Host:          srv.URL,
		EnvironmentID: "1",
		Token:         "test-token",
	})
	if err != nil {
		t.Fatalf("failed to create semantic layer client: %v", err)
	}

	prev := semanticLayerConfig
	SetSemanticLayerConfig(&SemanticLayerConfig{Client: client})
	t.Cleanup(func() { semanticLayerConfig = prev })

	return captured
}

// stubResponses answers each GraphQL operation with the smallest payload the
// handlers will accept. The metric and dimension names are real ones, so a test
// asserting on them is asserting something the allowlist also permits.
var stubResponses = map[string]string{
	"GetMetrics": `{"data":{"metricsPaginated":{"items":[
	  {"name":"total_contributors","label":"Contributors","description":"Contributor count","type":"simple"},
	  {"name":"active_maintainers","label":"Active maintainers","description":"Maintainer count","type":"simple"},
	  {"name":"current_membership_revenue","label":"Revenue","description":"Membership revenue","type":"simple"}
	]}}}`,
	"GetMetricsWithRelated": `{"data":{"metricsPaginated":{"items":[
	  {"name":"total_contributors","label":"Contributors","description":"Contributor count","type":"simple","dimensions":[{"name":"country__lf_region"}],"entities":[{"name":"country"}]},
	  {"name":"active_maintainers","label":"Active maintainers","description":"Maintainer count","type":"simple","dimensions":[{"name":"country__lf_region"}],"entities":[]},
	  {"name":"current_membership_revenue","label":"Revenue","description":"Membership revenue","type":"simple","dimensions":[{"name":"country__lf_region"}],"entities":[]}
	]}}}`,
	"GetDimensions": `{"data":{"dimensionsPaginated":{"items":[
	  {"name":"country__lf_region","type":"categorical","description":"Region","label":"Region","queryableGranularities":[]},
	  {"name":"asset_id__membership_tier","type":"categorical","description":"Tier","label":"Tier","queryableGranularities":[]}
	]}}}`,
	"CreateQuery": `{"data":{"createQuery":{"queryId":"q-1"}}}`,
	"GetQueryResult": `{"data":{"query":{"status":"SUCCESSFUL","error":null,"sql":"SELECT 1",
	  "jsonResult":"{\"schema\":{\"fields\":[{\"name\":\"country__lf_region\",\"type\":\"string\"}],\"primaryKey\":[]},\"data\":[{\"country__lf_region\":\"Asia Pacific\"}]}"}}}`,
}

// graphQLOperation extracts the operation name from a GraphQL document.
func graphQLOperation(query string) string {
	for _, line := range strings.Split(query, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"query ", "mutation "} {
			if rest, found := strings.CutPrefix(line, prefix); found {
				if idx := strings.IndexAny(rest, "( {"); idx > 0 {
					return rest[:idx]
				}
				return rest
			}
		}
	}
	return "unknown"
}

// schemaProperties returns the property names in a tool's input schema.
func schemaProperties(t *testing.T, tool *mcp.Tool) []string {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("failed to marshal input schema: %v", err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("failed to parse input schema: %v", err)
	}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	return names
}

// ---------------------------------------------------------------------------
// Handler behaviour
// ---------------------------------------------------------------------------

// TestSemanticLayerQueryReachesTheSemanticLayerDirectly is the point of the
// whole change: the handler talks to the dbt Semantic Layer itself, with no
// lfx-lens hop on the path.
func TestSemanticLayerQueryReachesTheSemanticLayerDirectly(t *testing.T) {
	captured := setupSemanticLayerTest(t)

	res, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
		Metrics: "active_maintainers",
		GroupBy: "country__lf_region",
		OrderBy: "-active_maintainers",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}

	vars, called := captured.operation("CreateQuery")
	if !called {
		t.Fatal("expected the handler to submit a query to the semantic layer")
	}

	metrics, _ := vars["metrics"].([]any)
	if len(metrics) != 1 {
		t.Fatalf("expected one metric, got %v", vars["metrics"])
	}
	if name, _ := metrics[0].(map[string]any)["name"].(string); name != "active_maintainers" {
		t.Errorf("unexpected metric: %v", metrics[0])
	}
	if vars["limit"] != float64(10) {
		t.Errorf("expected the limit forwarded, got %v", vars["limit"])
	}

	orderBy, _ := vars["orderBy"].([]any)
	if len(orderBy) != 1 {
		t.Fatalf("expected one order term, got %v", vars["orderBy"])
	}
	if desc, _ := orderBy[0].(map[string]any)["descending"].(bool); !desc {
		t.Errorf("expected the - prefix to mean descending, got %v", orderBy[0])
	}
}

// TestSemanticLayerQueryOmitsAnAbsentWhere keeps an empty filter from becoming
// an empty clause, which the semantic layer would reject.
func TestSemanticLayerQueryOmitsAnAbsentWhere(t *testing.T) {
	captured := setupSemanticLayerTest(t)

	if _, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
		Metrics: "active_maintainers",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vars, _ := captured.operation("CreateQuery")
	if _, present := vars["where"]; present {
		t.Errorf("expected where to be absent, got %v", vars["where"])
	}
}

func TestSemanticLayerQueryForwardsAWhereClause(t *testing.T) {
	captured := setupSemanticLayerTest(t)

	const filter = "{{ Dimension('maintainer_key__project_slug') }} = 'cncf'"
	if _, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
		Metrics: "active_maintainers",
		Where:   filter,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vars, _ := captured.operation("CreateQuery")
	where, _ := vars["where"].([]any)
	if len(where) != 1 {
		t.Fatalf("expected one where clause, got %v", vars["where"])
	}
	if sql, _ := where[0].(map[string]any)["sql"].(string); sql != filter {
		t.Errorf("where clause = %q; want it forwarded verbatim", sql)
	}
}

// TestSemanticLayerQueryRejectsAMetricOutsideTheAllowlist keeps the allowlist
// enforced in-process, now that there is no lens route to enforce it.
func TestSemanticLayerQueryRejectsAMetricOutsideTheAllowlist(t *testing.T) {
	captured := setupSemanticLayerTest(t)

	res, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
		Metrics: "some_internal_metric",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a metric outside the allowlist to be rejected")
	}
	if _, called := captured.operation("CreateQuery"); called {
		t.Error("expected no query to reach the semantic layer")
	}
	if text := resultText(t, res); !strings.Contains(text, "list_metrics") {
		t.Errorf("rejection should name the way forward: %q", text)
	}
}

func TestSemanticLayerListMetrics(t *testing.T) {
	captured := setupSemanticLayerTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "list_metrics",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if _, called := captured.operation("GetMetrics"); !called {
		t.Error("expected the metrics metadata query to run")
	}
	if text := resultText(t, res); !strings.Contains(text, "total_contributors") {
		t.Errorf("expected the metric list in the result: %q", text)
	}
}

func TestSemanticLayerGetDimensions(t *testing.T) {
	captured := setupSemanticLayerTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action:  "get_dimensions",
		Metrics: "active_maintainers",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if _, called := captured.operation("GetDimensions"); !called {
		t.Error("expected the dimensions metadata query to run")
	}
}

// TestSemanticLayerGetDimensionsFiltersOnSearch covers the narrowing that the
// lens route used to do, since the upstream API has no dimension search.
func TestSemanticLayerGetDimensionsFiltersOnSearch(t *testing.T) {
	setupSemanticLayerTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action:  "get_dimensions",
		Metrics: "active_maintainers",
		Search:  "tier",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "asset_id__membership_tier") {
		t.Errorf("expected the matching dimension: %q", text)
	}
	if strings.Contains(text, "country__lf_region") {
		t.Errorf("expected the non-matching dimension filtered out: %q", text)
	}
}

func TestSemanticLayerLimitTooLarge(t *testing.T) {
	setupSemanticLayerTest(t)

	res, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
		Metrics: "active_maintainers",
		Limit:   501,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || resultText(t, res) != "Error: limit must be 500 or less" {
		t.Errorf("expected limit error, got: %q (IsError=%v)", resultText(t, res), res.IsError)
	}
}

// TestSemanticLayerDefaultsTheLimitToTheAdvertisedCeiling: an omitted or
// negative limit reaches the API as "no limit", which returns one result page
// and reports its length as the row count — 1024 rows presented as the whole
// answer. The tool description promises a ceiling of 500, so that is what an
// unspecified limit has to mean.
func TestSemanticLayerDefaultsTheLimitToTheAdvertisedCeiling(t *testing.T) {
	for _, limit := range []int{0, -1} {
		captured := setupSemanticLayerTest(t)

		res, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
			Metrics: "active_maintainers",
			Limit:   limit,
		})
		if err != nil {
			t.Fatalf("limit %d: unexpected error: %v", limit, err)
		}
		if res.IsError {
			t.Fatalf("limit %d: unexpected error result: %s", limit, resultText(t, res))
		}

		vars, called := captured.operation("CreateQuery")
		if !called {
			t.Fatalf("limit %d: expected a query to be created", limit)
		}
		if got := vars["limit"]; got != float64(maxQueryLimit) {
			t.Errorf("limit %d: expected the query capped at %d, got %v", limit, maxQueryLimit, got)
		}
	}
}

// TestSlugLookupNoteFiresOnlyOnSlugDimensions: the note redirects slug
// lookups to search_projects, which answers by name directly. It must not
// attach to ordinary dimensions, where it would be noise on every call.
func TestSlugLookupNoteFiresOnlyOnSlugDimensions(t *testing.T) {
	for _, dimension := range []string{
		"registration_id__project_slug",
		"account_project_month_id__project_slug",
		" project_spine_slug ",
	} {
		if slugLookupNote(dimension) == "" {
			t.Errorf("expected a note for %q", dimension)
		}
	}
	for _, dimension := range []string{
		"country__lf_region",
		"country__country_name",
		"asset_id__membership_tier",
		"activity_project_id__organization_name",
		// Guards against matching on "slug" anywhere in the name.
		"health_metric_key__slug_status",
	} {
		if note := slugLookupNote(dimension); note != "" {
			t.Errorf("expected no note for %q, got %q", dimension, note)
		}
	}
}

// The note has to reach the caller as its own content block, so it cannot be
// read as part of the value list.
func TestSemanticLayerDimensionValuesCarriesTheSlugNote(t *testing.T) {
	prev := stubResponses["GetDimensions"]
	stubResponses["GetDimensions"] = `{"data":{"dimensionsPaginated":{"items":[
	  {"name":"registration_id__project_slug","type":"categorical","description":"Slug","label":"Slug","queryableGranularities":[]}
	]}}}`
	t.Cleanup(func() { stubResponses["GetDimensions"] = prev })

	setupSemanticLayerTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action:    "get_dimension_values",
		Dimension: "registration_id__project_slug",
		Metrics:   "total_contributors",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if len(res.Content) != 2 {
		t.Fatalf("expected the values and the note as separate blocks, got %d", len(res.Content))
	}
	note, ok := res.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected a text block, got %T", res.Content[1])
	}
	if !strings.Contains(note.Text, "search_projects") {
		t.Errorf("expected the note to name search_projects, got %q", note.Text)
	}
}

// TestSemanticLayerHelpQueryDescribesWhereScoping checks the help text moved
// off the removed scope parameter and onto the where clause.
func TestSemanticLayerHelpQueryDescribesWhereScoping(t *testing.T) {
	setupSemanticLayerTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "describe",
		Target: "query",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "there is no scope parameter") {
		t.Errorf("query help should say scoping happens in where: %q", text)
	}
	if strings.Contains(text, "project_slug  cncf") {
		t.Errorf("query help still shows a project_slug argument: %q", text)
	}
}

// ---------------------------------------------------------------------------
// Description content
// ---------------------------------------------------------------------------

// TestSemanticLayerDescriptions_FitSchemaBudget guards that limit for both
// semantic layer tools. Splitting discovery from querying gave each its own
// budget, which is the point of the split.
func TestSemanticLayerDescriptions_FitSchemaBudget(t *testing.T) {
	for name, desc := range map[string]string{
		"explore_lfx_semantic_layer": exploreSemanticLayerDescription,
		"query_lfx_semantic_layer":   querySemanticLayerDescription,
	} {
		if got := len(desc); got > schemaDescriptionBudget {
			t.Errorf("%s description is %d bytes; everything past %d is invisible to the model — move detail into help",
				name, got, schemaDescriptionBudget)
		}
	}
}

// TestExploreSemanticLayerDescription checks the discovery tool carries the
// routing contract: which domains are ours, and when to use query_lfx_lens.
func TestExploreSemanticLayerDescription(t *testing.T) {
	for _, want := range []string{
		// The domains are named explicitly. Without them the routing is
		// one-sided — query_lfx_lens lists concrete triggers while this tool
		// describes itself abstractly, so every specific question looks like a
		// better match for the other tool.
		// Search terms must be words the Semantic Layer actually matches.
		// The earlier headings were plurals — "contributions", "memberships",
		// "education", "project health" all returned zero metrics, and a live
		// client followed the instruction, got [], and fell back to
		// query_lfx_lens. These singular forms are verified against the API.
		"contributor, contribution —",
		"membership, revenue, churn —",
		"event, registration, speaker —",
		"enrollment, certification —",
		"maintainer —",
		"health, project —",
		// Regional questions route here for every topic, memberships included.
		"any of the above sliced by country or region — always here, never query_lfx_lens",
		// Dimension naming, and the regional person-vs-organization split.
		"entity__field",
		"country__lf_region",
		"activity_project_id__organization_lf_region",
		// Discovery must hand off to the query tool by name.
		"query_lfx_semantic_layer",
		// The value-discovery action, and the reason it exists. A filter naming
		// a real dimension but an unknown literal returns zero rows instead of
		// erroring, so a wrong guess is indistinguishable from missing data. A
		// live client burned five query attempts on 'APAC' and 'Vietnam' before
		// escaping via a country code.
		"get_dimension_values(dimension, metrics, search)",
		"returns zero rows, not an error",
		"'Asia Pacific' not 'APAC'",
		"'Viet Nam' not 'Vietnam'",
		// Either tool can be loaded without the other, so each states what the
		// semantic layer is. Here the regional rule sits in COVERS, asserted
		// above, rather than in the opening line.
		"query and data-exploration tool",
	} {
		if !strings.Contains(exploreSemanticLayerDescription, want) {
			t.Errorf("explore description missing %q", want)
		}
	}

	// Event sponsorships stay with query_lfx_lens, which does them better, so
	// this tool must not advertise them. Listing "sponsorship" as a topic here
	// put two tools in charge of the same question and contradicted the
	// carve-out query_lfx_lens still states.
	if strings.Contains(exploreSemanticLayerDescription, "sponsorship,") {
		t.Error("explore description claims sponsorships as a topic; query_lfx_lens owns them")
	}

	// The description used to warn that a plural search matches nothing. That
	// stopped being true once lens learned to fall back to a singular stem:
	// "memberships" now returns 18 metrics, "contributions" 2. Telling the model
	// otherwise wastes the budget on a false constraint.
	for _, unwanted := range []string{
		"a plural like",
		"matches nothing",
	} {
		if strings.Contains(exploreSemanticLayerDescription, unwanted) {
			t.Errorf("explore description still warns about plurals, which lens now handles: %q", unwanted)
		}
	}
}

// TestQuerySemanticLayerDescription checks the query tool is self-sufficient:
// its own description carries the syntax, so a caller never has to call help
// first.
func TestQuerySemanticLayerDescription(t *testing.T) {
	for _, want := range []string{
		"metrics (required)",
		"Dimension(",
		"TimeDimension(",
		"yyyy-mm-dd",
		"ceiling 500",
		"metric_time__year",
		"The join is outer",
		"ranked list",
		// Splitting discovery out made it possible to query without ever
		// exploring, and a live client did exactly that — going straight to a
		// query with guessed names. The rule has to be an instruction, not a
		// conditional suggestion.
		"ALWAYS call explore_lfx_semantic_layer first",
		"never guess",
		// Both neighbours are named so routing works from this tool too.
		"explore_lfx_semantic_layer",
		"query_lfx_lens",
		"query and data-exploration tool",
		"anything sliced by country or region",
		// The silent-zero-rows warning is only actionable if it names the way
		// out; without this the model retries the same wrong literal.
		"get_dimension_values",
	} {
		if !strings.Contains(querySemanticLayerDescription, want) {
			t.Errorf("query description missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"MUST include a project scope filter",
		// Scope validation is gone, so a promise to validate a project filter
		// against a foundation subtree would now be a lie to the model.
		"validated against that foundation",
		// Framings that understate the tool and misroute the questions it
		// exists to answer: it compiles SQL per request rather than serving
		// stored rollups, and grouping by a name dimension returns lists of
		// named organizations and people, not only figures.
		"pre-aggregated",
		"returns numbers, not records",
	} {
		if strings.Contains(querySemanticLayerDescription, unwanted) {
			t.Errorf("query description must not contain %q", unwanted)
		}
	}
}

// TestSemanticLayerArgs_FieldsFitSchemaBudget holds the other half of the
// budget contract: each property description is a separate field, so each must
// independently stay under the limit.
func TestSemanticLayerArgs_FieldsFitSchemaBudget(t *testing.T) {
	for _, tc := range []struct {
		tool  *mcp.Tool
		props []string
	}{
		{listExploreTool(t), []string{"action", "search", "metrics", "dimension", "target"}},
		{listQueryTool(t), []string{"metrics", "group_by", "where", "order_by", "limit"}},
	} {
		for _, property := range tc.props {
			if got := len(schemaPropertyDescription(t, tc.tool, property)); got > schemaDescriptionBudget {
				t.Errorf("%s.%s description is %d bytes; everything past %d is invisible to the model",
					tc.tool.Name, property, got, schemaDescriptionBudget)
			}
		}
	}
}

// TestCriticalGuidanceSurvivesSchemaCompaction is the load-bearing test for
// where guidance is allowed to live.
//
// Clients that defer tool schemas behind a search index re-serialise them and
// replace OPTIONAL parameter descriptions with a short generated summary. This
// was verified against a live client: the 459-byte where description arrived as
// "Filter conditions.", order_by as "Sort order.", and limit as no description
// at all — until limit was temporarily marked required, at which point its real
// text appeared. Only the tool description and required parameters survive.
//
// So syntax the model cannot guess must not live solely on an optional
// parameter. Keeping the full text there is fine and useful for clients that do
// pass it through; it just may not be the only copy.
func TestCriticalGuidanceSurvivesSchemaCompaction(t *testing.T) {
	var surviving string
	for _, tool := range []*mcp.Tool{listExploreTool(t), listQueryTool(t)} {
		surviving += "\n" + tool.Description
		for _, name := range schemaRequired(t, tool) {
			surviving += "\n" + schemaPropertyDescription(t, tool, name)
		}
	}

	for _, tc := range []struct {
		token string
		why   string
	}{
		{"Dimension(", "categorical filter syntax is unguessable"},
		{"TimeDimension(", "time filter syntax is unguessable"},
		{"yyyy-mm-dd", "date format silently returns wrong rows if guessed"},
		{"ceiling 500", "over-limit requests are rejected outright"},
		{"metric_time__year", "the only way to build a trend"},
		{"entity__field", "dimension names cannot be assembled by hand"},
		{"outer-joined", "explains NULLs in cross-domain results"},
		{"raw IDs", "grouping by an entity silently returns unusable output"},
		{"get_dimension_values", "the only recovery from a wrong filter literal"},
		{"zero rows", "a wrong literal is silent, so the model must be told to check first"},
	} {
		if !strings.Contains(surviving, tc.token) {
			t.Errorf("%q reaches the model only via an optional parameter, where it gets summarised away (%s). Move it into the tool description or onto a required parameter.",
				tc.token, tc.why)
		}
	}
}

func listExploreTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return listRegisteredTool(t, "explore_lfx_semantic_layer", RegisterExploreSemanticLayer)
}

func listQueryTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return listRegisteredTool(t, "query_lfx_semantic_layer", RegisterQuerySemanticLayer)
}

func TestRegisterSemanticLayer_Schema(t *testing.T) {
	explore := listExploreTool(t)
	query := listQueryTool(t)

	// Discovery: action is the only required field, and it must name exactly
	// the actions the dispatcher accepts — a stale list sends the model to an
	// action that errors. Querying lives on the other tool now.
	exploreRequired := schemaRequired(t, explore)
	if !contains(exploreRequired, "action") {
		t.Errorf("explore required = %v; expected to contain action", exploreRequired)
	}
	if contains(exploreRequired, "metrics") {
		t.Errorf("explore required = %v; metrics is only needed for get_dimensions", exploreRequired)
	}
	action := schemaPropertyDescription(t, explore, "action")
	for _, want := range []string{"list_metrics", "get_dimensions", "get_dimension_values", "help"} {
		if !strings.Contains(action, want) {
			t.Errorf("action schema description missing %q: %q", want, action)
		}
	}
	if strings.Contains(action, "describe") {
		t.Errorf("action schema description still advertises the renamed describe action: %q", action)
	}

	// Query: metrics is required, so its multi-metric join rules survive schema
	// compaction. Everything else stays optional — above all project_slug,
	// whose whole point is that global questions omit it.
	queryRequired := schemaRequired(t, query)
	if !contains(queryRequired, "metrics") {
		t.Errorf("query required = %v; metrics must be required so its guidance survives compaction", queryRequired)
	}
	for _, optional := range []string{"where", "group_by", "order_by", "limit"} {
		if contains(queryRequired, optional) {
			t.Errorf("query required = %v; %s must stay optional", queryRequired, optional)
		}
	}

	// The optional descriptions are still expected to be complete, for clients
	// that pass them through unchanged.
	where := schemaPropertyDescription(t, query, "where")
	for _, want := range []string{"Dimension(", "TimeDimension(", "yyyy-mm-dd"} {
		if !strings.Contains(where, want) {
			t.Errorf("where schema description missing %q: %q", want, where)
		}
	}
	groupBy := schemaPropertyDescription(t, query, "group_by")
	if !strings.Contains(groupBy, "metric_time__year") {
		t.Errorf("group_by schema description missing the trend grain: %q", groupBy)
	}
	// There is no scope parameter any more: a caller restricts a query by
	// putting the project filter in where, like any other filter. A leftover
	// project_slug property would read as a scoping guarantee that nothing
	// enforces.
	if contains(schemaProperties(t, query), "project_slug") {
		t.Error("query tool still exposes project_slug; scoping is done in the where clause now")
	}
}

// TestQueryToolRejectsMissingMetricsWithAPointer keeps the recovery path alive
// for a caller on a cached schema that still sends action=query here.
func TestQueryToolRejectsMissingMetricsWithAPointer(t *testing.T) {
	setupSemanticLayerTest(t)

	res, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result when metrics is empty")
	}
	if text := resultText(t, res); !strings.Contains(text, "explore_lfx_semantic_layer") {
		t.Errorf("missing-metrics error should point at the discovery tool: %q", text)
	}
}

// TestExploreToolRedirectsQueryAction covers the other half of that migration:
// a caller still passing action=query to the discovery tool gets told where
// querying moved rather than a bare unknown-action error.
func TestExploreToolRedirectsQueryAction(t *testing.T) {
	setupSemanticLayerTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "query",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for action=query on the discovery tool")
	}
	if text := resultText(t, res); !strings.Contains(text, "query_lfx_semantic_layer") {
		t.Errorf("redirect should name the query tool: %q", text)
	}
}

// TestHelpActionAndDescribeAlias checks the renamed action works and that the
// old action word still dispatches on this tool. It deliberately does NOT claim
// to cover the pre-split schema: that caller addresses query_lfx_semantic_layer,
// which no longer accepts an action, so no assertion here can exercise it.
func TestHelpActionAndDescribeAlias(t *testing.T) {
	setupSemanticLayerTest(t)

	for _, action := range []string{"help", "describe"} {
		res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
			Action: action,
		})
		if err != nil {
			t.Fatalf("action %q: unexpected error: %v", action, err)
		}
		if text := resultText(t, res); !strings.Contains(text, "how to use it") {
			t.Errorf("action %q did not return the help overview: %q", action, text)
		}
	}

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "help",
		Target: "query",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The worked examples are the reason help exists; they must survive the
	// move off the description.
	if text := resultText(t, res); !strings.Contains(text, "metric_time__year") {
		t.Errorf("help query text missing the trend example: %q", text)
	}
}

// TestUnknownActionListsTheRealActions guards the recovery message against
// drift: it is what a model reads after guessing an action name, so an action
// missing here is one it will not retry with.
func TestUnknownActionListsTheRealActions(t *testing.T) {
	setupSemanticLayerTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "list_dimension_values",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for an unknown action")
	}
	text := resultText(t, res)
	for _, want := range []string{"list_metrics", "get_dimensions", "get_dimension_values", "help"} {
		if !strings.Contains(text, want) {
			t.Errorf("unknown-action error missing %q: %q", want, text)
		}
	}
}

// TestHelpCoversGetDimensionValues checks the long-form guidance is reachable.
// It is the only place that records the billing_country trap, which has no room
// in the 2048-byte description.
func TestHelpCoversGetDimensionValues(t *testing.T) {
	setupSemanticLayerTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "help",
		Target: "get_dimension_values",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	text := resultText(t, res)
	for _, want := range []string{
		"zero rows",
		"'Asia Pacific'",
		"Viet Nam",
		// asset_id__billing_country is free text holding both spellings, so a
		// filter on it drops members filed under the other one. The transcript
		// that motivated this work "succeeded" on exactly that dimension.
		"asset_id__billing_country",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("get_dimension_values help missing %q", want)
		}
	}

	// The overview must advertise the target, or nothing points at it.
	overview, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "help",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text := resultText(t, overview); !strings.Contains(text, "get_dimension_values") {
		t.Errorf("help overview does not mention get_dimension_values: %q", text)
	}
}

// TestSemanticLayerToolsRegisterIndependently guards tool selection.
//
// Both tools used to be added by one function behind one gate keyed to
// "query_lfx_semantic_layer", so LFXMCP_TOOLS=explore_lfx_semantic_layer
// registered nothing at all, and selecting only the query tool silently
// exposed both. Each name must control exactly its own tool.
func TestSemanticLayerToolsRegisterIndependently(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
		absent   string
	}{
		{"explore_lfx_semantic_layer", RegisterExploreSemanticLayer, "query_lfx_semantic_layer"},
		{"query_lfx_semantic_layer", RegisterQuerySemanticLayer, "explore_lfx_semantic_layer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tool := listRegisteredTool(t, tc.name, tc.register); tool == nil {
				t.Fatalf("%s did not register itself", tc.name)
			}
			if found := findRegisteredTool(t, tc.absent, tc.register); found != nil {
				t.Errorf("registering %s also exposed %s; each name must select only its own tool",
					tc.name, tc.absent)
			}
		})
	}
}
