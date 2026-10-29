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

// countGateBaseline is, for one countable type, a count_lfx_resources call
// a caller without full view is allowed to make (a valid parent and the view
// fixtures it needs), and the arguments that caller may add to it with a
// probing value and still have the count run, each with the reason the
// people rule lets it through. The argument walk starts every probe from
// this baseline, so a refusal is the probed field's own and never the
// missing parent's; every field not listed in pass must be refused by
// peopleCountGate before the upstream call.
type countGateBaseline struct {
	args  CountLFXResourcesArgs
	setup func(api *stubLFXAPI)
	pass  map[string]string
}

const walkPastMeetingID = "91461158520-1771596000000"

// countGateBaselines is the explicit allow table of the argument walk, by
// type. A field in neither outcome (refused, or listed here) fails the walk
// with the field and type named.
var countGateBaselines = map[string]countGateBaseline{
	// committee_member: the bare type is accepted (member counts are on
	// every group page); every probing value is refused: a wrong-kind
	// parent, a tag, filter, name or date naming a person, a group key
	// outside the four structural prefixes, any metric.
	committeeMemberResourceType: {args: CountLFXResourcesArgs{Type: committeeMemberResourceType}},
	// v1_meeting_registrant: parent=meeting:<id> alone, as its organizer;
	// nothing may be added, the screen giving the one figure.
	meetingRegistrantResourceType: {
		args:  CountLFXResourcesArgs{Type: meetingRegistrantResourceType, Parent: "meeting:M1"},
		setup: func(api *stubLFXAPI) { api.GrantRelations("v1_meeting:M1#organizer") },
	},
	// v1_past_meeting_participant: parent=past_meeting:<id> as its
	// organizer, the view shown the whole attendance list. Full access
	// (the public-meeting case) keeps the plain count only:
	// TestCountLFXResources_AggregationFollowsThePeopleGate.
	pastMeetingParticipantResourceType: {
		args: CountLFXResourcesArgs{Type: pastMeetingParticipantResourceType, Parent: "past_meeting:" + walkPastMeetingID},
		setup: func(api *stubLFXAPI) {
			api.GrantRelations("v1_past_meeting:" + walkPastMeetingID + "#organizer")
			api.Respond(resourcesPath, page([]string{pastMeetingDoc(walkPastMeetingID)}, ""))
		},
		pass: map[string]string{
			"group_by":      "the organizer is shown the whole attendance list, so a count per tag value over it adds nothing",
			"group_by_size": "follows group_by",
			"metric":        "the organizer is shown the whole attendance list, so a distinct count over it adds nothing",
		},
	},
	// groupsio_member: parent=groupsio_mailing_list:<uid> for a list the
	// caller manages; the parent bounds every other argument to a list
	// whose members the caller is shown in full.
	mailingListMemberResourceType: {
		args:  CountLFXResourcesArgs{Type: mailingListMemberResourceType, Parent: "groupsio_mailing_list:" + mlWriterUID},
		setup: func(api *stubLFXAPI) { api.GrantRelations(listRelation(mlWriterUID, "writer")) },
		// The gate admits the count only to the list's managers and
		// auditors, who are shown the whole member list on screen, so a
		// breakdown over it adds nothing.
		pass: map[string]string{
			"name":          "bounded by the parent to a list the caller is shown in full",
			"tags":          "bounded by the parent to a list the caller is shown in full",
			"tags_all":      "bounded by the parent to a list the caller is shown in full",
			"date_field":    "bounded by the parent to a list the caller is shown in full",
			"date_from":     "bounded by the parent to a list the caller is shown in full",
			"date_to":       "bounded by the parent to a list the caller is shown in full",
			"filters_or":    "bounded by the parent to a list the caller is shown in full",
			"filters_all":   "bounded by the parent to a list the caller is shown in full",
			"group_by":      "bounded by the parent to a list the caller is shown in full",
			"group_by_size": "follows group_by",
			"metric":        "bounded by the parent to a list the caller is shown in full",
		},
	},
	meetingResourceType:               {args: CountLFXResourcesArgs{Type: meetingResourceType}, pass: meetingCountPassThroughs},
	pastMeetingResourceType:           {args: CountLFXResourcesArgs{Type: pastMeetingResourceType}, pass: meetingCountPassThroughs},
	meetingAttachmentResourceType:     {args: CountLFXResourcesArgs{Type: meetingAttachmentResourceType}, pass: meetingCountPassThroughs},
	pastMeetingAttachmentResourceType: {args: CountLFXResourcesArgs{Type: pastMeetingAttachmentResourceType}, pass: meetingCountPassThroughs},
	committeeResourceType:             {args: CountLFXResourcesArgs{Type: committeeResourceType}, pass: nonPeopleCountPassThroughs},
	projectResourceType:               {args: CountLFXResourcesArgs{Type: projectResourceType}, pass: nonPeopleCountPassThroughs},
	memberResourceType:                {args: CountLFXResourcesArgs{Type: memberResourceType}, pass: nonPeopleCountPassThroughs},
	b2bOrgResourceType:                {args: CountLFXResourcesArgs{Type: b2bOrgResourceType}, pass: nonPeopleCountPassThroughs},
	mailingListResourceType:           {args: CountLFXResourcesArgs{Type: mailingListResourceType}, pass: nonPeopleCountPassThroughs},
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
		size := 5
		args.GroupBy = "email"
		args.GroupBySize = &size
	case "metric":
		args.Metric = "cardinality:email"
	default:
		t.Fatalf("CountLFXResourcesArgs field %q has no probe: classify it in countGateProbe and, where a type lets it through, in countGateBaselines", field)
	}
}

