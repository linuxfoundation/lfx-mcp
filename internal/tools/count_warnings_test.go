// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
)

func TestBuildCountResult_Warnings(t *testing.T) {
	const incompleteGroups = "Not every group is guaranteed to be present: more groups may exist than group_by_size, or the count stopped early; raise group_by_size or narrow the query."
	if countGroupsIncompleteWarning != incompleteGroups {
		t.Errorf("group warning must describe both causes without claiming missing groups: %q", countGroupsIncompleteWarning)
	}
	complete, incomplete := true, false
	zero, bound := uint64(0), uint64(2)
	groups := []*querysvc.CountGroup{{Key: "project-a", Count: 7}}
	for _, tc := range []struct {
		name            string
		result          querysvc.QueryResourcesCountResult
		groupsRequested bool
		metricRequested bool
		complete        bool
		warnings        []string
	}{
		{"none", querysvc.QueryResourcesCountResult{}, false, false, true, nil},
		{"zero group error bound", querysvc.QueryResourcesCountResult{Groups: groups, GroupsComplete: &complete, GroupCountErrorUpperBound: &zero}, true, false, true, nil},
		{"complete metric", querysvc.QueryResourcesCountResult{MetricValue: &zero, MetricComplete: &complete}, false, true, true, nil},
		{"access cap alone", querysvc.QueryResourcesCountResult{HasMore: true}, false, false, false, []string{countLowerBoundWarning}},
		{"group cap alone", querysvc.QueryResourcesCountResult{Groups: groups, GroupsComplete: &incomplete}, true, false, false, []string{countGroupsIncompleteWarning}},
		{"group error alone", querysvc.QueryResourcesCountResult{Groups: groups, GroupsComplete: &complete, GroupCountErrorUpperBound: &bound}, true, false, false, []string{countGroupErrorBoundWarning}},
		{"empty groups alone", querysvc.QueryResourcesCountResult{GroupsComplete: &complete}, true, false, true, []string{countNoGroupTagsWarning}},
		{"metric cap alone", querysvc.QueryResourcesCountResult{MetricComplete: &incomplete}, false, true, false, []string{countMetricIncompleteWarning}},
		{"missing group completeness", querysvc.QueryResourcesCountResult{Groups: groups}, true, false, false, []string{countGroupsIncompleteWarning}},
		{"missing metric completeness", querysvc.QueryResourcesCountResult{}, false, true, false, []string{countMetricIncompleteWarning}},
		{"access and group caps", querysvc.QueryResourcesCountResult{HasMore: true, Groups: groups, GroupsComplete: &incomplete, GroupCountErrorUpperBound: &bound}, true, false, false, []string{countLowerBoundWarning, countGroupsIncompleteWarning, countGroupErrorBoundWarning}},
		// The helper's ordering is independent of request validation, which
		// delegates rejection of simultaneous groups and metrics to the service.
		{"all warnings in order", querysvc.QueryResourcesCountResult{HasMore: true, GroupsComplete: &incomplete, GroupCountErrorUpperBound: &bound, MetricComplete: &incomplete}, true, true, false, []string{countLowerBoundWarning, countGroupsIncompleteWarning, countGroupErrorBoundWarning, countNoGroupTagsWarning, countMetricIncompleteWarning}},
		{"unrequested aggregates", querysvc.QueryResourcesCountResult{GroupsComplete: &incomplete, GroupCountErrorUpperBound: &bound, MetricComplete: &incomplete}, false, false, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.result.Count = 7
			out := buildCountResult(&tc.result, tc.groupsRequested, tc.metricRequested)
			if out.Count != 7 || out.Complete != tc.complete || out.Visibility != "caller" || out.Note != callerVisibilityNote {
				t.Errorf("unexpected count result: %+v", out)
			}
			if !reflect.DeepEqual(out.Warnings, tc.warnings) {
				t.Errorf("warnings = %#v, want %#v", out.Warnings, tc.warnings)
			}
			for _, warning := range out.Warnings {
				if strings.TrimSpace(warning) != warning || !strings.HasSuffix(warning, ".") {
					t.Errorf("warning must be a standalone sentence without surrounding whitespace: %q", warning)
				}
			}
			raw, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			if wire["note"] != callerVisibilityNote {
				t.Errorf("note changed on wire: %v", wire["note"])
			}
			assertCountWarnings(t, wire, tc.warnings)
		})
	}
}

// assertCountWarnings checks both ordered entries and absence when not needed.
func assertCountWarnings(t *testing.T, out map[string]any, want []string) {
	t.Helper()
	raw, present := out["warnings"]
	if want == nil {
		if present {
			t.Errorf("warnings must be omitted when none apply, got %v", raw)
		}
		return
	}
	warnings, ok := raw.([]any)
	if !present || !ok {
		t.Fatalf("warnings must be a JSON array, got %T %v", raw, raw)
	}
	got := make([]string, len(warnings))
	for i, warning := range warnings {
		value, ok := warning.(string)
		if !ok {
			t.Fatalf("warning must be a string, got %T", warning)
		}
		got[i] = value
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("warnings = %q, want %q", got, want)
	}
}
