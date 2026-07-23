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
// Non-staff handler behavior
// ---------------------------------------------------------------------------

func TestSemanticLayerNonStaff_QueryRequiresProjectSlug(t *testing.T) {
	setupLensTest(t)

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
		Action:  "query",
		Metrics: "active_maintainers",
		Where:   "{{ Dimension('maintainer_key__project_slug') }} = 'cncf'",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError result")
	}
	if got := resultText(t, res); got != "Error: project_slug is required" {
		t.Errorf("unexpected error text: %q", got)
	}
}

func TestSemanticLayerNonStaff_QueryRequiresWhere(t *testing.T) {
	setupLensTest(t)

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
		ProjectSlug: "cncf",
		Action:      "query",
		Metrics:     "active_maintainers",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError result")
	}
	if got := resultText(t, res); !strings.Contains(got, "where is required for query") {
		t.Errorf("unexpected error text: %q", got)
	}
}

func TestSemanticLayerNonStaff_QuerySendsProjectSlugAndWhere(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
		ProjectSlug: "cncf",
		Action:      "query",
		Metrics:     "active_maintainers",
		Where:       "{{ Dimension('maintainer_key__project_slug') }} = 'cncf'",
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
	if body["project_slug"] != "cncf" {
		t.Errorf("expected project_slug 'cncf' in request body, got: %v", body["project_slug"])
	}
	if _, ok := body["where"]; !ok {
		t.Error("expected where key in request body")
	}
	if v, ok := body["is_staff"].(bool); !ok || v {
		t.Errorf("expected is_staff false in request body, got: %v", body["is_staff"])
	}
}

func TestSemanticLayerNonStaff_ListMetricsRequiresProjectSlug(t *testing.T) {
	setupLensTest(t)

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
		Action: "list_metrics",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError result")
	}
	if got := resultText(t, res); got != "Error: project_slug is required" {
		t.Errorf("unexpected error text: %q", got)
	}
}

