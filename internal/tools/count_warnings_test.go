// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	const walkedBound = "Each group's count may undercount by up to group_count_error_upper_bound within the records the service walked, or by more if the count stopped early."
	if countGroupErrorBoundWarning != walkedBound {
		t.Errorf("bound warning must distinguish walked records from the full count: %q", countGroupErrorBoundWarning)
	}
	const missingBound = "The service reported no error bound for the returned group counts."
	if countMissingGroupErrorBoundWarning != missingBound {
		t.Errorf("missing bound warning must not invent a bound: %q", countMissingGroupErrorBoundWarning)
	}
	const noMatchingGroupTag = "No matching visible record carried the group_by tag."
	if countNoMatchingGroupTagWarning != noMatchingGroupTag {
		t.Errorf("empty grouping warning = %q", countNoMatchingGroupTagWarning)
	}
	const missingMetric = "The service reported no distinct count for the requested metric."
	if countMissingMetricWarning != missingMetric {
		t.Errorf("missing metric warning must not claim an early stop: %q", countMissingMetricWarning)
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
		{"group cap alone", querysvc.QueryResourcesCountResult{Groups: groups, GroupsComplete: &incomplete, GroupCountErrorUpperBound: &zero}, true, false, false, []string{countGroupsIncompleteWarning}},
		{"group error alone", querysvc.QueryResourcesCountResult{Groups: groups, GroupsComplete: &complete, GroupCountErrorUpperBound: &bound}, true, false, false, []string{countGroupErrorBoundWarning}},
		{"positive count with empty groups", querysvc.QueryResourcesCountResult{GroupsComplete: &complete, GroupCountErrorUpperBound: &zero}, true, false, true, []string{countNoMatchingGroupTagWarning}},
		{"metric cap alone", querysvc.QueryResourcesCountResult{MetricValue: &zero, MetricComplete: &incomplete}, false, true, false, []string{countMetricIncompleteWarning}},
		{"missing group completeness", querysvc.QueryResourcesCountResult{Groups: groups, GroupCountErrorUpperBound: &zero}, true, false, false, []string{countGroupsIncompleteWarning}},
		{"missing group bound", querysvc.QueryResourcesCountResult{Groups: groups, GroupsComplete: &complete}, true, false, false, []string{countMissingGroupErrorBoundWarning}},
		{"missing group metadata", querysvc.QueryResourcesCountResult{Groups: groups}, true, false, false, []string{countGroupsIncompleteWarning, countMissingGroupErrorBoundWarning}},
		{"missing metric completeness", querysvc.QueryResourcesCountResult{MetricValue: &zero}, false, true, false, []string{countMissingMetricWarning}},
		{"missing metric value", querysvc.QueryResourcesCountResult{MetricComplete: &complete}, false, true, false, []string{countMissingMetricWarning}},
		{"missing value and incomplete metric", querysvc.QueryResourcesCountResult{MetricComplete: &incomplete}, false, true, false, []string{countMissingMetricWarning}},
		{"access and group caps", querysvc.QueryResourcesCountResult{HasMore: true, Groups: groups, GroupsComplete: &incomplete, GroupCountErrorUpperBound: &bound}, true, false, false, []string{countLowerBoundWarning, countGroupsIncompleteWarning, countGroupErrorBoundWarning}},
		// The helper's ordering is independent of request validation, which
		// delegates rejection of simultaneous groups and metrics to the service.
		{"all warnings in order", querysvc.QueryResourcesCountResult{HasMore: true, GroupsComplete: &incomplete, GroupCountErrorUpperBound: &bound, MetricComplete: &incomplete}, true, true, false, []string{countLowerBoundWarning, countGroupsIncompleteWarning, countGroupErrorBoundWarning, countNoMatchingGroupTagWarning, countMissingMetricWarning}},
		{"unrequested aggregates", querysvc.QueryResourcesCountResult{GroupsComplete: &incomplete, GroupCountErrorUpperBound: &bound, MetricComplete: &incomplete}, false, false, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.result.Count = 7
			out := buildCountResult(context.Background(), slog.Default(), &tc.result, committeeMemberResourceType, tc.groupsRequested, tc.metricRequested)
			if out.Count != 7 || out.Complete != tc.complete || out.Visibility != "caller" || out.Note != callerVisibilityNote {
				t.Errorf("unexpected count result: %+v", out)
			}
			if tc.metricRequested && tc.result.MetricValue == nil && (out.MetricComplete == nil || *out.MetricComplete) {
				t.Errorf("missing metric value must emit metric_complete=false: %+v", out)
			}
			if !complete || incomplete {
				t.Error("output flags must not mutate the service's flags")
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

func TestBuildCountResult_NilGroupsAreIncomplete(t *testing.T) {
	complete, incomplete := true, false
	zero := uint64(0)
	valid := []*querysvc.CountGroup{{Key: "a", Count: 2}, {Key: "b", Count: 1}}
	for _, tc := range []struct {
		name           string
		groups         []*querysvc.CountGroup
		groupsComplete *bool
		hasMore        bool
		wantGroups     []countGroup
		warnings       []string
	}{
		{"mixed", []*querysvc.CountGroup{nil, valid[0], nil, valid[1]}, &complete, false, []countGroup{{Key: "a", Count: 2}, {Key: "b", Count: 1}}, []string{countGroupsIncompleteWarning}},
		{"already incomplete", []*querysvc.CountGroup{valid[0], nil}, &incomplete, false, []countGroup{{Key: "a", Count: 2}}, []string{countGroupsIncompleteWarning}},
		{"missing flag", []*querysvc.CountGroup{nil, valid[1]}, nil, false, []countGroup{{Key: "b", Count: 1}}, []string{countGroupsIncompleteWarning}},
		{"access cap", []*querysvc.CountGroup{nil, valid[0]}, &incomplete, true, []countGroup{{Key: "a", Count: 2}}, []string{countLowerBoundWarning, countGroupsIncompleteWarning}},
		{"all nil", []*querysvc.CountGroup{nil, nil}, &complete, false, []countGroup{}, []string{countGroupsIncompleteWarning, countNoMatchingGroupTagWarning}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
			out := buildCountResult(context.Background(), logger, &querysvc.QueryResourcesCountResult{Count: 3, HasMore: tc.hasMore, Groups: tc.groups, GroupsComplete: tc.groupsComplete, GroupCountErrorUpperBound: &zero}, committeeMemberResourceType, true, false)
			if out.Count != 3 || out.Complete || out.Note != callerVisibilityNote {
				t.Errorf("unexpected result: %+v", out)
			}
			if out.GroupsComplete == nil || *out.GroupsComplete {
				t.Errorf("discarded groups must emit groups_complete=false: %+v", out)
			}
			if !complete || incomplete {
				t.Error("output flags must not mutate the service's flags")
			}
			raw, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			if wire["groups_complete"] != false {
				t.Errorf("groups_complete must be present and false on the wire: %s", raw)
			}
			if out.Groups == nil || !reflect.DeepEqual(*out.Groups, tc.wantGroups) {
				t.Errorf("mapped groups = %v, want %v", out.Groups, tc.wantGroups)
			}
			if !reflect.DeepEqual(out.Warnings, tc.warnings) {
				t.Errorf("warnings = %q, want exactly %q", out.Warnings, tc.warnings)
			}
			if strings.Count(logs.String(), "level=WARN") != 1 || !strings.Contains(logs.String(), "discarded nil groups") {
				t.Errorf("missing single warning log: %s", logs.String())
			}
		})
	}
}

func TestCountLFXResources_ZeroCountsAreNotProofOfAbsence(t *testing.T) {
	for _, resourceType := range countableResourceTypes {
		t.Run(resourceType, func(t *testing.T) {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count":0,"has_more":false}`)
			res, _, err := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: resourceType})
			if err != nil || res == nil || res.IsError {
				t.Fatalf("unexpected error: %v; result=%v", err, res)
			}
			out := resultJSON(t, res)
			if out["count"] != float64(0) || out["complete"] != true || out["note"] != callerVisibilityNote {
				t.Errorf("unexpected result: %v", out)
			}
			want := fmt.Sprintf("No %s records matching these filters are visible to you; results cover only records you can view, so this is not proof of absence.", resourceType)
			assertCountWarnings(t, out, []string{want})
		})
	}
}

