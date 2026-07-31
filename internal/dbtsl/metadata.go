// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package dbtsl provides a client for the dbt Semantic Layer API.
package dbtsl

import (
	"context"
	"sort"
	"strings"
)

// inlineDimensionsThreshold is the largest metric count for which dimensions
// are inlined into the metric list. Above it the caller is expected to narrow
// the search first, then ask for dimensions explicitly.
const inlineDimensionsThreshold = 15

const gqlMetrics = `
query GetMetrics($environmentId: BigInt!, $search: String, $pageNum: Int!) {
  metricsPaginated(environmentId: $environmentId, search: $search, pageNum: $pageNum) {
    totalPages
    items {
      name
      label
      description
      type
    }
  }
}
`

const gqlMetricsWithRelated = `
query GetMetricsWithRelated($environmentId: BigInt!, $search: String, $pageNum: Int!) {
  metricsPaginated(environmentId: $environmentId, search: $search, pageNum: $pageNum) {
    totalPages
    items {
      name
      label
      description
      type
      dimensions {
        name
      }
      entities {
        name
      }
    }
  }
}
`

const gqlDimensions = `
query GetDimensions($environmentId: BigInt!, $metrics: [MetricInput!]!, $pageNum: Int!) {
  dimensionsPaginated(environmentId: $environmentId, metrics: $metrics, pageNum: $pageNum) {
    totalPages
    items {
      name
      type
      description
      label
      queryableGranularities
    }
  }
}
`

// MetricInfo describes one metric in the semantic layer.
type MetricInfo struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type"`
	Dimensions  []string `json:"dimensions,omitempty"`
	Entities    []string `json:"entities,omitempty"`
}

// DimensionInfo describes one dimension available to a set of metrics.
type DimensionInfo struct {
	// Name is the qualified name, e.g. "country__lf_region".
	Name                       string   `json:"name"`
	Type                       string   `json:"type"`
	Description                string   `json:"description,omitempty"`
	Label                      string   `json:"label,omitempty"`
	QueryableTimeGranularities []string `json:"queryable_time_granularities"`
}

// metricsResponse decodes the metricsPaginated GraphQL shape.
type metricsResponse struct {
	MetricsPaginated struct {
		Items []struct {
			Name        string `json:"name"`
			Label       string `json:"label"`
			Description string `json:"description"`
			Type        string `json:"type"`
			Dimensions  []struct {
				Name string `json:"name"`
			} `json:"dimensions"`
			Entities []struct {
				Name string `json:"name"`
			} `json:"entities"`
		} `json:"items"`
		TotalPages int `json:"totalPages"`
	} `json:"metricsPaginated"`
}

// dimensionsResponse decodes the dimensionsPaginated GraphQL shape.
type dimensionsResponse struct {
	DimensionsPaginated struct {
		Items []struct {
			Name                   string   `json:"name"`
			Type                   string   `json:"type"`
			Description            string   `json:"description"`
			Label                  string   `json:"label"`
			QueryableGranularities []string `json:"queryableGranularities"`
		} `json:"items"`
		TotalPages int `json:"totalPages"`
	} `json:"dimensionsPaginated"`
}

// FetchAllowedMetrics returns allowlist-filtered metrics, with dimensions
// inlined when the result is small enough to be worth it.
//
// Only searched results are cached. The no-search case is a single cheap call
// that returns every metric without dimensions.
func (c *Client) FetchAllowedMetrics(ctx context.Context, search string) ([]MetricInfo, error) {
	search = strings.TrimSpace(search)

	if search != "" {
		if cached, ok := c.metricsCache.get(search); ok {
			return cached, nil
		}
	}

	all, err := c.fetchMetricsRaw(ctx, search, false)
	if err != nil {
		return nil, err
	}
	allowed := filterAllowed(all)
	matchedByWord := false

	// The Semantic Layer matches search as a single phrase and only in the
	// exact form given, so both a natural-language term like "contributor
	// organization country" and a bare plural like "contributions" come back
	// empty even though "contributor" alone finds two metrics. Rather than
	// leave the caller at a dead end, retry once matching any single word, or
	// its singular, against the metric name and description. This only runs
	// when the first search came back empty, so it cannot change a result that
	// already worked.
	if len(allowed) == 0 && search != "" {
		every, err := c.fetchMetricsRaw(ctx, "", false)
		if err != nil {
			return nil, err
		}
		allowed = matchAnyWord(every, search)
		matchedByWord = true
	}

	if len(allowed) > 0 && len(allowed) <= inlineDimensionsThreshold {
		// Re-fetch with dimensions. The per-word path must not send search
		// upstream, since it already returned nothing, so fetch everything and
		// re-apply the same word matching.
		if matchedByWord {
			every, err := c.fetchMetricsRaw(ctx, "", true)
			if err != nil {
				return nil, err
			}
			allowed = matchAnyWord(every, search)
		} else {
			withDims, err := c.fetchMetricsRaw(ctx, search, true)
			if err != nil {
				return nil, err
			}
			allowed = filterAllowed(withDims)
		}
	}

	if search != "" {
		c.metricsCache.put(search, allowed)
	}
	return allowed, nil
}

