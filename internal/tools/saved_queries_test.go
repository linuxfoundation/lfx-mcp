// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func listSavedQueriesTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return listRegisteredTool(t, "query_lfx_semantic_layer_saved_queries", RegisterSavedQueries)
}

// TestSavedQueriesDescription_FitsSchemaBudget holds the tool to the same
// budget as its two siblings: everything past the limit is invisible to the
// model, and this description IS the saved-query catalog.
func TestSavedQueriesDescription_FitsSchemaBudget(t *testing.T) {
	if got := len(savedQueriesDescription); got > schemaDescriptionBudget {
		t.Errorf("query_lfx_semantic_layer_saved_queries description is %d bytes; everything past %d is invisible to the model",
			got, schemaDescriptionBudget)
	}
	for _, property := range []string{"saved_query", "where", "limit"} {
		if got := len(schemaPropertyDescription(t, listSavedQueriesTool(t), property)); got > schemaDescriptionBudget {
			t.Errorf("saved_queries.%s description is %d bytes; limit %d", property, got, schemaDescriptionBudget)
		}
	}
}

// TestSavedQueriesDescription_ListsAllSavedQueries pins the catalog: the
// description is the only place a model learns the names, so every saved
// query defined in lf-dbt's models/semantic_saved_queries.yml must appear.
// When a saved query is added or retired there, this list changes with it.
func TestSavedQueriesDescription_ListsAllSavedQueries(t *testing.T) {
	for _, name := range []string{
		"kpi_members_and_dues_by_account",
		"kpi_new_members_by_year",
		"kpi_membership_tier_split",
		"kpi_membership_churn",
		"kpi_maintainers_by_org",
		"kpi_contributors_by_project",
		"kpi_contributions_by_org",
		"kpi_contributors_by_org",
		"kpi_event_registrations",
		"kpi_training_enrollments",
		"kpi_event_registrations_by_org",
		"kpi_training_enrollments_by_org",
	} {
		if !strings.Contains(savedQueriesDescription, name) {
			t.Errorf("description does not list saved query %q", name)
		}
	}
}

// TestSavedQueriesDescription_CarriesReadingContract pins the result-reading
// rules that repeatedly go wrong when left to the model: the pair grain on
// the *_by_org results, the NULL-attribution row, and the
// not-yet-deployed fallback route.
func TestSavedQueriesDescription_CarriesReadingContract(t *testing.T) {
	for _, want := range []string{
		// Pair grain: reading the rollup column as a ranking without
		// client-side re-aggregation has produced wrong deck figures.
		"PAIR grain",
		"sum rows sharing the rollup value client-side",
		// The NULL account row is the largest row in every org split and has
		// been presented as an organization.
		"never present it as an organization",
		// Saved queries deploy with the dbt project; the tool can be enabled
		// before the manifest carries them, and the failure must route back
		// to the ad-hoc tools instead of being read as missing data.
		"not deployed yet",
		"fall back to query_lfx_semantic_layer",
		// The preference chain: a matching recipe outranks explore+query.
		"prefer this tool over the explore_lfx_semantic_layer",
	} {
		if !strings.Contains(savedQueriesDescription, want) {
			t.Errorf("description lost the reading contract fragment %q", want)
		}
	}
}

// TestSavedQueries_RequiredParamSurvivesCompaction mirrors the compaction
// contract on the other semantic layer tools: only the tool description and
// REQUIRED parameter descriptions reliably reach the model, so the saved
// query name being fixed-recipe (no metrics/group_by) must be stated on the
// required saved_query parameter, not only on the optional ones.
func TestSavedQueries_RequiredParamSurvivesCompaction(t *testing.T) {
	desc := schemaPropertyDescription(t, listSavedQueriesTool(t), "saved_query")
	for _, want := range []string{"metrics", "group_by"} {
		if !strings.Contains(desc, want) {
			t.Errorf("saved_query description does not mention %q — the fixed-recipe contract must survive schema compaction", want)
		}
	}
}
