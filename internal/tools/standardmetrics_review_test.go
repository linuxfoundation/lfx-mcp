// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"regexp"
	"strings"
	"testing"
)

// A maintainer activity series is not a reconstruction of past rosters. Put
// the exception beside each generic at-date rule, including the required
// metric field that survives schema compaction, rather than only in an example.
func TestMaintainerExceptionBesideEveryAtDateRule(t *testing.T) {
	const exception = "maintainers is the exception: today's roster only; with period, one row per period of today's maintainers active in it, not the roster at that time"
	tool := listStandardMetricsTool(t)
	for name, text := range map[string]string{
		"metric schema":             schemaPropertyDescription(t, tool, "metric"),
		"period schema":             schemaPropertyDescription(t, tool, "period"),
		"standard metrics guidance": standardMetricsGuidance,
		"semantic layer guidance":   semanticLayerGuidance,
	} {
		t.Run(name, func(t *testing.T) {
			foundRule := false
			for _, paragraph := range regexp.MustCompile(`\n\s*\n`).Split(text, -1) {
				paragraph = strings.Join(strings.Fields(paragraph), " ")
				if !strings.Contains(paragraph, "AT-DATE family") &&
					!strings.Contains(paragraph, "AT-DATE:") &&
					!strings.Contains(paragraph, "at-date families report") {
					continue
				}
				foundRule = true
				if !strings.Contains(paragraph, exception) {
					t.Errorf("at-date rule/list lacks the adjacent maintainer exception: %s", paragraph)
				}
			}
			if !foundRule {
				t.Fatal("no at-date rule found; retain the generic contract and its exception")
			}
		})
	}
}

func TestYearIsDocumentedOnceAsACompatibilityAlias(t *testing.T) {
	text := strings.Join(strings.Fields(standardMetricsGuidance), " ")
	const alias = "by=year is accepted as a compatibility alias of period=year on new_members and membership_churn; it is not a grouping and the family lists do not include it"
	if !strings.Contains(text, alias) {
		t.Error("guidance must distinguish the year compatibility alias from canonical groupings")
	}
	if got := strings.Count(text, "by=year"); got != 1 {
		t.Errorf("by=year occurs %d times; explain the alias once, use period=year in examples", got)
	}
}

func TestMembershipAsOfUsesTheNonNullTermEnd(t *testing.T) {
	text := strings.Join(strings.Fields(semanticLayerGuidance), " ")
	for _, want := range []string{
		"Members as of date D: membership_count with metric_time <= 'D' AND asset_id__end_date >= 'D'",
		"end_date is never NULL; open-ended terms carry a far-future placeholder",
		"never churn_date, which is derived from a different end column and undercounts",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("membership as-of guidance missing %q", want)
		}
	}
	if strings.Contains(text, "asset_id__end_date IS NULL") {
		t.Error("membership as-of guidance still invents NULL-ended terms")
	}
}

// Period is a second grouping axis, not a replacement for by. In particular,
// omitting it cannot turn an organization breakdown into a single figure.
func TestPeriodAddsTimeWithoutReplacingBy(t *testing.T) {
	tool := listStandardMetricsTool(t)
	period := schemaPropertyDescription(t, tool, "period")
	const omission = "Omitted = no time series; the rows are whatever by groups, one figure on by=total"
	if !strings.Contains(period, omission) {
		t.Error("period schema must preserve by when the time grouping is omitted")
	}
	// The only single-figure claim in this field must explicitly be by=total.
	if strings.Contains(strings.ToLower(strings.ReplaceAll(period, omission, "")), "one figure") {
		t.Error("period schema has an unqualified single-figure claim")
	}
	for name, text := range map[string]string{
		"period schema":             period,
		"standard metrics guidance": standardMetricsGuidance,
		"semantic layer guidance":   semanticLayerGuidance,
	} {
		text = strings.Join(strings.Fields(text), " ")
		for _, banned := range []string{"Omitted = one figure", "none = one figure", "instead of one figure"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s still claims %q", name, banned)
			}
		}
		if !strings.Contains(text, "by=org with period=month is one row per organization per month") {
			t.Errorf("%s must explain that period preserves the organization grouping", name)
		}
	}
	if !strings.Contains(standardMetricsGuidance, "| none = no time series |") {
		t.Error("the period default in the contract table must be no time series, not a total")
	}
}
