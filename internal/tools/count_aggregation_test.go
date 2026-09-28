// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCountLFXResources_AggregationSchema(t *testing.T) {
	tool := listRegisteredTool(t, "count_lfx_resources", RegisterCountLFXResources)
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("unexpected schema: %T", tool.InputSchema)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties")
	}
	for name, typ := range map[string]string{"group_by": "string", "group_by_size": "integer", "metric": "string"} {
		property, ok := props[name].(map[string]any)
		if !ok || property["type"] != typ {
			t.Errorf("%s schema = %v, want %s", name, property, typ)
		}
	}
	if !reflect.DeepEqual(schema["required"], []any{"type"}) {
		t.Errorf("new aggregation args must be optional: required=%v", schema["required"])
	}
	for name, fragment := range map[string]string{"group_by": "Record tag prefix", "group_by_size": "requires group_by", "metric": "cardinality:email; not with group_by"} {
		if !strings.Contains(schemaPropertyDescription(t, tool, name), fragment) {
			t.Errorf("%s description missing %q", name, fragment)
		}
	}
	// Compare the whole tool surface with its pre-aggregation description budget.
	bytes := len(tool.Description)
	for _, name := range schemaProperties(t, tool) {
		bytes += len(schemaPropertyDescription(t, tool, name))
	}
	if bytes > 1862 {
		t.Errorf("tool and parameter descriptions grew: %d bytes", bytes)
	}
}

func TestCountLFXResources_AggregationPayloadMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		args CountLFXResourcesArgs
		want map[string][]string
	}{
		{"group", CountLFXResourcesArgs{GroupBy: "organization_id"}, map[string][]string{"group_by": {"organization_id"}}},
		{"size", CountLFXResourcesArgs{GroupBy: "committee_uid", GroupBySize: 1000}, map[string][]string{"group_by": {"committee_uid"}, "group_by_size": {"1000"}}},
		{"email metric", CountLFXResourcesArgs{Metric: "cardinality:email"}, map[string][]string{"metric": {"cardinality:email"}}},
		{"username metric", CountLFXResourcesArgs{Metric: "cardinality:username"}, map[string][]string{"metric": {"cardinality:username"}}},
		{"unset", CountLFXResourcesArgs{}, map[string][]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count":0,"has_more":false}`)
			tc.args.Type = "committee_member"
			res, _, err := handleCountLFXResources(context.Background(), stubCallToolRequest(), tc.args)
			if err != nil || res.IsError {
				t.Fatalf("unexpected error: %v; result=%v", err, res)
			}
			requests := api.RequestsTo(countPath)
			if len(requests) != 1 {
				t.Fatalf("want one count request, got %d", len(requests))
			}
			r := requests[0]
			assertExchangedAuth(t, r)
			tc.want["v"] = []string{"1"}
			tc.want["type"] = []string{tc.args.Type}
			if !reflect.DeepEqual(map[string][]string(r.Query), tc.want) {
				t.Errorf("query = %v, want %v", r.Query, tc.want)
			}
		})
	}
}

func TestCountLFXResources_GroupKeyAllowlist(t *testing.T) {
	want := strings.Fields("audience_access category committee_category committee_uid committee_voting_status display_name group_name is_attended is_invited is_member mailing_list_uid meeting_and_occurrence_id meeting_id meeting_type organization_id organization_name parent_b2b_org_uid parent_uid project_sfid project_slug project_uid public service_uid status timezone title type visibility voting_status")
	if !slices.Equal(countGroupKeys, want) || !slices.IsSorted(countGroupKeys) {
		t.Fatalf("group keys = %v, want sorted %v", countGroupKeys, want)
	}
	for _, key := range want {
		t.Run(key, func(t *testing.T) {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count":0,"has_more":false,"groups_complete":true}`)
			// A key is accepted even for a type that never emits it.
			res, _, err := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: "project", GroupBy: key})
			if err != nil || res.IsError {
				t.Fatalf("group key rejected: %v; result=%v", err, res)
			}
			if requests := api.RequestsTo(countPath); len(requests) != 1 || requests[0].Query.Get("group_by") != key {
				t.Errorf("key did not reach service unchanged: %v", requests)
			}
		})
	}
	for _, key := range []string{"email", "username", "committee_member_uid", "registrant_uid", "past_meeting_participant_uid", "member_uid", "project_membership_uid", "b2b_org_uid", "groupsio_mailing_list_uid", "past_meeting_id", "organization_website", "unknown", "Organization_id", "organization_id:example"} {
		t.Run("reject_"+key, func(t *testing.T) {
			api := setupCountTest(t)
			res, _, err := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: "committee_member", GroupBy: key})
			if err != nil || !res.IsError {
				t.Fatalf("expected error result: %v; result=%v", err, res)
			}
			wantMessage := fmt.Sprintf("Error: group_by %q is not a group key; group keys describe records, not people: %s", key, strings.Join(want, ", "))
			if text := strings.TrimSuffix(allResultText(t, res), "\n"); text != wantMessage {
				t.Errorf("error = %q, want %q", text, wantMessage)
			}
			if len(api.Requests()) != 0 {
				t.Error("invalid group key must be rejected before any HTTP request")
			}
		})
	}
}

