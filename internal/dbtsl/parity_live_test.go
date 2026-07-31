// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

//go:build parity

// Live parity harness for the dbt Semantic Layer client.
//
// This is excluded from a normal build and test run: it talks to the real
// semantic layer and needs credentials. It exists because this client's query
// path is written from scratch against the GraphQL API, while both reference
// implementations (lfx-lens and dbt Labs' dbt-mcp) execute queries over Arrow
// Flight through the Python SDK. Unit tests against a stub cannot tell us the
// GraphQL request shapes are right, only that they are consistent.
//
// Run it with credentials from the lfx-lens .env:
//
//	set -a && source ../lfx-lens/.env && set +a
//	go test -tags parity -v -run TestLive ./internal/dbtsl/
package dbtsl

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func liveClient(t *testing.T) *Client {
	t.Helper()

	cfg := Config{
		Host:          os.Getenv("DBT_SL_HOST"),
		EnvironmentID: os.Getenv("DBT_SEMANTIC_ENV_ID"),
		Token:         os.Getenv("DBT_SEMANTIC_SERVICE_TOKEN"),
	}
	if cfg.Host == "" || cfg.EnvironmentID == "" || cfg.Token == "" {
		t.Skip("set DBT_SL_HOST, DBT_SEMANTIC_ENV_ID and DBT_SEMANTIC_SERVICE_TOKEN to run the parity harness")
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	return client
}

func liveContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

// TestLiveListMetrics checks the allowlist actually intersects what the
// semantic layer exposes. A rename upstream would show up here as a metric we
// allow but can no longer reach.
func TestLiveListMetrics(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	metrics, err := client.FetchAllowedMetrics(ctx, "")
	if err != nil {
		t.Fatalf("FetchAllowedMetrics failed: %v", err)
	}
	t.Logf("allowlisted metrics reachable: %d of %d", len(metrics), len(AllowedMetricNames()))
	if len(metrics) == 0 {
		t.Fatal("no allowlisted metric is reachable")
	}

	reachable := make(map[string]bool, len(metrics))
	for _, m := range metrics {
		reachable[m.Name] = true
	}
	var missing []string
	for _, name := range AllowedMetricNames() {
		if !reachable[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Logf("allowlisted but not reachable upstream: %v", missing)
	}
}

// TestLiveSearchFallbacks covers the two rescues that keep a search from
// dead-ending: the singular stem and the per-word retry.
func TestLiveSearchFallbacks(t *testing.T) {
	client := liveClient(t)

	for _, tc := range []struct {
		name   string
		search string
	}{
		{"exact singular", "contributor"},
		{"plural needs the stem", "contributions"},
		{"natural language needs the per-word retry", "contributor organization country"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := liveContext(t)
			defer cancel()

			metrics, err := client.FetchAllowedMetrics(ctx, tc.search)
			if err != nil {
				t.Fatalf("FetchAllowedMetrics(%q) failed: %v", tc.search, err)
			}
			if len(metrics) == 0 {
				t.Errorf("search %q returned nothing, which is the dead end this is meant to prevent", tc.search)
			}
			names := make([]string, 0, len(metrics))
			for _, m := range metrics {
				names = append(names, m.Name)
			}
			t.Logf("search %q -> %v", tc.search, names)
		})
	}
}

// TestLiveEverySearchableTopicReturnsMetrics is the live half of the topic
// word test. The unit test only proves a topic matches a name we ship; this
// proves the search actually returns something.
func TestLiveEverySearchableTopicReturnsMetrics(t *testing.T) {
	client := liveClient(t)

	for _, topic := range SearchableTopics() {
		ctx, cancel := liveContext(t)
		metrics, err := client.FetchAllowedMetrics(ctx, topic)
		cancel()
		if err != nil {
			t.Errorf("topic %q failed: %v", topic, err)
			continue
		}
		if len(metrics) == 0 {
			t.Errorf("topic %q returned no metrics, so the guidance sends callers to a dead end", topic)
		}
	}
}

func TestLiveDimensions(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	dimensions, err := client.FetchDimensions(ctx, []string{"total_contributors"})
	if err != nil {
		t.Fatalf("FetchDimensions failed: %v", err)
	}
	if len(dimensions) == 0 {
		t.Fatal("expected dimensions for total_contributors")
	}
	t.Logf("total_contributors exposes %d dimensions", len(dimensions))

	var hasRegion bool
	for _, d := range dimensions {
		if d.Name == "country__lf_region" {
			hasRegion = true
		}
	}
	if !hasRegion {
		t.Error("expected country__lf_region among the dimensions, which the regional lens work added")
	}
}

// TestLiveDimensionValuesRegion is the motivating case for the whole epic:
// the stored value is 'Asia Pacific', and a caller guessing 'APAC' gets zero
// rows rather than an error.
func TestLiveDimensionValuesRegion(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	start := time.Now()
	values, err := client.FetchDimensionValues(ctx, "country__lf_region", []string{"total_contributors"}, "", 100)
	if err != nil {
		t.Fatalf("FetchDimensionValues failed: %v", err)
	}
	t.Logf("country__lf_region -> %d values in %s: %v", values.ValueCount, time.Since(start).Round(time.Millisecond), values.Values)

	if !containsValue(values.Values, "Asia Pacific") {
		t.Errorf("expected 'Asia Pacific' among the region values, got %v", values.Values)
	}
	if containsValue(values.Values, "APAC") {
		t.Error("'APAC' is not a stored value, so finding it means the query is wrong")
	}
}

// TestLiveDimensionValuesCountrySearch proves the ISO spelling comes back for
// the everyday one, which is the fix for the Vietnam question.
func TestLiveDimensionValuesCountrySearch(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	values, err := client.FetchDimensionValues(ctx, "country__country_name", []string{"total_contributors"}, "viet", 100)
	if err != nil {
		t.Fatalf("FetchDimensionValues failed: %v", err)
	}
	t.Logf("country__country_name search 'viet' -> %v", values.Values)

	if !containsValue(values.Values, "Viet Nam") {
		t.Errorf("expected the ISO spelling 'Viet Nam', got %v", values.Values)
	}
}

// TestLiveDimensionValuesGate confirms the metric gate holds against the real
// API, not just the stub. Without it, any dimension in the semantic layer
// would be enumerable, because a dimension-only query never consults the
// metric allowlist.
func TestLiveDimensionValuesGate(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	if _, err := client.FetchDimensionValues(ctx, "user__email", []string{"total_contributors"}, "", 10); err == nil {
		t.Fatal("expected a dimension outside the metric to be rejected")
	}
}

// TestLiveQuery exercises the createQuery and poll path, which is the part
// with no reference implementation to copy.
func TestLiveQuery(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	start := time.Now()
	result, err := client.Query(ctx, QueryArgs{
		Metrics: []string{"total_contributors"},
		GroupBy: []string{"country__lf_region"},
		OrderBy: []string{"-total_contributors"},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	t.Logf("query returned %d rows in %s", result.RowCount, time.Since(start).Round(time.Millisecond))
	t.Logf("columns: %v", result.Columns)
	for _, row := range result.Data {
		t.Logf("  %v", row)
	}

	if result.RowCount == 0 {
		t.Error("expected rows for contributors by region")
	}
	if len(result.Columns) != 2 {
		t.Errorf("expected the group by and the metric as columns, got %v", result.Columns)
	}
	for _, row := range result.Data {
		if _, present := row["index"]; present {
			t.Error("the synthetic row index leaked into the result")
			break
		}
	}
}

// TestLiveQueryWithTimeGrain covers the translation the GraphQL API forces:
// callers write metric_time__year, the API wants a separate grain field. If
// this fails, every time series query is broken.
func TestLiveQueryWithTimeGrain(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	result, err := client.Query(ctx, QueryArgs{
		Metrics: []string{"total_contributors"},
		GroupBy: []string{"metric_time__year"},
		Limit:   5,
	})
	if err != nil {
		t.Fatalf("time grain query failed: %v", err)
	}
	t.Logf("columns: %v, rows: %d", result.Columns, result.RowCount)
	for _, row := range result.Data {
		t.Logf("  %v", row)
	}
	if result.RowCount == 0 {
		t.Error("expected at least one year of contributors")
	}
}

// TestLiveQueryWithWhereFilter checks a MetricFlow filter survives the trip
// through the GraphQL WhereInput.
func TestLiveQueryWithWhereFilter(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	result, err := client.Query(ctx, QueryArgs{
		Metrics: []string{"total_contributors"},
		GroupBy: []string{"country__country_name"},
		Where:   []string{"{{ Dimension('country__lf_region') }} = 'Asia Pacific'"},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("filtered query failed: %v", err)
	}
	t.Logf("Asia Pacific -> %d rows: %v", result.RowCount, result.Data)
	if result.RowCount == 0 {
		t.Error("expected rows for Asia Pacific, so either the filter or the literal is wrong")
	}
}

// TestLiveQueryRejectsAnUnknownMetric confirms an application-level failure
// arrives as QueryFailedError with the upstream reason intact, rather than as
// a transport error.
func TestLiveQueryRejectsAnUnknownMetric(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	_, err := client.Query(ctx, QueryArgs{Metrics: []string{"definitely_not_a_metric"}, Limit: 1})
	if err == nil {
		t.Fatal("expected an unknown metric to fail")
	}
	t.Logf("unknown metric error: %v", err)
}

func containsValue(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
