// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// guidanceRoutingSection returns the routing section of the semantic-layer
// guidance, whitespace-normalised, as the other tools1 guidance pins do.
func guidanceRoutingSection(t *testing.T) string {
	t.Helper()
	_, routing, found := strings.Cut(semanticLayerGuidance, "## Routing\n")
	if !found {
		t.Fatal("guidance is missing its routing section")
	}
	routing, _, found = strings.Cut(routing, "\n## Protocol")
	if !found {
		t.Fatal("guidance is missing its protocol section after routing")
	}
	return strings.Join(strings.Fields(routing), " ")
}

// TestGuidanceSeparatesContactOfRecordFromSeatHolder pins the two answers to
// "who represents ORG": the membership's contact of record and the holder of
// the seat are different records, and the guidance must say so where rosters
// are routed (routing bullet, recipe 12, the seats sentence) and where the
// standard metrics describe organizations.
func TestGuidanceSeparatesContactOfRecordFromSeatHolder(t *testing.T) {
	routing := guidanceRoutingSection(t)
	const routingWant = `"Who represents ORG at FOUNDATION" has two true answers: the membership's contact of record (get_membership_key_contacts — key-contact roles such as Representative/Voting Contact, Authorized Signatory or Billing Contact; status Active or Inactive, no dates) and who holds the seat (get_org_committee_seats or search_committee_members — roster rows with voting_status Voting Rep, Alternate Voting Rep, Observer, Emeritus or None). They are different records and can name different people: return both, labelled, never one for the other.`
	if !strings.Contains(routing, routingWant) {
		t.Error("routing must separate the membership's contact of record from the seat holder")
	}

	semantic := strings.Join(strings.Fields(semanticLayerGuidance), " ")
	for _, want := range []string{
		"Never infer a roster from membership or event data. A membership's key contact is the contact of record, not a seat; a roster row is a seat, not the contact of record — label which one you cite.",
		"are not the route for an organisation's seats. The membership's contact of record is get_membership_key_contacts; the two can name different people.",
	} {
		if !strings.Contains(semantic, want) {
			t.Errorf("recipe 12 missing %q", want)
		}
	}

	standard := strings.Join(strings.Fields(standardMetricsGuidance), " ")
	const standardWant = `The standard metrics carry no people. Who represents an organization has two answers from two records: the membership's contact of record (get_membership_key_contacts) and the holder of a seat (get_org_committee_seats, search_committee_members). Read seats on Board and TOC/TSC committees and on the member-class rosters filed under category Other whose voting_status is Voting Rep or Alternate Voting Rep — never Board alone. A seat carries its created date only and a contact its updated date: cite each as recorded on its side, with its date, never as "current"; when the two name different people show both side by side, labelled, never merged.`
	if !strings.Contains(standard, standardWant) {
		t.Error("standard-metrics organizations section must separate the contact of record from the seat holder")
	}
}

// TestMaintainersRowCarriesRosterInheritanceCaveat pins the maintainers
// inventory row's caveat about inherited kernel-tree rosters, MAINTAINERS-file
// reviewers and per-project counting.
func TestMaintainersRowCarriesRosterInheritanceCaveat(t *testing.T) {
	standard := strings.Join(strings.Fields(standardMetricsGuidance), " ")
	for _, want := range []string{
		"by=maintainer has no series; per-project counts include people the roster inherits from vendored Linux kernel trees on kernel-fork projects, and MAINTAINERS-file reviewers count as maintainers",
		"a person maintaining two projects counts once in each and can sit in two role or source rows",
	} {
		if !strings.Contains(standard, want) {
			t.Errorf("maintainers row must carry %q", want)
		}
	}
}

// TestRepresentationDescriptionsSeparateContactsFromSeats pins the two tool
// descriptions on either side of the contact-of-record / seat-holder line.
func TestRepresentationDescriptionsSeparateContactsFromSeats(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
		wants    []string
	}{
		{
			name:     "get_membership_key_contacts",
			register: RegisterGetMembershipKeyContacts,
			wants:    []string{"contacts of record", "can name different people", "get_org_committee_seats", "search_committee_members"},
		},
		{
			name:     "get_org_committee_seats",
			register: RegisterGetOrgCommitteeSeats,
			wants:    []string{"Seats only", "get_membership_key_contacts"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := listRegisteredTool(t, tc.name, tc.register)
			for _, want := range tc.wants {
				if !strings.Contains(tool.Description, want) {
					t.Errorf("%s description missing %q", tc.name, want)
				}
			}
		})
	}
}