func TestCountLFXResources_GroupedResults(t *testing.T) {
	const noGroupsNote = " No groups came back: a zero can mean the group_by prefix is not indexed for this type."
	if countNoGroupTagsNote != noGroupsNote {
		t.Errorf("empty groups must not claim the prefix exists: %q", countNoGroupTagsNote)
	}
	for _, tc := range []struct {
		name     string
		response string
		groups   string
		complete bool
		note     string
	}{
		{"exact", `{"count":42,"has_more":false,"groups":[{"key":"P1","count":30},{"key":"P2","count":12}],"groups_complete":true,"group_count_error_upper_bound":0}`, `[{"key":"P1","count":30},{"key":"P2","count":12}]`, true, ""},
		{"truncated", `{"count":42,"has_more":false,"groups":[{"key":"P1","count":30}],"groups_complete":false,"group_count_error_upper_bound":0}`, `[{"key":"P1","count":30}]`, false, countGroupsIncompleteNote},
		{"error bound", `{"count":42,"has_more":false,"groups":[{"key":"P1","count":30}],"groups_complete":true,"group_count_error_upper_bound":2}`, `[{"key":"P1","count":30}]`, true, countGroupErrorBoundNote},
		{"access cap", `{"count":42,"has_more":true,"groups":[{"key":"P1","count":30}],"groups_complete":false,"group_count_error_upper_bound":2}`, `[{"key":"P1","count":30}]`, false, countLowerBoundNote + countGroupsIncompleteNote + countGroupErrorBoundNote},
		{"has_more dominates", `{"count":42,"has_more":true,"groups":[{"key":"P1","count":30}],"groups_complete":true}`, `[{"key":"P1","count":30}]`, false, countLowerBoundNote},
		{"no match", `{"count":42,"has_more":false,"groups_complete":true,"group_count_error_upper_bound":0}`, `[]`, true, countNoGroupTagsNote},
		{"missing completeness", `{"count":42,"has_more":false}`, `[]`, false, countGroupsIncompleteNote + countNoGroupTagsNote},
		{"service order", `{"count":42,"has_more":false,"groups":[{"key":"Z","count":30},{"key":"A","count":12}],"groups_complete":true}`, `[{"key":"Z","count":30},{"key":"A","count":12}]`, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupCountTest(t)
			api.Respond(countPath, tc.response)
			res, _, err := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: "committee_member", GroupBy: "organization_id"})
			if err != nil || res.IsError {
				t.Fatalf("unexpected error: %v; result=%v", err, res)
			}
			out := resultJSON(t, res)
			if out["count"] != float64(42) || out["complete"] != tc.complete || out["visibility"] != "caller" || out["note"] != callerVisibilityNote+tc.note {
				t.Errorf("unexpected result: %v", out)
			}
			var wantGroups any
			if err := json.Unmarshal([]byte(tc.groups), &wantGroups); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(out["groups"], wantGroups) {
				t.Errorf("groups = %v, want %v", out["groups"], wantGroups)
			}
			var upstream map[string]any
			if err := json.Unmarshal([]byte(tc.response), &upstream); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"groups_complete", "group_count_error_upper_bound"} {
				if !reflect.DeepEqual(out[key], upstream[key]) {
					t.Errorf("%s = %v, want %v", key, out[key], upstream[key])
				}
			}
			for _, key := range []string{"metric_value", "metric_complete"} {
				if _, ok := out[key]; ok {
					t.Errorf("unrequested %s present", key)
				}
			}
		})
	}
}

