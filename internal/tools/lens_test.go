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

func TestSemanticLayer_LimitPassesThrough(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleQuerySemanticLayer(context.Background(), &mcp.CallToolRequest{}, QuerySemanticLayerArgs{
		Metrics: "active_maintainers",
		Limit:   501,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Errorf("a limit above 500 is no longer rejected here; got: %q", resultText(t, res))
	}
	if captured.Path != "/lfx-lens/semantic-layer/query" {
		t.Errorf("the request did not reach the lens query route: %s", captured.Path)
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

// TestExploreSemanticLayerDescription checks the slimmed discovery tool
// still carries the routing contract: what the layer covers, the
// guidance-first instruction, the standard metric recipe preference, the discovery
// actions, and the redirects. Everything else moved to the guidance tool
// (pinned in TestSemanticLayerGuidanceContent).
func TestExploreSemanticLayerDescription(t *testing.T) {
	for _, want := range []string{
		// The covered domains, named so routing works from this tool.
		"contributor, contribution, membership, revenue, event, registration, speaker, sponsorship, enrollment, certification, maintainer, health",
		"country or region",
		// Guidance-first, once per session, shared with the query tool.
		"read_lfx_semantic_layer_guidance",
		"read it BEFORE using this tool",
		"one read also covers query_lfx_semantic_layer",
		// A matching standard metric recipe outranks the explore+query flow.
		"query_lfx_standard_metrics",
		// The three discovery actions with their arguments.
		"list_metrics(search)",
		"get_dimensions(metrics, search)",
		"get_dimension_values(dimension, metrics, search)",
		// The silent-zero-rows trap and its canonical example.
		"zero rows, not an error",
		"'Asia Pacific' not 'APAC'",
		// Naming discipline and the resolvers.
		"entity__field",
		"copy qualified_names, never assemble",
		"search_projects",
		"search_b2b_orgs",
		// Routing to the neighbours.
		"query_lfx_semantic_layer",
		"past-date membership counts, cross-domain joins",
		"social listening (mentions, sentiment, reach)",
		"Board/committee/ambassador rosters: committee tools",
		"Start here unless exact names are known",
	} {
		if !strings.Contains(exploreSemanticLayerDescription, want) {
			t.Errorf("explore description missing %q", want)
		}
	}
}

// TestQuerySemanticLayerDescription checks the slimmed query tool keeps
// what a model cannot guess and cannot recover from silently: the MetricFlow
// syntax, the foundation-scope trap, name resolution, and the recovery path.
// The full doctrine moved to the guidance tool.
func TestQuerySemanticLayerDescription(t *testing.T) {
	for _, want := range []string{
		// Discovery-first and guidance-first.
		"ALWAYS explore_lfx_semantic_layer first",
		"never guess",
		"read_lfx_semantic_layer_guidance",
		"read it BEFORE querying",
		"query_lfx_standard_metrics",
		// The unguessable syntax.
		"metrics (required)",
		"Dimension(",
		"TimeDimension(",
		"yyyy-mm-dd",
		"limit optional",
		"metric_time__year",
		// The deadliest scope trap, stated even before the guidance is read.
		"project__foundation_slug",
		"NEVER scope a foundation with project_slug",
		"search_projects",
		// Name resolution.
		"FULL LEGAL names",
		"search_b2b_orgs",
		// The silent-failure recovery chain.
		"0 rows = misspelled literal",
		"get_dimension_values",
		"query_lfx_lens",
		// The answer contract.
		"State definition and window",
		"country/region",
	} {
		if !strings.Contains(querySemanticLayerDescription, want) {
			t.Errorf("query description missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"MUST include a project scope filter",
		"project_slug: optional",
		"pre-aggregated",
		"returns numbers, not records",
	} {
		if strings.Contains(querySemanticLayerDescription, unwanted) {
			t.Errorf("query description must not contain %q", unwanted)
		}
	}
}

// TestSlimDescriptionsKeepHeadroom encodes the post-guidance contract: the
// data tools' descriptions carry routing and unguessable syntax only, so
// they must never creep back toward the 2048-byte cliff. Detail belongs in
// the guidance tools, whose results have no budget.
//
// query_lfx_standard_metrics carries one thing the others do not — the metric
// inventory, which a caller cannot guess and which must reach the model from
// the tool itself — so it keeps its own ceiling instead of the shared 1600.
func TestSlimDescriptionsKeepHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name    string
		desc    string
		ceiling int
	}{
		{"explore_lfx_semantic_layer", exploreSemanticLayerDescription, 1600},
		{"query_lfx_semantic_layer", querySemanticLayerDescription, 1600},
		{"query_lfx_standard_metrics", standardMetricsDescription, standardMetricsDescriptionCeiling},
	} {
		if got := len(tc.desc); got > tc.ceiling {
			t.Errorf("%s description is %d bytes; keep it under %d — move detail into the guidance tools", tc.name, got, tc.ceiling)
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
	// surviving is what a compacting client still shows for one tool: its
	// description plus the descriptions of its REQUIRED parameters.
	surviving := func(tool *mcp.Tool) string {
		text := tool.Description
		for _, name := range schemaRequired(t, tool) {
			text += "\n" + schemaPropertyDescription(t, tool, name)
		}
		return text
	}

	type token struct {
		token string
		why   string
	}

	// Asserted PER TOOL, not over the concatenation: a caller looking at
	// query_lfx_standard_metrics never sees explore_lfx_semantic_layer's description, so
	// a token that only survives on a sibling has not survived for this one.
	for _, tc := range []struct {
		// tools is the set that must carry the tokens between them: the
		// explore/query pair is one routing surface (a caller reaching for
		// either sees both), query_lfx_standard_metrics is its own.
		name   string
		tools  []*mcp.Tool
		tokens []token
	}{
		{
			name:  "explore_lfx_semantic_layer + query_lfx_semantic_layer",
			tools: []*mcp.Tool{listExploreTool(t), listQueryTool(t)},
			tokens: []token{
				{"Dimension(", "categorical filter syntax is unguessable"},
				{"TimeDimension(", "time filter syntax is unguessable"},
				{"yyyy-mm-dd", "date format silently returns wrong rows if guessed"},
				{"limit optional", "an omitted limit returns the complete set"},
				{"metric_time__year", "the only way to build a trend"},
				{"entity__field", "dimension names cannot be assembled by hand"},
				{"outer-joined", "explains NULLs in cross-domain results"},
				{"raw IDs", "grouping by an entity silently returns unusable output"},
				{"get_dimension_values", "the only recovery from a wrong filter literal"},
				{"zero rows", "a wrong literal is silent, so the model must be told to check first"},
				{"FULL LEGAL names", "short-name value searches silently miss legal-name accounts"},
				{"search_b2b_orgs", "the resolver for org legal names; its empty results are access-filtered, not proof of absence"},
				{"read_lfx_semantic_layer_guidance", "the guidance recipes are useless if nothing routes the model to them"},
			},
		},
		{
			name:  "query_lfx_standard_metrics",
			tools: []*mcp.Tool{listStandardMetricsTool(t)},
			tokens: []token{
				{"read_lfx_standard_metrics_guidance", "the standard metric inventory is only reachable if the tool routes the model to it"},
				{"BEFORE the first call", "the guidance defines every grouping, switch and default; the description only routes to it"},
				// The family names tell a caller whether this tool covers
				// its question at all, so the whole inventory is pinned:
				// on the description AND on the required metric parameter.
				{"STANDARD METRICS memberships, new_members, membership_churn, contributors, contributions, contributing_organizations, participants, maintainers, maintainer_contributions, project_health, software_value, event_registrations, event_sponsorships, speakers, training_enrollments, certifications, social_mentions, social_reach.", "the family inventory reaches the model only here"},
				{"search_projects", "project takes the stored slug; an everyday name silently misses"},
				{"search_b2b_orgs", "org takes the stored legal name; a short name silently misses"},
				{"never pass a name they have not returned", "a guessed literal is a confident wrong answer, not an error"},
				{"start_date, end_date and period", "the three date parameters replace since/until/as_of; a caller on the old contract must learn the new words here"},
				{"WINDOW", "the two kinds decide what end_date means; the model must know there are two"},
				{"AT-DATE", "the two kinds decide what end_date means; the model must know there are two"},
				{"state on end_date", "an at-date family reports a state, not a count between dates"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var text string
			for _, tool := range tc.tools {
				text += "\n" + surviving(tool)
			}
			for _, tk := range tc.tokens {
				if !strings.Contains(text, tk.token) {
					t.Errorf("%q reaches the model only via an optional parameter of %s, where it gets summarised away (%s). Move it into the tool description or onto a required parameter.",
						tk.token, tc.name, tk.why)
				}
			}
		})
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

// schemaProperties returns the names of every input-schema property, in no
// particular order.
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
	for _, want := range []string{"list_metrics", "get_dimensions", "get_dimension_values", "read_lfx_semantic_layer_guidance"} {
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

// TestHelpAndDescribeReturnGuidance keeps the retired help action safe for
// callers on a cached schema: both old action words return the full semantic
// layer guidance rather than an error, whatever target they pass. A failed
// help call would push the model toward guessing or a premature
// query_lfx_lens fallback.
func TestHelpAndDescribeReturnGuidance(t *testing.T) {
	setupLensTest(t)

	for _, tc := range []struct{ action, target string }{
		{"help", ""},
		{"help", "doctrine"},
		{"help", "query"},
		{"describe", ""},
		{"describe", "total_bananas_metric"},
	} {
		res, _, err := handleExploreSemanticLayer(context.Background(), &mcp.CallToolRequest{}, ExploreSemanticLayerArgs{
			Action: tc.action,
			Target: tc.target,
		})
		if err != nil {
			t.Fatalf("action %q target %q: unexpected error: %v", tc.action, tc.target, err)
		}
		if res.IsError {
			t.Errorf("action %q target %q: help must never fail", tc.action, tc.target)
			continue
		}
		if text := resultText(t, res); !strings.Contains(text, "Worked recipes") {
			t.Errorf("action %q target %q: expected the guidance document, got %q", tc.action, tc.target, text[:min(120, len(text))])
		}
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

// TestQueryLFXLens_StdioNilExtraUsesAnonymous pins the stdio crash fix: in
// stdio mode req.Extra is nil (HTTP mode always populates it), and the handler
// used to dereference req.Extra.TokenInfo and SIGSEGV the whole server. The
// request must complete and run the workflow as the anonymous user.
func TestQueryLFXLens_StdioNilExtraUsesAnonymous(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleQueryLFXLens(context.Background(), &mcp.CallToolRequest{}, QueryLFXLensArgs{
		ProjectSlug: "cncf",
		Input:       "how many contributors last year?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if !strings.Contains(string(captured.Body), AnonymousUserID) {
		t.Errorf("expected workflow request to carry the anonymous user ID %q, body: %s", AnonymousUserID, captured.Body)
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
		"LF-wide",
		"project_slug='tlf'",
		// Lens generates its own SQL and picks arbitrary windows when the
		// question leaves them open — the description must carry the default
		// window convention and require concrete dates in the question.
		"default trailing 12 months",
		"state concrete yyyy-mm-dd dates",
		"the SQL picks its own",
		// Governance rosters are neither lens's lane nor the semantic
		// layer's — the redirect must survive here too.
		"Committee/board rosters: the committee tools",
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
	if !strings.Contains(input, "membership, event, education, health and social listening questions belong to the semantic layer") {
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
	for _, want := range []string{"list_metrics", "get_dimensions", "get_dimension_values", "read_lfx_semantic_layer_guidance"} {
		if !strings.Contains(text, want) {
			t.Errorf("unknown-action error missing %q: %q", want, text)
		}
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
