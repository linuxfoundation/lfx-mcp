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

func TestSemanticLayer_GlobalQueryOmitsProjectSlugAndWhere(t *testing.T) {
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

func TestSemanticLayer_ScopedQuerySendsProjectSlugAndWhere(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
		ProjectSlug: "cncf",
		Metrics:     "active_maintainers",
		Where:       "{{ Dimension('maintainer_key__project_slug') }} = 'cncf'",
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
	if body["project_slug"] != "cncf" {
		t.Errorf("expected project_slug 'cncf' in request body, got: %v", body["project_slug"])
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
	if !strings.Contains(text, "project_slug is optional") {
		t.Errorf("describe query text missing optional-scope wording: %q", text)
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
		"contributions —",
		"memberships —",
		"events —",
		"education —",
		"maintainers —",
		"project health —",
		// Regional questions route here for every topic, memberships included.
		"any of the above sliced by country or region — always here, never query_lfx_lens",
		"memberships not sliced by country or region",
		// Dimension naming, and the regional person-vs-organization split.
		"entity__field",
		"country__lf_region",
		"activity_project_id__organization_lf_region",
		// Discovery must hand off to the query tool by name.
		"query_lfx_semantic_layer",
	} {
		if !strings.Contains(exploreSemanticLayerDescription, want) {
			t.Errorf("explore description missing %q", want)
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
		"outer-joined",
		"ranked list",
		"project_slug",
		// Both neighbours are named so routing works from this tool too.
		"explore_lfx_semantic_layer",
		"query_lfx_lens",
	} {
		if !strings.Contains(querySemanticLayerDescription, want) {
			t.Errorf("query description missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"MUST include a project scope filter",
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
		{listExploreTool(t), []string{"action", "search", "metrics", "target"}},
		{listQueryTool(t), []string{"metrics", "group_by", "where", "order_by", "limit", "project_slug"}},
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
	} {
		if !strings.Contains(surviving, tc.token) {
			t.Errorf("%q reaches the model only via an optional parameter, where it gets summarised away (%s). Move it into the tool description or onto a required parameter.",
				tc.token, tc.why)
		}
	}
}

// TestBothLensToolDescriptionsFitBudget guards every description that ships in
// tools/list, not just the semantic layer's. query_lfx_lens has far less
// headroom and is the likelier of the two to drift past the cut unnoticed.
func TestBothLensToolDescriptionsFitBudget(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"explore_lfx_semantic_layer", RegisterSemanticLayer},
		{"query_lfx_semantic_layer", RegisterSemanticLayer},
		{"query_lfx_lens", RegisterQueryLFXLens},
	} {
		tool := listRegisteredTool(t, tc.name, tc.register)
		if got := len(tool.Description); got > schemaDescriptionBudget {
			t.Errorf("%s description is %d chars; everything past %d is invisible to the model",
				tc.name, got, schemaDescriptionBudget)
		}
	}
}

// ---------------------------------------------------------------------------
// Registration / schema
// ---------------------------------------------------------------------------

func listExploreTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return listRegisteredTool(t, "explore_lfx_semantic_layer", RegisterSemanticLayer)
}

func listQueryTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return listRegisteredTool(t, "query_lfx_semantic_layer", RegisterSemanticLayer)
}

func listRegisteredTool(t *testing.T, name string, register func(*mcp.Server)) *mcp.Tool {
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
	t.Fatalf("%s not found in tool list", name)
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
	for _, want := range []string{"list_metrics", "get_dimensions", "help"} {
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
	for _, optional := range []string{"project_slug", "where", "group_by", "order_by", "limit"} {
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
	slug := schemaPropertyDescription(t, query, "project_slug")
	if !strings.Contains(slug, "Omit it for global or cross-foundation questions") {
		t.Errorf("project_slug schema description missing the optional-scope rule: %q", slug)
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
// old name still dispatches, so a client working from a cached schema does not
// hit an Unknown action error.
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
	// The worked examples are the reason help exists; they must survive the
	// move off the description.
	if text := resultText(t, res); !strings.Contains(text, "metric_time__year") {
		t.Errorf("help query text missing the trend example: %q", text)
	}
}

// TestQueryLFXLensDescription_RegionalException guards the other half of the
// routing contract: query_lfx_lens claims memberships, and both its
// description and its input schema ship with tools/list. If they keep saying
// "always use for memberships" unconditionally, clients get instructions that
// contradict the semantic layer's regional carve-out.
func TestQueryLFXLensDescription_RegionalException(t *testing.T) {
	tool := listRegisteredTool(t, "query_lfx_lens", RegisterQueryLFXLens)

	if !strings.Contains(tool.Description, "EXCEPT country/region") {
		t.Errorf("query_lfx_lens description missing the regional exception: %q", tool.Description)
	}

	input := schemaPropertyDescription(t, tool, "input")
	if !strings.Contains(input, "memberships (except country/region breakdowns)") {
		t.Errorf("query_lfx_lens input schema missing the regional exception: %q", input)
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
