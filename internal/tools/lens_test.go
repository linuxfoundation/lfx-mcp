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

func TestSemanticLayerDescription(t *testing.T) {
	for _, want := range []string{
		"project_slug is optional",
		"may be omitted for global or cross-foundation questions",
		"For membership metrics, a Linux Foundation ('tlf') filter only captures direct LF memberships",
		"Activity metrics are fanned out to foundations",
	} {
		if !strings.Contains(semanticLayerDescription, want) {
			t.Errorf("description missing %q", want)
		}
	}
	if strings.Contains(semanticLayerDescription, "MUST include a project scope filter") {
		t.Error("description must not contain the mandatory-scope wording")
	}
}

// ---------------------------------------------------------------------------
// Registration / schema
// ---------------------------------------------------------------------------

func listSemanticLayerTool(t *testing.T) *mcp.Tool {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test-server",
		Version: "0.0.1",
	}, nil)
	RegisterSemanticLayer(server)

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
	if !strings.Contains(tool.Description, "project_slug is optional") {
		t.Error("description missing optional project_slug wording")
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
