// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package dbtsl provides a client for the dbt Semantic Layer API.
package dbtsl

import "strings"

// singularVariants returns word plus a naive singular form.
//
// Metric names and descriptions are written in the singular ("contribution",
// "membership"), so a caller searching the plural matches nothing, and a
// plural is the natural thing to type for a domain. Substring matching cannot
// rescue it either, since the plural is the longer string.
func singularVariants(word string) []string {
	variants := []string{word}
	for _, rule := range []struct{ suffix, stem string }{
		{"ies", "y"},
		{"ses", "s"},
		{"s", ""},
	} {
		if strings.HasSuffix(word, rule.suffix) && len(word)-len(rule.suffix) >= 3 {
			variants = append(variants, strings.TrimSuffix(word, rule.suffix)+rule.stem)
			break
		}
	}
	return variants
}

// matchAnyWord returns the allowlisted metrics whose name or description
// contains any word of search, or that word's singular form.
func matchAnyWord(metrics []MetricInfo, search string) []MetricInfo {
	var words []string
	for _, token := range strings.Fields(search) {
		w := strings.ToLower(strings.TrimSpace(token))
		if w == "" {
			continue
		}
		words = append(words, singularVariants(w)...)
	}
	if len(words) == 0 {
		return nil
	}

	var matched []MetricInfo
	for _, m := range metrics {
		if !IsAllowedMetric(m.Name) {
			continue
		}
		haystack := strings.ToLower(m.Name + " " + m.Description)
		for _, w := range words {
			if strings.Contains(haystack, w) {
				matched = append(matched, m)
				break
			}
		}
	}
	return matched
}
