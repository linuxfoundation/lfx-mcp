// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/serviceapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubTokenSource struct{}

func (stubTokenSource) GetToken(_ context.Context) (string, error) {
	return "test-token", nil
}

// capturedLensRequest records the last request received by the stub lens API.
type capturedLensRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
}

// setupLensTest points the shared lensConfig at a stub lens API server that
// captures requests and returns a small JSON payload. The previous config is
// restored on test cleanup. Tests using this must not run in parallel because
// lensConfig is a package-level global.
func setupLensTest(t *testing.T) *capturedLensRequest {
	t.Helper()

	captured := &capturedLensRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Method = r.Method
		captured.Path = r.URL.Path
		captured.Query = r.URL.Query()
		body, _ := io.ReadAll(r.Body)
		captured.Body = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
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

	return captured
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected a result with content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return text.Text
}

// ---------------------------------------------------------------------------
// Handler behavior
// ---------------------------------------------------------------------------

func TestSemanticLayer_UnfilteredQueryOmitsScopeAndWhere(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
		Metrics: "active_maintainers",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if captured.Method != http.MethodPost || captured.Path != "/lfx-lens/semantic-layer/query" {
		t.Errorf("unexpected request: %s %s", captured.Method, captured.Path)
	}

	var body map[string]any
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("failed to parse captured body: %v", err)
	}
	if _, ok := body["project_slug"]; ok {
		t.Errorf("expected project_slug key to be absent from request body, got: %v", body["project_slug"])
	}
	if _, ok := body["where"]; ok {
		t.Errorf("expected where key to be absent from request body, got: %v", body["where"])
	}
}

func TestSemanticLayer_ScopedQueryUsesWhereOnly(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
		Metrics: "total_contributors",
		Where:   "{{ Dimension('activity_project_id__project_spine_slug') }} = 'cncf'",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}

	var body map[string]any
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("failed to parse captured body: %v", err)
	}
	if _, ok := body["project_slug"]; ok {
		t.Errorf("project_slug must not be sent; scope belongs in where: %v", body["project_slug"])
	}
	if _, ok := body["where"]; !ok {
		t.Error("expected where key in request body")
	}
}

func TestSemanticLayer_ListMetricsWithoutProjectSlug(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "list_metrics",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if captured.Path != "/lfx-lens/semantic-layer/metrics" {
		t.Errorf("unexpected request path: %s", captured.Path)
	}
}

func TestSemanticLayer_GetDimensionsWithoutProjectSlug(t *testing.T) {
	captured := setupLensTest(t)

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
	if captured.Path != "/lfx-lens/semantic-layer/dimensions" {
		t.Errorf("unexpected request path: %s", captured.Path)
	}
}

