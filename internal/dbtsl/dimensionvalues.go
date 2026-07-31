// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package dbtsl provides a client for the dbt Semantic Layer API.
package dbtsl

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Dimension value discovery.
//
// A filter naming a real dimension but a wrong literal is not an error: the
// query succeeds and returns zero rows, which reads as "no such data" rather
// than "no such spelling". Observed live: lf_region 'APAC' when the value is
// 'Asia Pacific', and country_name 'Vietnam' when it is 'Viet Nam', each
// costing several wrong-but-successful queries. Listing the values is the only
// fix that generalises past the literals we happen to have seen.

// DimensionValuesMaxLimit caps how many values a single call can return.
const DimensionValuesMaxLimit = 500

// safeDimensionName matches a qualified dimension name.
//
// Qualified names are entity__field, word characters throughout. The name is
// interpolated into a MetricFlow filter expression, so anything else is
// rejected rather than escaped.
var safeDimensionName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// UnknownDimensionError is returned when a dimension is not available to the
// given metrics, or is not a name this client will interpolate.
type UnknownDimensionError struct {
	Message string
}

func (e *UnknownDimensionError) Error() string {
	return e.Message
}

// DimensionValues is the outcome of a dimension value lookup.
type DimensionValues struct {
	Dimension  string   `json:"dimension"`
	Values     []string `json:"values"`
	ValueCount int      `json:"value_count"`
	// Truncated reports that the list hit the limit, so it is a sample rather
	// than the dimension's full domain. Narrow it with a search.
	Truncated bool `json:"truncated"`
}

// escapeSQLLiteral escapes value for use inside a single-quoted SQL string.
func escapeSQLLiteral(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `'`, `''`)
}

// FetchDimensionValues returns the distinct values of a dimension, so a caller
// can write a filter that matches something.
//
// metricNames gates access. A dimension-only query bypasses the metric
// allowlist completely, so without this check any dimension in the semantic
// layer, including PII-bearing ones no allowlisted metric exposes, would be
// dumpable. Callers already hold the metric from list_metrics, so requiring it
// costs nothing.
//
// The query itself passes no metrics: values are then the dimension's full
// domain rather than only those appearing for one metric, and it still returns
// in about a second on a 250-value dimension.
func (c *Client) FetchDimensionValues(ctx context.Context, dimension string, metricNames []string, search string, limit int) (*DimensionValues, error) {
	dimension = strings.TrimSpace(dimension)
	if !safeDimensionName.MatchString(dimension) {
		return nil, &UnknownDimensionError{Message: fmt.Sprintf(
			"Invalid dimension name %q. Expected a qualified_name from get_dimensions, e.g. 'country__lf_region'.",
			dimension,
		)}
	}

	// An empty list has nothing disallowed in it, so it would pass the check
	// below and then ask for dimensions scoped to no metric at all, which the
	// API answers with every dimension in the environment (295 of them here,
	// including ones no allowlisted metric exposes). Require the gate to have
	// something to check before checking it.
	if len(normalizeMetricNames(metricNames)) == 0 {
		return nil, &UnknownDimensionError{Message: "At least one metric is required. Dimension values are checked against the metrics you intend to query; pass the metric from list_metrics."}
	}

	if disallowed := ValidateMetrics(metricNames); len(disallowed) > 0 {
		return nil, &UnknownDimensionError{Message: fmt.Sprintf(
			"Metrics not in allowlist: %s.", strings.Join(disallowed, ", "),
		)}
	}

	available, err := c.FetchDimensions(ctx, metricNames)
	if err != nil {
		return nil, err
	}
	found := false
	for _, d := range available {
		if d.Name == dimension {
			found = true
			break
		}
	}
	if !found {
		return nil, &UnknownDimensionError{Message: fmt.Sprintf(
			"Dimension %q is not available to %s. Use get_dimensions to list them.",
			dimension, strings.Join(metricNames, ", "),
		)}
	}

	if limit < 1 {
		limit = 1
	}
	if limit > DimensionValuesMaxLimit {
		limit = DimensionValuesMaxLimit
	}

	search = strings.TrimSpace(search)
	cacheKey := fmt.Sprintf("%s|%s|%d", dimension, search, limit)
	if cached, ok := c.dimensionValuesCache.get(cacheKey); ok {
		return newDimensionValues(dimension, cached, limit), nil
	}

	args := QueryArgs{
		Metrics: nil,
		GroupBy: []string{dimension},
		Limit:   limit,
	}
	if search != "" {
		args.Where = []string{fmt.Sprintf(
			"{{ Dimension('%s') }} ILIKE '%%%s%%'", dimension, escapeSQLLiteral(search),
		)}
	}

	result, err := c.Query(ctx, args)
	if err != nil {
		return nil, err
	}

	values := distinctValues(result, dimension)
	c.dimensionValuesCache.put(cacheKey, values)
	return newDimensionValues(dimension, values, limit), nil
}

// distinctValues pulls the sorted, deduplicated, non-null values out of a
// dimension-only query result.
//
// NULL is not a filterable literal, so it is noise here.
func distinctValues(result *QueryResult, dimension string) []string {
	column := dimension
	if len(result.Columns) > 0 {
		column = result.Columns[0]
	}

	seen := make(map[string]struct{}, len(result.Data))
	values := make([]string, 0, len(result.Data))
	for _, row := range result.Data {
		raw, ok := row[column]
		if !ok || raw == nil {
			continue
		}
		value := fmt.Sprintf("%v", raw)
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func newDimensionValues(dimension string, values []string, limit int) *DimensionValues {
	return &DimensionValues{
		Dimension:  dimension,
		Values:     values,
		ValueCount: len(values),
		Truncated:  len(values) >= limit,
	}
}

// NoDimensionValuesDetail is the message for a lookup that matched nothing.
//
// The stored spelling is often not the everyday one, so the caller is pointed
// at that rather than left to conclude the data is empty.
func NoDimensionValuesDetail(dimension, search string) string {
	if search == "" {
		return fmt.Sprintf("Dimension %q has no non-null values.", dimension)
	}
	return fmt.Sprintf(
		"No values of %q matched %q. The search is a plain substring, so try a "+
			"shorter fragment - country names use their ISO spelling "+
			"('Viet Nam', 'Korea, Republic of'), which often differs from the "+
			"everyday one.",
		dimension, search,
	)
}