func TestSemanticLayerNonStaff_DescribeWithoutProjectSlug(t *testing.T) {
	setupLensTest(t)

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
		Action: "describe",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if got := resultText(t, res); !strings.Contains(got, "Available actions") {
		t.Errorf("unexpected describe text: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Staff handler behavior
// ---------------------------------------------------------------------------

func TestSemanticLayerStaff_GlobalQueryOmitsProjectSlugAndWhere(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleSemanticLayerStaff(context.Background(), &mcp.CallToolRequest{}, SemanticLayerStaffLFXLensArgs{
		Action:  "query",
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
	if v, ok := body["is_staff"].(bool); !ok || !v {
		t.Errorf("expected is_staff true in request body, got: %v", body["is_staff"])
	}
}

func TestSemanticLayerStaff_ScopedQuerySendsProjectSlugAndWhere(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleSemanticLayerStaff(context.Background(), &mcp.CallToolRequest{}, SemanticLayerStaffLFXLensArgs{
		ProjectSlug: "cncf",
		Action:      "query",
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
	if v, ok := body["is_staff"].(bool); !ok || !v {
		t.Errorf("expected is_staff true in request body, got: %v", body["is_staff"])
	}
}

func TestSemanticLayerStaff_ListMetricsWithoutProjectSlug(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleSemanticLayerStaff(context.Background(), &mcp.CallToolRequest{}, SemanticLayerStaffLFXLensArgs{
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

func TestSemanticLayerStaff_GetDimensionsWithoutProjectSlug(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleSemanticLayerStaff(context.Background(), &mcp.CallToolRequest{}, SemanticLayerStaffLFXLensArgs{
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

func TestSemanticLayer_LimitTooLargeBothVariants(t *testing.T) {
	setupLensTest(t)

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
		ProjectSlug: "cncf",
		Action:      "query",
		Metrics:     "active_maintainers",
		Where:       "{{ Dimension('maintainer_key__project_slug') }} = 'cncf'",
		Limit:       501,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || resultText(t, res) != "Error: limit must be 500 or less" {
		t.Errorf("non-staff: expected limit error, got: %q (IsError=%v)", resultText(t, res), res.IsError)
	}

	res, _, err = handleSemanticLayerStaff(context.Background(), &mcp.CallToolRequest{}, SemanticLayerStaffLFXLensArgs{
		Action:  "query",
		Metrics: "active_maintainers",
		Limit:   501,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || resultText(t, res) != "Error: limit must be 500 or less" {
		t.Errorf("staff: expected limit error, got: %q (IsError=%v)", resultText(t, res), res.IsError)
	}
}

func TestSemanticLayer_DescribeQueryVariants(t *testing.T) {
	setupLensTest(t)

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
		Action: "describe",
		Target: "query",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	nonStaffText := resultText(t, res)
	if !strings.Contains(nonStaffText, "project_slug is always required") {
		t.Errorf("non-staff describe query text missing mandatory-scope wording: %q", nonStaffText)
	}

	res, _, err = handleSemanticLayerStaff(context.Background(), &mcp.CallToolRequest{}, SemanticLayerStaffLFXLensArgs{
		Action: "describe",
		Target: "query",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	staffText := resultText(t, res)
	if !strings.Contains(staffText, "project_slug is optional") {
		t.Errorf("staff describe query text missing optional-scope wording: %q", staffText)
	}
	if strings.Contains(staffText, "project_slug is always required") {
		t.Errorf("staff describe query text must not contain mandatory-scope wording: %q", staffText)
	}
}

// ---------------------------------------------------------------------------
// Description assembly
// ---------------------------------------------------------------------------

// TestSemanticLayerDescription_NonStaffVerbatim guards against drift: the
// composed non-staff description must remain byte-for-byte the description the
// tool shipped with before the staff variant was introduced.
func TestSemanticLayerDescription_NonStaffVerbatim(t *testing.T) {
	const want = `LFX Insights Semantic Layer — pre-aggregated metrics for code activities & contributions, maintainer counts, project health scores, projects, events & event registrations, and education & certifications. Returns deterministic results in seconds.

Best for direct, well-scoped questions: totals, counts, averages, breakdowns by a single dimension, and time series (e.g. "total activities for CNCF", "active maintainers by organization", "health score trend by month", "total enrollments by course"). This is also the right tool for contributor/activity questions — it has full contributor data including names, organizations, and activity breakdowns.

Use query_lfx_lens INSTEAD for:
- All membership questions (memberships model works better with ad-hoc SQL)
- Maintainer names, maintainer+contribution (activities data) joins, or maintainer trends
- Open-ended or exploratory analysis (e.g. "which projects need attention?")
- Questions involving subprojects (e.g. "health scores by project")
- Cross-domain joins (maintainers and contributors are separate models)
- Any question where this tool is struggling or returning errors
- Event sponsorships. All other event and event registration data is fine here

Use search_projects first to find the project slug. Then call list_metrics to discover available metrics.

Actions:

- list_metrics: First step. Returns metrics with descriptions. When <=15 match, dimensions are included — often enough to go straight to query.

- get_dimensions: Get group_by/filter fields for specific metrics. Use when list_metrics returned too many results to include dimensions.

- query: Execute a metric query. CRITICAL rules:
  1. The where clause MUST include a project scope filter — the project_slug parameter alone does not filter the data. Check the dimensions list for the correct one (e.g. registration_id__project_slug). Some models don't have project_slug — they use project_name instead. In that case, use the full project name from search_projects (e.g. "Cloud Native Computing Foundation (CNCF)").
  2. Different metrics use different entity prefixes — always check the dimensions list from list_metrics to find the correct qualified_names. Do not guess prefixes.
  3. Set a reasonable limit (10-50) to avoid huge results.
  4. If you have loaded in metrics and dimensions, and you still can't get the data you are looking for in 5 query turns or less, use query_lfx_lens.

- describe: Get detailed syntax reference and examples for any action.

Tips:
- Contributors and code-related data (commits, PRs, insertions, deletions) are in the activities model — search for "activities" in list_metrics.
  IMPORTANT: Questions about contributors and code-related topics that do not involve maintainers should prefer this tool.
- Events metrics use project_name rather than project_slug for filtering.
- Questions about The Linux Foundation (slug is tlf) still need to be scoped with the correct where clause.`

	if got := semanticLayerDescription(false); got != want {
		t.Errorf("non-staff description drifted from the original text:\ngot:\n%s", got)
	}
}

func TestSemanticLayerDescription_Staff(t *testing.T) {
	got := semanticLayerDescription(true)
	for _, want := range []string{
		"project_slug is optional",
		"may be omitted for global or cross-foundation questions",
		"For membership metrics, a Linux Foundation ('tlf') filter only captures direct LF memberships",
		"Activity metrics are fanned out to foundations",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("staff description missing %q", want)
		}
	}
	if strings.Contains(got, "MUST include a project scope filter") {
		t.Error("staff description must not contain the mandatory-scope wording")
	}
}

// ---------------------------------------------------------------------------
// Registration / schema variants
// ---------------------------------------------------------------------------

func listSemanticLayerTool(t *testing.T, cache *mcp.SchemaCache, isStaff bool) *mcp.Tool {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test-server",
		Version: "0.0.1",
	}, &mcp.ServerOptions{SchemaCache: cache})
	RegisterSemanticLayer(server, isStaff)

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
		if tool.Name == "query_lfx_semantic_layer" {
			return tool
		}
	}
	t.Fatal("query_lfx_semantic_layer not found in tool list")
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

func TestRegisterSemanticLayer_Variants(t *testing.T) {
	// Share one schema cache across both variants to prove the per-type
	// caching does not cross-contaminate the two arg struct schemas.
	cache := mcp.NewSchemaCache()

	nonStaff := listSemanticLayerTool(t, cache, false)
	staff := listSemanticLayerTool(t, cache, true)

	// Non-staff: project_slug required, mandatory-scope description preserved.
	required := schemaRequired(t, nonStaff)
	if !contains(required, "project_slug") {
		t.Errorf("non-staff schema required = %v; expected to contain project_slug", required)
	}
	if !strings.Contains(nonStaff.Description, "MUST include a project scope filter") {
		t.Error("non-staff description missing mandatory-scope wording")
	}

	// Staff: project_slug and where both optional, capability-style description.
	required = schemaRequired(t, staff)
	if contains(required, "project_slug") {
		t.Errorf("staff schema required = %v; project_slug must be optional", required)
	}
	if contains(required, "where") {
		t.Errorf("staff schema required = %v; where must be optional", required)
	}
	if !strings.Contains(staff.Description, "project_slug is optional") {
		t.Error("staff description missing optional project_slug wording")
	}
	if !strings.Contains(staff.Description, "For membership metrics, a Linux Foundation ('tlf') filter only captures direct LF memberships") {
		t.Error("staff description missing the tlf membership caveat")
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