func TestSemanticLayer_LimitTooLarge(t *testing.T) {
	setupLensTest(t)

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

func TestSemanticLayer_DescribeQuery(t *testing.T) {
	setupLensTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "describe",
		Target: "query",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, res)
	for _, want := range []string{
		"There is no separate project parameter",
		"activity_project_id__project_spine_slug",
		"prior 365 complete UTC days",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("describe query text missing %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Description content
// ---------------------------------------------------------------------------

// schemaDescriptionBudget is the hard limit that makes the rest of this
// guidance meaningful: descriptions shipped in tools/list are truncated at 2048
// before the model sees them. Anything past the cut is silently invisible —
// which is what happened to the tlf membership caveat and the project_name tip
// for as long as they sat at the end of the description.
//
// These checks measure bytes, because len() on a Go string is bytes and the
// prose is full of em-dashes at 3 bytes each — the description runs ~30 bytes
// over its character count. Whether the truncation upstream counts bytes,
// characters or tokens is not something we control or have measured, so the
// budget is enforced against the larger number.
const schemaDescriptionBudget = 2048

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
		"event, registration, speaker, sponsorship —",
		"enrollment, certification —",
		"maintainer —",
		"health, project —",
		// Regional questions route here for every topic, memberships included.
		"any of the above sliced by country or region — always here, never query_lfx_lens",
		// Dimension naming. The regional person-vs-organization split moved to
		// help('doctrine') (asserted in TestDoctrineHelp) to make room for the
		// eval-verified failure patterns.
		"entity__field",
		"country__lf_region",
		// Discovery must hand off to the query tool by name and explain where
		// project scope is expressed now that there is no project parameter.
		"query_lfx_semantic_layer",
		"Project scope lives in query's where clause (no parameter)",
		"resolve slugs via search_projects",
		// The value-discovery action, and the reason it exists. A filter naming
		// a real dimension but an unknown literal returns zero rows instead of
		// erroring, so a wrong guess is indistinguishable from missing data. A
		// live client burned five query attempts on 'APAC' and 'Vietnam' before
		// escaping via a country code.
		"get_dimension_values(dimension, metrics, search)",
		"returns zero rows, not an error",
		// The 207-question eval showed models falling back to query_lfx_lens
		// without ever reading the recipes; the description now routes
		// struggling through help('doctrine') first.
		"help('doctrine')",
		"ALWAYS call it before a query_lfx_lens fallback",
		"maintainer-contribution joins, social listening",
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

	// Sponsorships flipped owners: the semantic layer covers them now
	// (sponsored_events_count and the sponsorship revenue metrics), and
	// query_lfx_lens keeps exactly two lanes. Both descriptions must agree.
	if strings.Contains(exploreSemanticLayerDescription, "never query_lfx_lens") &&
		!strings.Contains(exploreSemanticLayerDescription, "sponsorship") {
		t.Error("explore description no longer claims sponsorships; the semantic layer owns them now")
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
		"outer-join",
		"ranked list",
		// Splitting discovery out made it possible to query without ever
		// exploring, and a live client did exactly that — going straight to a
		// query with guessed names. The rule has to be an instruction, not a
		// conditional suggestion.
		"ALWAYS explore_lfx_semantic_layer first",
		"never guess",
		// Both neighbours are named so routing works from this tool too.
		"explore_lfx_semantic_layer",
		"query_lfx_lens",
		"country/region",
		// Scope dimensions are copied exactly from the current live layer. A
		// wrong dimension returns a plausible wrong population. The conformed
		// project entity replaced the spine as the primary foundation scope;
		// the spine, per-domain long-form guidance and slug examples moved to
		// help('query') and help('doctrine'), asserted in their own tests.
		"resolve slugs via search_projects",
		"project__foundation_slug",
		"NEVER scope a foundation with project_slug",
		"asset_id__project_slug",
		"registration_id__project_slug",
		"event_id__project_name",
		"maintainer_key__cm_project_grandparents_slug",
		"is_lf_project=true",
		"0 rows = misspelled literal",
		// The five eval-verified failure patterns (62 wrong answers in a
		// 207-question replay traced overwhelmingly to these): bots on raw
		// activity metrics, full-legal-name lookups, share denominators,
		// unstated windows, and undiscovered COCOMO value metrics.
		"Bot exclusion is the Insights default",
		"bot_activities",
		"Share of work = activity volumes",
		"DAILY snapshots",
		"Critical <20",
		"International Business Machines Corporation",
		"org-attributed base",
		"default trailing 12 months",
		"total_software_value",
		"COCOMO",
		"asset_id__end_date",
		"future-dated",
		// Struggling routes through the doctrine before the lens fallback.
		"help('doctrine') BEFORE query_lfx_lens",
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
		"project_slug: optional",
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
		{"bot_activities", "the explicit bot view; bot exclusion became the metric default in lf-dbt"},
		{"International Business Machines", "short-name value searches silently miss legal-name accounts"},
		{"help('doctrine')", "the overflow recipes are useless if nothing routes the model to them"},
	} {
		if !strings.Contains(surviving, tc.token) {
			t.Errorf("%q reaches the model only via an optional parameter, where it gets summarised away (%s). Move it into the tool description or onto a required parameter.",
				tc.token, tc.why)
		}
	}
}

// TestAllLensToolDescriptionsFitBudget guards every description that ships in
// tools/list, not just the semantic layer's. query_lfx_lens has far less
// headroom and is the likeliest to drift past the cut unnoticed.
func TestAllLensToolDescriptionsFitBudget(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"explore_lfx_semantic_layer", RegisterExploreSemanticLayer},
		{"query_lfx_semantic_layer", RegisterQuerySemanticLayer},
		{"query_lfx_lens", RegisterQueryLFXLens},
	} {
		tool := listRegisteredTool(t, tc.name, tc.register)
		if got := len(tool.Description); got > schemaDescriptionBudget {
			t.Errorf("%s description is %d bytes; everything past %d is invisible to the model",
				tc.name, got, schemaDescriptionBudget)
		}
	}
}

