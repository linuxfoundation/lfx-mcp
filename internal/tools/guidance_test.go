// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The guidance tools replaced the explore tool's help action: tool results
// carry no byte budget, and tool descriptions/results are the only MCP
// surfaces that reach the model in every client. These tests pin the content
// the same way TestDoctrineHelp pinned the help action — if a token
// disappears, a failure pattern that produced wrong answers in the evals
// loses its recipe.

// TestGuidanceDescriptions_ShortAndFunctional keeps the guidance tools'
// descriptions small: their job is to be found and read, not to compete with
// the query tools for routing budget.
func TestGuidanceDescriptions_ShortAndFunctional(t *testing.T) {
	for name, desc := range map[string]string{
		"read_lfx_semantic_layer_guidance": semanticLayerGuidanceDescription,
		"read_lfx_deck_building_guidance":  deckBuildingGuidanceDescription,
		"read_lfx_saved_queries_guidance":  savedQueriesGuidanceDescription,
	} {
		if got := len(desc); got > 400 {
			t.Errorf("%s description is %d bytes; guidance descriptions stay short (<=400)", name, got)
		}
		if !strings.Contains(desc, "Read") {
			t.Errorf("%s description should instruct when to read it", name)
		}
	}
	// The semantic layer guidance is shared by explore and query; its
	// description names the tools it covers so one read is understood to be
	// enough.
	for _, want := range []string{"explore_lfx_semantic_layer", "query_lfx_semantic_layer", "query_lfx_lens", "once per session"} {
		if !strings.Contains(semanticLayerGuidanceDescription, want) {
			t.Errorf("semantic layer guidance description missing %q", want)
		}
	}
}

// TestGuidanceTools_RegisterReadOnly checks both tools register under their
// gateable names with read-only annotations.
func TestGuidanceTools_RegisterReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"read_lfx_semantic_layer_guidance", RegisterSemanticLayerGuidance},
		{"read_lfx_deck_building_guidance", RegisterDeckBuildingGuidance},
		{"read_lfx_saved_queries_guidance", RegisterSavedQueriesGuidance},
	} {
		tool := listRegisteredTool(t, tc.name, tc.register)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s must carry ReadOnlyHint", tc.name)
		}
	}
}

// TestGuidanceHandlers_ReturnTheDocuments checks the handlers hand back the
// embedded documents verbatim.
func TestGuidanceHandlers_ReturnTheDocuments(t *testing.T) {
	res, _, err := handleSemanticLayerGuidance(context.Background(), nil, GuidanceArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resultText(t, res); got != semanticLayerGuidance || len(got) < 5000 {
		t.Errorf("semantic layer guidance result is not the embedded document (len %d)", len(got))
	}
	res, _, err = handleDeckBuildingGuidance(context.Background(), nil, GuidanceArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resultText(t, res); got != deckBuildingGuidance || len(got) < 2000 {
		t.Errorf("deck building guidance result is not the embedded document (len %d)", len(got))
	}
	res, _, err = handleSavedQueriesGuidance(context.Background(), nil, GuidanceArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resultText(t, res); got != savedQueriesGuidance || len(got) < 1500 {
		t.Errorf("saved queries guidance result is not the embedded document (len %d)", len(got))
	}
}

// TestSemanticLayerGuidanceContent pins the doctrine: every recipe verified
// against the live layer during the August 2026 evals, plus the failure
// modes the 2026-08-31 post-deploy eval rounds surfaced (rollup direction,
// person-grain rankings, events/training account dimensions, LF-wide scope,
// one-hop saved-query filters).
func TestSemanticLayerGuidanceContent(t *testing.T) {
	text := semanticLayerGuidance
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
		"There is no separate project parameter",
		"conformed lens",
		"asset_id__project_slug",
		"registration_id__project_slug",
		"event_id__project_name",
		"maintainer_key__cm_project_grandparents_slug",
		"health_metric_key__foundation_slug",
		"counts only, never sums",
		"__segment_slug",
		"PCC-style foundation rollups",
		`"Direct children of X"`,
		// LF-wide scope ambiguity (round-2 eval divergence)
		"'tlf' slug is the umbrella",
		// syntax
		"metric_time__year",
		"yyyy-mm-dd",
		"ceiling 500",
		// value discovery
		"'Asia Pacific'",
		"Viet Nam",
		"asset_id__billing_country",
		"zero rows",
		// bots
		"member_is_bot",
		"bot_activities",
		"roughly 1.8x",
		// org shares and headcounts
		"org-ATTRIBUTED",
		"Individual - No Account",
		"2-4x",
		// name discovery and rollups
		"International Business Machines Corporation",
		"Red Hat LLC",
		"account__account_rollup_name",
		"subsidiaries INTO parents",
		// tiers, health, value
		"Premier Membership",
		"Critical <20, Unsteady",
		"total_software_value",
		"COCOMO",
		// populations, maintainers, regions, person grain
		"total_contributors_with_collaboration",
		"2000-01-01 sentinel",
		"maintainer_key__is_lf_project",
		"organization_lf_region",
		"activity_project_id__member_display_name",
		"not identity keys",
		// governance and meetings routing
		"search_committees",
		"search_committee_members",
		"Never infer a roster",
		"search_past_meetings",
		"not answerable with governed data today",
		"disclose there is no canonical way",
		// events/training account entities
		"account__account_name",
		"NULL bucket",
		// saved queries
		"one-hop",
		"query_lfx_semantic_layer_saved_queries",
		"PREFER it over the",
		"read_lfx_deck_building_guidance",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("semantic layer guidance missing %q", want)
		}
	}
}

// TestDeckBuildingGuidanceContent pins the deck workflow: saved queries
// first, the pair-grain reading contract, rollup direction, and the honest
// out-of-scope list.
func TestDeckBuildingGuidanceContent(t *testing.T) {
	text := deckBuildingGuidance
	for _, want := range []string{
		"read_lfx_semantic_layer_guidance",
		"query_lfx_semantic_layer_saved_queries",
		"kpi_members_and_dues_by_account",
		"kpi_contributors_by_org",
		"kpi_event_registrations_by_org",
		"kpi_training_enrollments_by_org",
		"one-hop",
		"no order_by",
		"read_lfx_saved_queries_guidance",
		"subsidiaries INTO parents",
		"Meeting attendance by company",
		"definitional delta",
		"Be honest about what each figure represents",
		"label the\nresult as generated SQL",
		"maintainers to activity at person grain",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("deck building guidance missing %q", want)
		}
	}
}

// TestSavedQueriesGuidanceContent pins the saved-query catalog annotations
// and the reading contract that moved here from the tool description: the
// pair grain, the NULL-attribution row, rollup direction, one-hop filters,
// and the not-deployed fallback.
func TestSavedQueriesGuidanceContent(t *testing.T) {
	text := savedQueriesGuidance
	for _, want := range []string{
		// catalog annotations
		"as-of today (active terms)",
		"YTD-bounded",
		"tier literals\n  differ per foundation",
		"churn-date axis",
		"never sum rows - people span projects",
		"VOLUME by organization",
		"HEADCOUNT by organization",
		"rows, not\n  people",
		"TI+edX",
		"NULL account",
		// mechanics
		"no order_by",
		"ONE-HOP",
		"get_dimension_values",
		"zero rows",
		"ceiling 500",
		// reading contract
		"PAIR grain",
		"never present it as an organization",
		"subsidiaries INTO parents",
		"not deployed yet",
		"fall back to query_lfx_semantic_layer",
		// routing
		"read_lfx_deck_building_guidance",
		"prefer it over the explore+query flow",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("saved queries guidance missing %q", want)
		}
	}
}