func filterAllowed(metrics []MetricInfo) []MetricInfo {
	var allowed []MetricInfo
	for _, m := range metrics {
		if IsAllowedMetric(m.Name) {
			allowed = append(allowed, m)
		}
	}
	return allowed
}

// fetchMetricsRaw fetches metrics from the semantic layer, uncached and
// unfiltered. When includeDimensions is set each metric carries its available
// dimension names, at the cost of an extra GraphQL field.
func (c *Client) fetchMetricsRaw(ctx context.Context, search string, includeDimensions bool) ([]MetricInfo, error) {
	query := gqlMetrics
	if includeDimensions {
		query = gqlMetricsWithRelated
	}

	var metrics []MetricInfo

	// Metadata is paginated on the same terms as query results. One page holds
	// every metric in this environment today, so this loop runs once, but a
	// partial metric list is a partial allowlist: metrics that exist would
	// report as unknown, and the caller would be told to search a topic that
	// then returns nothing. Follow the pages rather than assume one.
	for page, totalPages := 1, 1; page <= totalPages; page++ {
		variables := map[string]any{"pageNum": page}
		if search != "" {
			variables["search"] = search
		}

		var resp metricsResponse
		if err := c.graphqlRequest(ctx, query, variables, &resp); err != nil {
			return nil, err
		}
		if resp.MetricsPaginated.TotalPages > totalPages {
			totalPages = resp.MetricsPaginated.TotalPages
		}

		for _, item := range resp.MetricsPaginated.Items {
			metric := MetricInfo{
				Name:        item.Name,
				Label:       item.Label,
				Description: item.Description,
				Type:        item.Type,
			}
			for _, d := range item.Dimensions {
				metric.Dimensions = append(metric.Dimensions, d.Name)
			}
			for _, e := range item.Entities {
				metric.Entities = append(metric.Entities, e.Name)
			}
			metrics = append(metrics, metric)
		}
	}
	return metrics, nil
}

// FetchDimensions returns the dimensions available to a set of metrics.
//
// Results are cached under the deduplicated, sorted metric names.
func (c *Client) FetchDimensions(ctx context.Context, metricNames []string) ([]DimensionInfo, error) {
	normalized := normalizeMetricNames(metricNames)
	cacheKey := strings.Join(normalized, ",")
	if cached, ok := c.dimensionsCache.get(cacheKey); ok {
		return cached, nil
	}

	metricInputs := make([]map[string]string, 0, len(normalized))
	for _, name := range normalized {
		metricInputs = append(metricInputs, map[string]string{"name": name})
	}

	// Paginated for the same reason as the metric list: a dimension missing
	// from this result is one the caller is told does not exist, and
	// FetchDimensionValues uses exactly this list to decide what may be read.
	var dimensions []DimensionInfo
	for page, totalPages := 1, 1; page <= totalPages; page++ {
		variables := map[string]any{"metrics": metricInputs, "pageNum": page}

		var resp dimensionsResponse
		if err := c.graphqlRequest(ctx, gqlDimensions, variables, &resp); err != nil {
			return nil, err
		}
		if resp.DimensionsPaginated.TotalPages > totalPages {
			totalPages = resp.DimensionsPaginated.TotalPages
		}

		for _, item := range resp.DimensionsPaginated.Items {
			granularities := item.QueryableGranularities
			if granularities == nil {
				granularities = []string{}
			}
			dimensions = append(dimensions, DimensionInfo{
				Name:                       item.Name,
				Type:                       item.Type,
				Description:                item.Description,
				Label:                      item.Label,
				QueryableTimeGranularities: granularities,
			})
		}
	}

	c.dimensionsCache.put(cacheKey, dimensions)
	return dimensions, nil
}

// normalizeMetricNames trims, drops empties, deduplicates and sorts.
func normalizeMetricNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	var normalized []string
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	sort.Strings(normalized)
	return normalized
}
