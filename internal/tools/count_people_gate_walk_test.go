// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// countGatePassThroughs is the explicit allow table of the argument walk:
// for each countable type, the count_lfx_resources arguments (by json name)
// that a caller without full view may set to a probing value and still have
// the count run, each with the reason the people rule lets it through. Every
// other argument on every other type must be refused by peopleCountGate before
// the upstream call. A field in neither outcome fails the walk.
var countGatePassThroughs = map[string]map[string]string{
	meetingResourceType:               meetingCountPassThroughs,
	pastMeetingResourceType:           meetingCountPassThroughs,
	meetingAttachmentResourceType:     meetingCountPassThroughs,
	pastMeetingAttachmentResourceType: meetingCountPassThroughs,
	committeeResourceType:             nonPeopleCountPassThroughs,
	projectResourceType:               nonPeopleCountPassThroughs,
	memberResourceType:                nonPeopleCountPassThroughs,
	b2bOrgResourceType:                nonPeopleCountPassThroughs,
	mailingListResourceType:           nonPeopleCountPassThroughs,
	// committee_member, v1_meeting_registrant, v1_past_meeting_participant
	// and groupsio_member: no pass-through. Every probing value is refused
	// (a wrong-kind parent, a tag, filter, name, date, group key or metric
	// on committee_member; anything but the one accepted parent form on
	// the others).
}

// meetingCountPassThroughs are the arguments a meeting or attachment record
// count accepts unrestricted: the record is not a people record, and only
// its filters_or / filters_all and date_field can name a person's field, so
// those are the gated ones (meetingCountFilterFields, attachmentCountFilterFields).
var meetingCountPassThroughs = map[string]string{
	"parent":        "a meeting, past meeting or project ref; no parent kind names a person",
	"name":          "matches the record's title or file name, not a person",
	"tags":          "the indexer's structural tags on the record; none carries a person",
	"tags_all":      "the indexer's structural tags on the record; none carries a person",
	"group_by":      "a group key is a tag prefix; tags are unrestricted on these records",
	"group_by_size": "follows group_by",
	"metric":        "a distinct count over a tag; tags are unrestricted on these records",
}

// nonPeopleCountPassThroughs: the type is not a people record (AGENTS.md,
// nonPeopleTools); peopleCountGate does not read it.
var nonPeopleCountPassThroughs = map[string]string{
	"parent":        "not a people type",
	"name":          "not a people type",
	"tags":          "not a people type",
	"tags_all":      "not a people type",
	"date_field":    "not a people type",
	"date_from":     "not a people type",
	"date_to":       "not a people type",
	"filters_or":    "not a people type",
	"filters_all":   "not a people type",
	"group_by":      "not a people type",
	"group_by_size": "not a people type",
	"metric":        "not a people type",
}

// countGateProbe sets one argument of CountLFXResourcesArgs, named by its
// json name, to a value that would single out a person if it reached the
// query service, or fails the test for an argument it does not know: a new
// field on the struct has to be classified here before it ships.
func countGateProbe(t *testing.T, field string, args *CountLFXResourcesArgs) {
	t.Helper()
	switch field {
	case "parent":
		args.Parent = "b2b_org:x" // a ref kind no people type accepts
	case "name":
		args.Name = "x"
	case "tags":
		args.Tags = []string{"email:x"}
	case "tags_all":
		args.TagsAll = []string{"email:x"}
	case "date_field", "date_from", "date_to":
		// A range on a people path: the three fields are one probe.
		args.DateField, args.DateFrom, args.DateTo = "created_by.timestamp", "2026-01-01", "2026-12-31"
	case "filters_or":
		args.FiltersOr = []string{"email:x"}
	case "filters_all":
		args.FiltersAll = []string{"email:x"}
	case "group_by":
		args.GroupBy = "email"
	case "group_by_size":
		// Meaningless alone (the service's 400 answers it): probed with
		// the group key it bounds.
		args.GroupBy = "email"
		size := 5
		args.GroupBySize = &size
	case "metric":
		args.Metric = "cardinality:email"
	default:
		t.Fatalf("CountLFXResourcesArgs field %q has no probe: classify it in countGateProbe and, where a type lets it through, in countGatePassThroughs", field)
	}
}

// TestCountLFXResources_EveryArgumentIsClassifiedWithoutFullView walks every
// field of CountLFXResourcesArgs over every countable type as a caller without
// full view, with the predicate stubs answering "not shown": the outcome is
// the gate's refusal before any upstream call, or the field is in the allow
// table for that type. A field in neither outcome, or one the table claims
// but the gate refuses, fails with the field and type named.
func TestCountLFXResources_EveryArgumentIsClassifiedWithoutFullView(t *testing.T) {
	argsType := reflect.TypeOf(CountLFXResourcesArgs{})
	var fields []string
	for i := range argsType.NumField() {
		name, _, _ := strings.Cut(argsType.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			t.Fatalf("field %s has no json name", argsType.Field(i).Name)
		}
		if name == "type" {
			continue
		}
		fields = append(fields, name)
	}
	if len(fields) < 12 {
		t.Fatalf("expected the walk to cover at least 12 fields, found %d: %v", len(fields), fields)
	}
	for _, resourceType := range countableResourceTypes {
		for _, field := range fields {
			t.Run(resourceType+"/"+field, func(t *testing.T) {
				api := setupCountTest(t)
				api.GrantRelations() // no relation: every view predicate answers "not shown"
				api.Respond(countPath, `{"count": 1, "has_more": false}`)
				args := CountLFXResourcesArgs{Type: resourceType}
				countGateProbe(t, field, &args)
				res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), args)
				text := allResultText(t, res)
				counted := len(api.RequestsTo(countPath))
				reason, passes := countGatePassThroughs[resourceType][field]
				switch {
				case res.IsError && strings.HasPrefix(text, "Error: counting "+resourceType):
					if counted != 0 {
						t.Fatalf("refused after %d upstream count call(s): %s", counted, text)
					}
					if passes {
						t.Fatalf("the allow table says %s passes on %s (%s), but the gate refused it: %s", field, resourceType, reason, text)
					}
					assertNoEmail(t, text)
				case !res.IsError && counted == 1:
					if !passes {
						t.Fatalf("field %q on type %s reached the query service without full view and is in no outcome: refuse it in peopleCountGate or add it to countGatePassThroughs with its reason", field, resourceType)
					}
				default:
					t.Fatalf("field %q on type %s: neither the gate's refusal nor a counted pass-through (isError=%v, counts=%d): %s", field, resourceType, res.IsError, counted, text)
				}
			})
		}
	}
}

// TestCountGatePassThroughsNameOnlyRealFields keeps the allow table honest:
// every field it names exists on CountLFXResourcesArgs, and every type it
// names is countable.
func TestCountGatePassThroughsNameOnlyRealFields(t *testing.T) {
	argsType := reflect.TypeOf(CountLFXResourcesArgs{})
	known := map[string]bool{}
	for i := range argsType.NumField() {
		name, _, _ := strings.Cut(argsType.Field(i).Tag.Get("json"), ",")
		known[name] = true
	}
	for resourceType, fields := range countGatePassThroughs {
		if !slices.Contains(countableResourceTypes, resourceType) {
			t.Errorf("allow table names a type that is not countable: %s", resourceType)
		}
		for field, reason := range fields {
			if !known[field] {
				t.Errorf("allow table names a field CountLFXResourcesArgs lacks: %s.%s", resourceType, field)
			}
			if strings.TrimSpace(reason) == "" {
				t.Errorf("allow table entry %s.%s has no reason", resourceType, field)
			}
		}
	}
}
