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

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
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
}

func TestSemanticLayer_ScopedQuerySendsProjectSlugAndWhere(t *testing.T) {
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

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
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

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
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

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
		Action:  "query",
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

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
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

// TestSemanticLayerDescription_FitsSchemaBudget guards that limit for the tool
// description itself.
func TestSemanticLayerDescription_FitsSchemaBudget(t *testing.T) {
	if got := len(semanticLayerDescription); got > schemaDescriptionBudget {
		t.Errorf("description is %d chars; everything past %d is invisible to the model — move detail onto a parameter or into help",
			got, schemaDescriptionBudget)
	}
}

func TestSemanticLayerDescription(t *testing.T) {
	for _, want := range []string{
		// The domains this tool owns are named explicitly. Without them the
		// routing is one-sided — query_lfx_lens lists concrete triggers while
		// this tool describes itself abstractly, so every specific question
		// looks like a better match for the other tool.
		"contributions —",
		"memberships —",
		"events —",
		"education —",
		"maintainers —",
		"project health —",
		// Regional questions route here for every topic, memberships included.
		"any of the above sliced by country or region — always here, never query_lfx_lens",
		"memberships not sliced by country or region",
		// Capabilities that the earlier "pre-aggregated metrics" framing hid.
		"ranked list",
		"List several metrics in one query",
		"metric_time__year",
		// Regional dimensions: person's country vs organization HQ.
		"country__lf_region",
		"activity_project_id__organization_lf_region",
		// help is a fallback, not a prerequisite.
		"Start with action=list_metrics",
	} {
		if !strings.Contains(semanticLayerDescription, want) {
			t.Errorf("description missing %q", want)
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
		if strings.Contains(semanticLayerDescription, unwanted) {
			t.Errorf("description must not contain %q", unwanted)
		}
	}
}

// TestSemanticLayerArgs_FieldsFitSchemaBudget holds the other half of the
// budget contract. Syntax was deliberately moved out of the tool description
// and onto the parameters it governs; each property description is a separate
// field, so each must independently stay under the limit.
func TestSemanticLayerArgs_FieldsFitSchemaBudget(t *testing.T) {
	tool := listSemanticLayerTool(t)
	for _, property := range []string{
		"project_slug", "action", "target", "metrics",
		"search", "group_by", "where", "order_by", "limit",
	} {
		if got := len(schemaPropertyDescription(t, tool, property)); got > schemaDescriptionBudget {
			t.Errorf("%s description is %d chars; everything past %d is invisible to the model",
				property, got, schemaDescriptionBudget)
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

func listSemanticLayerTool(t *testing.T) *mcp.Tool {
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
	tool := listSemanticLayerTool(t)

	required := schemaRequired(t, tool)
	if contains(required, "project_slug") {
		t.Errorf("schema required = %v; project_slug must be optional", required)
	}
	if contains(required, "where") {
		t.Errorf("schema required = %v; where must be optional", required)
	}
	if !contains(required, "action") {
		t.Errorf("schema required = %v; expected to contain action", required)
	}
	// The optional-scope rule moved onto the parameter it governs, where the
	// model reads it while filling the field.
	slug := schemaPropertyDescription(t, tool, "project_slug")
	if !strings.Contains(slug, "Omit it for global or cross-foundation questions") {
		t.Errorf("project_slug schema description missing the optional-scope rule: %q", slug)
	}

	// The action property's own guidance ships with tools/list, so it must name
	// the same four actions the dispatcher accepts — a stale list here sends
	// the model to an action that errors.
	action := schemaPropertyDescription(t, tool, "action")
	for _, want := range []string{"list_metrics", "get_dimensions", "query", "help"} {
		if !strings.Contains(action, want) {
			t.Errorf("action schema description missing %q: %q", want, action)
		}
	}
	if strings.Contains(action, "describe") {
		t.Errorf("action schema description still advertises the renamed describe action: %q", action)
	}

	// Syntax the description defers to the parameters must actually be there.
	where := schemaPropertyDescription(t, tool, "where")
	for _, want := range []string{"Dimension(", "TimeDimension(", "yyyy-mm-dd"} {
		if !strings.Contains(where, want) {
			t.Errorf("where schema description missing %q: %q", want, where)
		}
	}
	groupBy := schemaPropertyDescription(t, tool, "group_by")
	for _, want := range []string{"metric_time__year", "join keys, not group-by values"} {
		if !strings.Contains(groupBy, want) {
			t.Errorf("group_by schema description missing %q: %q", want, groupBy)
		}
	}
}

// TestHelpActionAndDescribeAlias checks the renamed action works and that the
// old name still dispatches, so a client working from a cached schema does not
// hit an Unknown action error.
func TestHelpActionAndDescribeAlias(t *testing.T) {
	setupLensTest(t)

	for _, action := range []string{"help", "describe"} {
		res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
			Action: action,
		})
		if err != nil {
			t.Fatalf("action %q: unexpected error: %v", action, err)
		}
		if text := resultText(t, res); !strings.Contains(text, "how to use it") {
			t.Errorf("action %q did not return the help overview: %q", action, text)
		}
	}

	res, _, err := handleSemanticLayer(context.Background(), &mcp.CallToolRequest{}, SemanticLayerLFXLensArgs{
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