// ---------------------------------------------------------------------------
// Registration / schema
// ---------------------------------------------------------------------------

func listExploreTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return listRegisteredTool(t, "explore_lfx_semantic_layer", RegisterExploreSemanticLayer)
}

func listQueryTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return listRegisteredTool(t, "query_lfx_semantic_layer", RegisterQuerySemanticLayer)
}

// listRegisteredTool returns the named tool, failing the test if it is absent.
func listRegisteredTool(t *testing.T, name string, register func(*mcp.Server)) *mcp.Tool {
	t.Helper()
	tool := findRegisteredTool(t, name, register)
	if tool == nil {
		t.Fatalf("%s not found in tool list", name)
	}
	return tool
}

// findRegisteredTool returns the named tool, or nil when it is not registered.
func findRegisteredTool(t *testing.T, name string, register func(*mcp.Server)) *mcp.Tool {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test-server",
		Version: "0.0.1",
	}, nil)
	register(server)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect failed: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	res, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name == name {
			return tool
		}
	}
	return nil
}

func schemaRequired(t *testing.T, tool *mcp.Tool) []string {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("failed to marshal input schema: %v", err)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("failed to parse input schema: %v", err)
	}
	return schema.Required
}

// schemaPropertyDescription returns the description a client sees for one
// input-schema property. These travel with tools/list alongside the tool
// description, so guidance in them must not contradict it.
func schemaPropertyDescription(t *testing.T, tool *mcp.Tool, property string) string {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("failed to marshal input schema: %v", err)
	}
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("failed to parse input schema: %v", err)
	}
	prop, ok := schema.Properties[property]
	if !ok {
		t.Fatalf("input schema has no %q property", property)
	}
	return prop.Description
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
	// compaction. Everything else stays optional. Project scope is expressed
	// only in where, so project_slug must not exist in the schema.
	queryRequired := schemaRequired(t, query)
	if !contains(queryRequired, "metrics") {
		t.Errorf("query required = %v; metrics must be required so its guidance survives compaction", queryRequired)
	}
	for _, optional := range []string{"where", "group_by", "order_by", "limit"} {
		if contains(queryRequired, optional) {
			t.Errorf("query required = %v; %s must stay optional", queryRequired, optional)
		}
	}
	querySchema, err := json.Marshal(query.InputSchema)
	if err != nil {
		t.Fatalf("failed to marshal query schema: %v", err)
	}
	if strings.Contains(string(querySchema), `"project_slug"`) {
		t.Errorf("query schema must not expose removed project_slug: %s", querySchema)
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
}

