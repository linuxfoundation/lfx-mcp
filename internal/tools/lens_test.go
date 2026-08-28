// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
// Shared description-budget helpers
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
			t.Errorf("%s description is %d chars; everything past %d is invisible to the model",
				tc.name, got, schemaDescriptionBudget)
		}
	}
}

// TestDataToolDescriptionsShareLastTwelveMonthsConvention guards RC-2. Lens
// and the two semantic-layer tools must give the model one boundary convention,
// and must require the answer to expose the concrete range so users can compare
// results across tools.
func TestDataToolDescriptionsShareLastTwelveMonthsConvention(t *testing.T) {
	const convention = `"last 12 months" means the 365 complete UTC days before today: [UTC today-365d, UTC today), excluding today and never data-max anchored.`

	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"explore_lfx_semantic_layer", RegisterExploreSemanticLayer},
		{"query_lfx_semantic_layer", RegisterQuerySemanticLayer},
		{"query_lfx_lens", RegisterQueryLFXLens},
	} {
		desc := listRegisteredTool(t, tc.name, tc.register).Description
		if !strings.Contains(desc, convention) {
			t.Errorf("%s description does not carry the shared RC-2 convention", tc.name)
		}
		if !strings.Contains(desc, "State inclusive dates plus the exclusive UTC anchor") {
			t.Errorf("%s description does not require a concrete date range and anchor", tc.name)
		}
	}
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
	if !strings.Contains(tool.Description, "contributor, activity and membership questions belong to the semantic layer") {
		t.Error("query_lfx_lens description should hand memberships to the semantic layer explicitly")
	}

	input := schemaPropertyDescription(t, tool, "input")
	if strings.Contains(input, "Always use for memberships") {
		t.Errorf("query_lfx_lens input schema still claims memberships: %q", input)
	}
	if !strings.Contains(input, "Contributor, activity and membership questions belong to the semantic layer") {
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
