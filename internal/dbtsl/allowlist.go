// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package dbtsl provides a client for the dbt Semantic Layer API.
package dbtsl

import (
	"fmt"
	"sort"
	"strings"
)

// Insights metric allowlist.
//
// Only these metrics are exposed through the semantic layer tools. They are
// grouped by Insights domain for maintainability: to add a metric, append it
// to the domain it belongs to, and add any new topic word to searchableTopics.

var membershipsMetrics = []string{
	"membership_revenue",
	"renewal_price",
	"current_membership_revenue",
	"current_membership_count",
	"current_new_account_membership_count",
	"churned_membership_count",
	"last_completed_year_active_membership_count",
	"last_completed_year_active_membership_revenue",
	"total_discount_amount",
	"total_downgrade_churn_amount",
	"total_invoice_amount",
	"total_next_membership_revenue",
	"current_membership_discount_amount",
	"current_membership_invoice_amount",
	"churned_membership_discount_amount",
	"churned_membership_invoice_amount",
	"last_completed_year_active_discount_amount",
	"last_completed_year_active_invoice_amount",
}

var activitiesMetrics = []string{
	"total_activities",
	"total_code_insertions",
	"total_code_deletions",
	"total_first_time_contributors",
	"total_contributors",
	"total_contributing_organizations",
	"code_contribution_activities",
	"main_branch_commits",
	"approved_pull_requests",
	"bot_activities",
	"human_activities",
	"lf_project_activities",
}

var maintainersMetrics = []string{
	"total_maintainers",
	"active_maintainers",
	"total_maintainer_records",
	"active_maintainer_records",
}

var healthValueMetrics = []string{
	"avg_project_health_score",
	"project_health_count",
	"total_software_value",
	"total_estimated_cost",
}

var projectsMetrics = []string{
	"project_count",
}

var eventsMetrics = []string{
	"total_events",
	"past_events_count",
	"upcoming_events_count",
	"total_registrations",
	"total_gross_revenue",
	"total_registration_tax",
	"total_registration_net_revenue",
	"total_event_registrations_goal",
	"total_sponsorship_revenue",
	"total_sponsorship_count",
	"sponsorship_quantity_total",
	"total_speakers",
	"total_speaking_engagements",
	"total_accepted_proposals",
	"past_event_speakers",
}

var educationMetrics = []string{
	"total_enrollments",
	"total_enrolled_users",
	"training_enrollments",
	"certification_enrollments",
	"total_certifications",
}

// metricsAllowlist is the union of every domain above.
var metricsAllowlist = func() map[string]struct{} {
	allowlist := make(map[string]struct{})
	for _, domain := range [][]string{
		membershipsMetrics,
		activitiesMetrics,
		maintainersMetrics,
		healthValueMetrics,
		projectsMetrics,
		eventsMetrics,
		educationMetrics,
	} {
		for _, name := range domain {
			allowlist[name] = struct{}{}
		}
	}
	return allowlist
}()

// searchableTopics are topic words known to match metric names, one group per
// domain above.
//
// Search matches names and descriptions only, so a caller guessing a domain
// word can land on nothing with no way forward. These are what we offer
// instead. Adding a domain means adding its words here, and
// TestEverySearchableTopicMatchesAMetric fails if one stops matching.
var searchableTopics = []string{
	"membership",
	"revenue",
	"churn",
	"contributor",
	"contribution",
	"activities",
	"maintainer",
	"health",
	"software",
	"project",
	"event",
	"registration",
	"sponsorship",
	"speaker",
	"enrollment",
	"certification",
}

// IsAllowedMetric reports whether name is in the Insights allowlist.
func IsAllowedMetric(name string) bool {
	_, ok := metricsAllowlist[name]
	return ok
}

// ValidateMetrics returns the names that are not in the allowlist.
func ValidateMetrics(names []string) []string {
	var disallowed []string
	for _, name := range names {
		if !IsAllowedMetric(name) {
			disallowed = append(disallowed, name)
		}
	}
	return disallowed
}

// AllowedMetricNames returns every allowlisted metric name, sorted.
func AllowedMetricNames() []string {
	names := make([]string, 0, len(metricsAllowlist))
	for name := range metricsAllowlist {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SearchableTopics returns the topic words offered when a search finds nothing.
func SearchableTopics() []string {
	return append([]string(nil), searchableTopics...)
}

// NoMetricsDetail is the rejection message for a search that matched nothing.
//
// Like UnknownMetricsDetail, it never leaves the caller holding an empty
// result with nothing to try next.
func NoMetricsDetail(search string) string {
	return fmt.Sprintf(
		"No metrics matched %q. Search matches metric names and descriptions "+
			"only, so search by topic. Try one of: %s. Country, region and tier "+
			"are dimensions rather than metrics - pick a metric first, then call "+
			"get_dimensions to see how it can be sliced.",
		search, strings.Join(searchableTopics, ", "),
	)
}

// UnknownMetricsDetail is the rejection message for metric names outside the
// allowlist, naming plausible alternatives for each.
func UnknownMetricsDetail(disallowed []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Metrics not available: %s.", strings.Join(disallowed, ", "))
	for _, name := range disallowed {
		if suggestions := SuggestMetrics(name, 5); len(suggestions) > 0 {
			fmt.Fprintf(&b, " Did you mean (for %q): %s?", name, strings.Join(suggestions, ", "))
		}
	}
	b.WriteString(" Use list_metrics to see what is available.")
	return b.String()
}

// SuggestMetrics returns allowlisted metric names close to name.
//
// A caller that guesses a metric ("contributor_count" for "total_contributors")
// would otherwise get a rejection with no way forward, so the error can name
// plausible alternatives instead. Matches on shared underscore-separated words
// first, then falls back to fuzzy similarity.
func SuggestMetrics(name string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	var words []string
	for _, w := range strings.Split(strings.ToLower(name), "_") {
		if w != "" {
			words = append(words, w)
		}
	}

	type scored struct {
		score int
		name  string
	}

	// Substring rather than whole-word matching, so "contributor" reaches
	// "total_contributors". Score by matched word length, not match count, so
	// a distinctive term ("contributor") outranks a generic one ("count") that
	// half the allowlist shares. Ties break on name for a stable order.
	var matches []scored
	for metric := range metricsAllowlist {
		lower := strings.ToLower(metric)
		score := 0
		for _, w := range words {
			if strings.Contains(lower, w) {
				score += len(w)
			}
		}
		if score > 0 {
			matches = append(matches, scored{score: score, name: metric})
		}
	}
	if len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].score != matches[j].score {
				return matches[i].score > matches[j].score
			}
			return matches[i].name < matches[j].name
		})
		if len(matches) > limit {
			matches = matches[:limit]
		}
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.name)
		}
		return names
	}

	// Fallback: closest by similarity, mirroring difflib.get_close_matches
	// with its default 0.6 cutoff.
	const cutoff = 0.6
	type ranked struct {
		ratio float64
		name  string
	}
	var closest []ranked
	for _, metric := range AllowedMetricNames() {
		if r := similarityRatio(name, metric); r >= cutoff {
			closest = append(closest, ranked{ratio: r, name: metric})
		}
	}
	sort.SliceStable(closest, func(i, j int) bool { return closest[i].ratio > closest[j].ratio })
	names := make([]string, 0, limit)
	for _, c := range closest {
		if len(names) == limit {
			break
		}
		names = append(names, c.name)
	}
	return names
}
