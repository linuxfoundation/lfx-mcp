// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools implements the MCP tool handlers for the LFX MCP server.
package tools

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// read_lfx_semantic_layer_guidance / read_lfx_deck_building_guidance
// ---------------------------------------------------------------------------

// Guidance ships as tool results because tool descriptions and tool results
// are the only MCP surfaces that reach the model in every client (verified
// against Claude Cowork 2026-09-01: server instructions arrive, but
// resources and prompts are listed by the client and never read into model
// context, and only tool calls happened on the wire). A tool result carries
// no byte budget, so the doctrine that used to be squeezed into 2048-byte
// descriptions and the explore tool's help action lives here in full.
//
// One tool per audience rather than one tool with a topic parameter: each
// name is independently gateable through LFXMCP_TOOLS, so a deployment
// enables exactly the guidance its callers need. The guidance tools carry
// deliberately short descriptions — their job is to be found and read, not
// to compete with the query tools for routing.

//go:embed guidance/semantic-layer.md
var semanticLayerGuidance string

//go:embed guidance/deck-building.md
var deckBuildingGuidance string

//go:embed guidance/standard-metrics.md
var standardMetricsGuidance string

const semanticLayerGuidanceDescription = `Agent guidance for the LFX semantic layer and lens tools: routing, query syntax, scoping, worked recipes, failure modes. Read once per session BEFORE the first explore_lfx_semantic_layer, query_lfx_semantic_layer or query_lfx_lens call.`

const deckBuildingGuidanceDescription = `Agent guidance for building customer-facing metric decks and presentations from LFX data: recipe-first workflow, deck lane mapping, rollups, reconciliation, presentation rules. Read BEFORE assembling deck or briefing numbers.`

const standardMetricsGuidanceDescription = `Agent guidance for the LFX standard metrics: the inventory with result columns, time shape and caveats, how to scope and window a call, and how to read the results. Read once per session BEFORE the first query_lfx_standard_metrics call.`

// GuidanceArgs is the (empty) input for the guidance tools: the content is
// the whole point, so there is nothing to parameterize.
type GuidanceArgs struct{}

// RegisterSemanticLayerGuidance registers the read_lfx_semantic_layer_guidance tool.
func RegisterSemanticLayerGuidance(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_lfx_semantic_layer_guidance",
		Description: semanticLayerGuidanceDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:          "Read LFX Semantic Layer Guidance",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, handleSemanticLayerGuidance)
}

// RegisterDeckBuildingGuidance registers the read_lfx_deck_building_guidance tool.
func RegisterDeckBuildingGuidance(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_lfx_deck_building_guidance",
		Description: deckBuildingGuidanceDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:          "Read LFX Deck Building Guidance",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, handleDeckBuildingGuidance)
}

// RegisterStandardMetricsGuidance registers the read_lfx_standard_metrics_guidance tool.
func RegisterStandardMetricsGuidance(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_lfx_standard_metrics_guidance",
		Description: standardMetricsGuidanceDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:          "Read LFX Standard Metrics Guidance",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, handleStandardMetricsGuidance)
}

func handleStandardMetricsGuidance(_ context.Context, _ *mcp.CallToolRequest, _ GuidanceArgs) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: standardMetricsGuidance}},
	}, nil, nil
}

func handleSemanticLayerGuidance(_ context.Context, _ *mcp.CallToolRequest, _ GuidanceArgs) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: semanticLayerGuidance}},
	}, nil, nil
}

func handleDeckBuildingGuidance(_ context.Context, _ *mcp.CallToolRequest, _ GuidanceArgs) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: deckBuildingGuidance}},
	}, nil, nil
}