func TestCountLFXResources_MetricResults(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		metric   string
		complete bool
		note     string
	}{
		{"exact", `{"count":42,"has_more":false,"metric_value":17,"metric_complete":true}`, "cardinality:email", true, ""},
		{"early stop", `{"count":42,"has_more":false,"metric_value":17,"metric_complete":false}`, "cardinality:email", false, countMetricIncompleteNote},
		{"access cap", `{"count":42,"has_more":true,"metric_value":17,"metric_complete":false}`, "cardinality:username", false, countLowerBoundNote + countMetricIncompleteNote},
		{"zero", `{"count":42,"has_more":false,"metric_value":0,"metric_complete":true}`, "cardinality:email", true, ""},
		{"missing completeness", `{"count":42,"has_more":false,"metric_value":17}`, "cardinality:email", false, countMetricIncompleteNote},
		{"has_more dominates", `{"count":42,"has_more":true,"metric_value":17,"metric_complete":true}`, "cardinality:email", false, countLowerBoundNote},
		{"plain", `{"count":42,"has_more":false}`, "", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupCountTest(t)
			api.Respond(countPath, tc.response)
			res, _, err := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: "committee_member", Metric: tc.metric})
			if err != nil || res.IsError {
				t.Fatalf("unexpected error: %v; result=%v", err, res)
			}
			out := resultJSON(t, res)
			if out["count"] != float64(42) || out["complete"] != tc.complete || out["visibility"] != "caller" || out["note"] != callerVisibilityNote+tc.note {
				t.Errorf("unexpected result: %v", out)
			}
			var upstream map[string]any
			if err := json.Unmarshal([]byte(tc.response), &upstream); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"metric_value", "metric_complete"} {
				got, present := out[key]
				want, wanted := upstream[key]
				if present != wanted || !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %v (present %v), want %v (present %v)", key, got, present, want, wanted)
				}
			}
			for _, key := range []string{"groups", "groups_complete", "group_count_error_upper_bound"} {
				if _, ok := out[key]; ok {
					t.Errorf("unrequested %s present", key)
				}
			}
		})
	}
}

func TestCountLFXResources_AggregationServiceErrorsPassThrough(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    CountLFXResourcesArgs
		message string
	}{
		{"metric per group", CountLFXResourcesArgs{GroupBy: "organization_id", Metric: "cardinality:email"}, "metric per group is not supported; group first, then count each group with tags"},
		{"size without group", CountLFXResourcesArgs{GroupBySize: 5}, "group_by_size requires group_by; omit group_by_size for plain counts or metrics"},
		{"sum", CountLFXResourcesArgs{Metric: "sum:amount"}, "metric must be cardinality:<tag_prefix>; sum is not available on this index (data fields are flat_object)"},
		{"untrimmed metric", CountLFXResourcesArgs{Metric: " cardinality:email "}, "invalid metric"},
		{"negative size", CountLFXResourcesArgs{GroupBy: "category", GroupBySize: -1}, "group_by_size must be at least 1"},
		{"oversized", CountLFXResourcesArgs{GroupBy: "category", GroupBySize: 1001}, "group_by_size must be at most 1000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupCountTest(t)
			body, err := json.Marshal(map[string]string{"message": tc.message})
			if err != nil {
				t.Fatal(err)
			}
			api.RespondStatus(countPath, http.StatusBadRequest, string(body))
			tc.args.Type = "committee_member"
			res, _, err := handleCountLFXResources(context.Background(), stubCallToolRequest(), tc.args)
			if err != nil || !res.IsError {
				t.Fatalf("expected error result: %v; result=%v", err, res)
			}
			if text := strings.TrimSuffix(allResultText(t, res), "\n"); text != "Failed to count resources: BadRequest: "+tc.message {
				t.Errorf("service error changed: %q", text)
			}
			requests := api.RequestsTo(countPath)
			if len(requests) != 1 {
				t.Fatalf("want one request and no retry, got %d", len(requests))
			}
			q := requests[0].Query
			if q.Get("group_by") != tc.args.GroupBy || q.Get("metric") != tc.args.Metric {
				t.Errorf("request rewritten: %v", q)
			}
			if tc.args.GroupBySize != 0 && q.Get("group_by_size") != fmt.Sprint(tc.args.GroupBySize) {
				t.Errorf("size rewritten: %v", q)
			}
		})
	}
}