// countArgsJSONFields lists the json names of CountLFXResourcesArgs other
// than type, in struct order.
func countArgsJSONFields(t *testing.T) []string {
	t.Helper()
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
	return fields
}

// runCountGateWalkCall runs one count_lfx_resources call as a caller
// without full view against a fresh stub with the baseline's fixtures, and
// reports whether the gate refused it (with its text) and how many count
// calls reached the stub.
func runCountGateWalkCall(t *testing.T, baseline countGateBaseline, args CountLFXResourcesArgs) (refused bool, text string, counted int) {
	t.Helper()
	api := setupCountTest(t)
	if baseline.setup != nil {
		baseline.setup(api)
	} else {
		api.GrantRelations() // no relation: every view predicate answers "not shown"
	}
	api.Respond(countPath, `{"count": 1, "has_more": false}`)
	res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), args)
	text = allResultText(t, res)
	counted = len(api.RequestsTo(countPath))
	refused = res.IsError && strings.HasPrefix(text, "Error: counting "+args.Type)
	if !refused && (res.IsError || counted != 1) {
		t.Fatalf("neither the gate's refusal nor a counted pass-through (isError=%v, counts=%d): %s", res.IsError, counted, text)
	}
	return refused, text, counted
}

// TestCountLFXResources_EveryArgumentIsClassifiedWithoutFullView walks every
// field of CountLFXResourcesArgs over every countable type as a caller without
// full view. Each type's baseline call (countGateBaselines) must pass; then
// each field is set to a probing value on top of it, and the outcome must be
// the gate's refusal before any upstream call, or the field must be in the
// baseline's allow table. A field in neither outcome, or one the table
// claims but the gate refuses, fails with the field and type named.
func TestCountLFXResources_EveryArgumentIsClassifiedWithoutFullView(t *testing.T) {
	fields := countArgsJSONFields(t)
	for _, resourceType := range countableResourceTypes {
		baseline, ok := countGateBaselines[resourceType]
		if !ok {
			t.Fatalf("countable type %s has no baseline in countGateBaselines", resourceType)
		}
		t.Run(resourceType+"/baseline", func(t *testing.T) {
			if refused, text, _ := runCountGateWalkCall(t, baseline, baseline.args); refused {
				t.Fatalf("the baseline call must be accepted: %s", text)
			}
		})
		for _, field := range fields {
			t.Run(resourceType+"/"+field, func(t *testing.T) {
				args := baseline.args
				countGateProbe(t, field, &args)
				refused, text, counted := runCountGateWalkCall(t, baseline, args)
				reason, passes := baseline.pass[field]
				switch {
				case refused && counted != 0:
					t.Fatalf("refused after %d upstream count call(s): %s", counted, text)
				case refused && passes:
					t.Fatalf("the allow table says %s passes on %s (%s), but the gate refused it: %s", field, resourceType, reason, text)
				case refused:
					assertNoEmail(t, text)
				case !passes:
					t.Fatalf("field %q on type %s reached the query service without full view and is in no outcome: refuse it in peopleCountGate or add it to the type's baseline in countGateBaselines with its reason", field, resourceType)
				}
			})
		}
	}
}

// TestCountLFXResources_ParentRequiredTypesRefuseTheBareType keeps the
// missing-parent refusal covered apart from the walk: the people types whose
// baseline names a parent are refused without one, before any count.
func TestCountLFXResources_ParentRequiredTypesRefuseTheBareType(t *testing.T) {
	for resourceType, baseline := range countGateBaselines {
		if baseline.args.Parent == "" {
			continue
		}
		t.Run(resourceType, func(t *testing.T) {
			refused, text, counted := runCountGateWalkCall(t, baseline, CountLFXResourcesArgs{Type: resourceType})
			if !refused || counted != 0 {
				t.Fatalf("expected the refusal and no count, got %s", text)
			}
		})
	}
}

// TestCountGateBaselinesNameOnlyRealFields keeps the allow table honest:
// every field it names exists on CountLFXResourcesArgs, every type it names
// is countable, and every entry carries a reason.
func TestCountGateBaselinesNameOnlyRealFields(t *testing.T) {
	argsType := reflect.TypeOf(CountLFXResourcesArgs{})
	known := map[string]bool{}
	for i := range argsType.NumField() {
		name, _, _ := strings.Cut(argsType.Field(i).Tag.Get("json"), ",")
		known[name] = true
	}
	for resourceType, baseline := range countGateBaselines {
		if !slices.Contains(countableResourceTypes, resourceType) {
			t.Errorf("allow table names a type that is not countable: %s", resourceType)
		}
		if baseline.args.Type != resourceType {
			t.Errorf("baseline for %s carries type %q", resourceType, baseline.args.Type)
		}
		for field, reason := range baseline.pass {
			if !known[field] {
				t.Errorf("allow table names a field CountLFXResourcesArgs lacks: %s.%s", resourceType, field)
			}
			if strings.TrimSpace(reason) == "" {
				t.Errorf("allow table entry %s.%s has no reason", resourceType, field)
			}
		}
	}
}