// TestQueryToolRejectsMissingMetricsWithAPointer keeps the recovery path alive
// for a caller on a cached schema that still sends action=query here.
func TestQueryToolRejectsMissingMetricsWithAPointer(t *testing.T) {
	setupLensTest(t)

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
	setupLensTest(t)

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
	setupLensTest(t)

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
	// The full doctrine and worked examples are the reason help exists; they
	// must survive the move off the byte-limited description.
	text := resultText(t, res)
	for _, want := range []string{
		"metric_time__year",
		"There is no separate project parameter",
		"project__foundation_slug",
		"conformed lens",
		"spine_hierarchy_level = 2",
		"activity_project_id__project_spine_slug",
		"asset_id__project_slug",
		"registration_id__project_slug",
		"event_id__project_name",
		"maintainer_key__cm_project_grandparents_slug",
		"health_metric_key__foundation_slug",
		"CNCF contributors, last 12 months",
		"Kubernetes code volume",
		"Compare three foundations",
		"CNCF membership count",
		"Foundation to its projects (walk-down)",
		"counts only, never sums",
		"__segment_slug",
		"PCC-style foundation rollups",
		"risc-v-international / riscv",
		"cff / cloud-foundry",
		"opensearch-foundation / opensearch-project",
		// Hierarchy walks became expressible with the spine dimensions; the
		// old "not expressible today" disclaimer must be gone.
		`"Direct children of X"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("help query text missing %q", want)
		}
	}
}

// TestQueryLFXLensScopeIsContextNotBoundary pins the staff-only cross-project
// contract. project_slug remains required as a default context for the Lens
// workflow, but it must not be described as an authorization or scope boundary.
func TestQueryLFXLensScopeIsContextNotBoundary(t *testing.T) {
	tool := listRegisteredTool(t, "query_lfx_lens", RegisterQueryLFXLens)

	for _, want := range []string{
		"project_slug is required default context, NOT a scope boundary",
		"Find it via search_projects",
		"For multiple foundations",
		"name the others in input",
		"name the others in input",
		"LF-wide",
		"project_slug='tlf'",
	} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("query_lfx_lens description missing scope guidance %q", want)
		}
	}

	required := schemaRequired(t, tool)
	for _, want := range []string{"project_slug", "input"} {
		if !contains(required, want) {
			t.Errorf("query_lfx_lens required = %v; expected %s to remain compulsory", required, want)
		}
	}

	slug := schemaPropertyDescription(t, tool, "project_slug")
	for _, want := range []string{
		"Required default context slug",
		"not a scope boundary",
		"name the others in input",
		"'tlf' for LF-wide questions",
	} {
		if !strings.Contains(slug, want) {
			t.Errorf("project_slug schema description missing %q: %q", want, slug)
		}
	}
}

// TestQueryLFXLensDoesNotClaimMemberships guards the other half of the routing
// contract. query_lfx_lens used to open with "Always use this tool for:
// Membership questions", carved out only for country/region. Memberships now
// belong to the semantic layer in full — 18 metrics covering revenue, counts,
// churn, discounts and invoices, sliceable and trendable like any other domain
// — so a leftover claim here produces two tools asserting ownership of the same
// question. Both the description and the input schema ship with tools/list.
func TestQueryLFXLensDoesNotClaimMemberships(t *testing.T) {
	tool := listRegisteredTool(t, "query_lfx_lens", RegisterQueryLFXLens)

	for _, unwanted := range []string{
		"Always use this tool for:\n- Membership questions",
		"EXCEPT country/region",
	} {
		if strings.Contains(tool.Description, unwanted) {
			t.Errorf("query_lfx_lens description still claims memberships: %q", unwanted)
		}
	}
	if !strings.Contains(tool.Description, "Everything else - contributors, activities, memberships") ||
		!strings.Contains(tool.Description, "belongs to explore_lfx_semantic_layer") {
		t.Error("query_lfx_lens description should hand memberships to the semantic layer explicitly")
	}

	input := schemaPropertyDescription(t, tool, "input")
	if strings.Contains(input, "Always use for memberships") {
		t.Errorf("query_lfx_lens input schema still claims memberships: %q", input)
	}
	if !strings.Contains(input, "membership, event, education and health questions belong to the semantic layer") {
		t.Errorf("query_lfx_lens input schema should redirect memberships: %q", input)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// get_dimension_values
//
// The action exists because a filter naming a real dimension but an unknown
// literal is not an error: the query succeeds and returns zero rows. Against a
// live client that read as "no such data" and cost five wrong-but-successful
// queries — 'APAC' for a region that is stored as 'Asia Pacific', 'Vietnam' for
// a country stored as 'Viet Nam'.
// ---------------------------------------------------------------------------

func TestGetDimensionValuesForwardsToTheValuesEndpoint(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action:    "get_dimension_values",
		Dimension: "  country__lf_region  ",
		Metrics:   " total_contributors , current_membership_revenue ",
		Search:    "asia",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if captured.Path != "/lfx-lens/semantic-layer/dimension-values" {
		t.Errorf("unexpected request path: %s", captured.Path)
	}
	// Whitespace around a copied qualified_name must not reach lens, which
	// rejects anything outside [A-Za-z0-9_] rather than trimming it.
	if got := captured.Query.Get("dimension"); got != "country__lf_region" {
		t.Errorf("dimension = %q; want it trimmed to country__lf_region", got)
	}
	if got := captured.Query.Get("metrics"); got != "total_contributors,current_membership_revenue" {
		t.Errorf("metrics = %q; want the CSV normalised", got)
	}
	if got := captured.Query.Get("search"); got != "asia" {
		t.Errorf("search = %q; want asia", got)
	}
}

func TestGetDimensionValuesOmitsAnEmptySearch(t *testing.T) {
	captured := setupLensTest(t)

	_, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action:    "get_dimension_values",
		Dimension: "country__lf_region",
		Metrics:   "total_contributors",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// An empty search must be absent, not sent as "": lens turns a present
	// search into an ILIKE '%%' filter and would report zero matches.
	if captured.Query.Has("search") {
		t.Errorf("search should be omitted when empty, got %q", captured.Query.Get("search"))
	}
}

func TestGetDimensionValuesRejectsMissingArgumentsWithAPointer(t *testing.T) {
	for _, tc := range []struct {
		name string
		args ExploreSemanticLayerArgs
		want string
	}{
		{
			name: "no dimension",
			args: ExploreSemanticLayerArgs{Action: "get_dimension_values", Metrics: "total_contributors"},
			want: "country__lf_region",
		},
		{
			name: "blank dimension",
			args: ExploreSemanticLayerArgs{Action: "get_dimension_values", Dimension: "   ", Metrics: "total_contributors"},
			want: "country__lf_region",
		},
		{
			name: "no metrics",
			args: ExploreSemanticLayerArgs{Action: "get_dimension_values", Dimension: "country__lf_region"},
			want: "metrics is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupLensTest(t)

			res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !res.IsError {
				t.Fatal("expected an error result")
			}
			if text := resultText(t, res); !strings.Contains(text, tc.want) {
				t.Errorf("error should show the way forward (%q): %q", tc.want, text)
			}
		})
	}
}

// TestUnknownActionListsTheRealActions guards the recovery message against
// drift: it is what a model reads after guessing an action name, so an action
// missing here is one it will not retry with.
func TestUnknownActionListsTheRealActions(t *testing.T) {
	setupLensTest(t)

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
	setupLensTest(t)

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

// TestDoctrineHelp pins the overflow doctrine: every recipe that the
// 2048-byte descriptions cannot hold, verified against the live layer during
// the August 2026 eval. If one of these tokens disappears, a failure pattern
// that produced wrong answers in the 207-question replay loses its recipe.
func TestDoctrineHelp(t *testing.T) {
	setupLensTest(t)

	res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
		Action: "help",
		Target: "doctrine",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	text := resultText(t, res)
	for _, want := range []string{
		// windows and membership parity
		"trailing 12 months",
		"asset_id__end_date",
		"current_membership_count",
		"future-dated",
		// scoping and hierarchy
		"project__foundation_slug",
		"spine_hierarchy_level = 2",
		"risc-v-international/riscv",
		// bots
		"member_is_bot",
		"bot_activities",
		"3,619,940",
		// org shares and headcounts
		"org-ATTRIBUTED",
		"Individual - No Account",
		"2-4x",
		// name discovery
		"International Business Machines Corporation",
		"Red Hat LLC",
		// tiers, health, value
		"Premier Membership",
		"Critical <20, Unsteady 20-39",
		"total_software_value",
		"COCOMO",
		// populations, maintainers, regions
		"total_contributors_with_collaboration",
		"2000-01-01 sentinel",
		"maintainer_key__is_lf_project",
		"organization_lf_region",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("doctrine help missing %q", want)
		}
	}
}

// TestHelpNeverFails pins the help contract: help always returns guidance.
// An IsError help result pushes the model toward guessing or a premature
// query_lfx_lens fallback - the exact behaviors the doctrine exists to stop.
func TestHelpNeverFails(t *testing.T) {
	setupLensTest(t)

	for _, tc := range []struct {
		target string
		want   string
	}{
		{"", "how to use it"},
		{"overview", "how to use it"},
		{"doctrine", "WORKED RECIPES"},
		{"recipes", "WORKED RECIPES"},              // alias
		{"bots", "member_is_bot"},                  // topic keyword routes to doctrine
		{"windows", "trailing 12 months"},          // topic keyword routes to doctrine
		{"metrics", "list_metrics"},                // action-ish keyword
		{"total_bananas_metric", "WORKED RECIPES"}, // unknown: overview+doctrine, not an error
	} {
		res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
			Action: "help",
			Target: tc.target,
		})
		if err != nil {
			t.Fatalf("target %q: unexpected error: %v", tc.target, err)
		}
		if res.IsError {
			t.Errorf("target %q: help returned an error result; it must always return guidance", tc.target)
			continue
		}
		if text := resultText(t, res); !strings.Contains(text, tc.want) {
			t.Errorf("target %q: help text missing %q", tc.target, text[:min(120, len(text))])
		}
	}
}