func TestBuildCountResult_ZeroWarningFollowsCompletenessWarnings(t *testing.T) {
	complete, incomplete := true, false
	zero := uint64(0)
	const visibilityWarning = "No committee_member records matching these filters are visible to you; results cover only records you can view, so this is not proof of absence."
	for _, tc := range []struct {
		name     string
		result   querysvc.QueryResourcesCountResult
		groups   bool
		metric   bool
		complete bool
		warnings []string
	}{
		{"walk stopped", querysvc.QueryResourcesCountResult{HasMore: true}, false, false, false, []string{countLowerBoundWarning, visibilityWarning}},
		{"empty groups", querysvc.QueryResourcesCountResult{GroupsComplete: &complete, GroupCountErrorUpperBound: &zero}, true, false, true, []string{visibilityWarning}},
		{"unknown group completeness", querysvc.QueryResourcesCountResult{}, true, false, false, []string{countGroupsIncompleteWarning, countMissingGroupErrorBoundWarning, visibilityWarning}},
		{"zero metric", querysvc.QueryResourcesCountResult{MetricValue: &zero, MetricComplete: &complete}, false, true, true, []string{visibilityWarning}},
		{"incomplete metric", querysvc.QueryResourcesCountResult{MetricValue: &zero, MetricComplete: &incomplete}, false, true, false, []string{countMetricIncompleteWarning, visibilityWarning}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := buildCountResult(context.Background(), slog.Default(), &tc.result, committeeMemberResourceType, tc.groups, tc.metric)
			if out.Count != 0 || out.Complete != tc.complete || out.Note != callerVisibilityNote {
				t.Errorf("unexpected result: %+v", out)
			}
			if !reflect.DeepEqual(out.Warnings, tc.warnings) {
				t.Errorf("warnings = %q, want %q", out.Warnings, tc.warnings)
			}
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
