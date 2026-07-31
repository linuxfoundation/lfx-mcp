// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package dbtsl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The published GraphQL docs name this argument "order", but the deployed
// schema calls it "orderBy". Introspection is the source of truth here.
const gqlCreateQuery = `
mutation CreateQuery($environmentId: BigInt!, $metrics: [MetricInput!], $groupBy: [GroupByInput!], $where: [WhereInput!], $orderBy: [OrderByInput!], $limit: Int) {
  createQuery(environmentId: $environmentId, metrics: $metrics, groupBy: $groupBy, where: $where, orderBy: $orderBy, limit: $limit) {
    queryId
  }
}
`

const gqlQueryResult = `
query GetQueryResult($environmentId: BigInt!, $queryId: String!) {
  query(environmentId: $environmentId, queryId: $queryId) {
    status
    error
    sql
    jsonResult(encoded: false, orient: TABLE)
  }
}
`

// Query polling cadence. The Semantic Layer compiles and runs warehouse SQL,
// so the first result is rarely ready immediately. The interval backs off so a
// slow query does not generate a poll storm, and the overall bound comes from
// the caller's context.
const (
	pollInitialInterval = 250 * time.Millisecond
	pollMaxInterval     = 2 * time.Second
	pollBackoffFactor   = 1.5
)

// Query status values returned by the Semantic Layer. Anything else means the
// query is still in flight.
const (
	statusSuccessful = "SUCCESSFUL"
	statusFailed     = "FAILED"
)

// timeGranularities are the TimeGranularity enum values a group_by name may
// carry as a trailing __suffix.
//
// Callers write time grains the way the dbt SDK accepts them,
// "metric_time__month", but the GraphQL API takes the grain as a separate
// field, {name: "metric_time", grain: MONTH}. Splitting on a known
// granularity is what bridges the two. A suffix that is not a granularity,
// such as the "lf_region" in "country__lf_region", is left alone.
var timeGranularities = map[string]string{
	"nanosecond":  "NANOSECOND",
	"microsecond": "MICROSECOND",
	"millisecond": "MILLISECOND",
	"second":      "SECOND",
	"minute":      "MINUTE",
	"hour":        "HOUR",
	"day":         "DAY",
	"week":        "WEEK",
	"month":       "MONTH",
	"quarter":     "QUARTER",
	"year":        "YEAR",
}

// QueryArgs describes a metric query.
type QueryArgs struct {
	// Metrics may be empty, which asks for a dimension's own domain rather
	// than the values co-occurring with some metric.
	Metrics []string
	GroupBy []string
	// Where holds MetricFlow filter expressions, e.g.
	// "{{ Dimension('country__lf_region') }} = 'Asia Pacific'".
	Where []string
	// OrderBy holds field names, prefixed with "-" for descending.
	OrderBy []string
	Limit   int
}

// QueryResult is a completed query's tabular result.
type QueryResult struct {
	Columns  []string         `json:"columns"`
	Data     []map[string]any `json:"data"`
	RowCount int              `json:"row_count"`
	// CompiledSQL is the warehouse SQL the Semantic Layer generated, when the
	// API returned it.
	CompiledSQL string `json:"compiled_sql,omitempty"`
}

// QueryFailedError is returned when the Semantic Layer rejects or fails a
// query. It is an application-level error, not a transport failure, so the
// caller should surface the message rather than retry.
type QueryFailedError struct {
	Message string
}

func (e *QueryFailedError) Error() string {
	return e.Message
}

type createQueryResponse struct {
	CreateQuery struct {
		QueryID string `json:"queryId"`
	} `json:"createQuery"`
}

type queryResultResponse struct {
	Query struct {
		Status     string `json:"status"`
		Error      string `json:"error"`
		SQL        string `json:"sql"`
		JSONResult string `json:"jsonResult"`
	} `json:"query"`
}

// Query runs a metric query and returns its rows.
//
// It submits the query, then polls until the Semantic Layer reports it
// successful or failed. Results come back as JSON rather than Arrow, which
// keeps this client free of an Arrow and gRPC dependency.
func (c *Client) Query(ctx context.Context, args QueryArgs) (*QueryResult, error) {
	queryID, err := c.createQuery(ctx, args)
	if err != nil {
		return nil, err
	}

	interval := pollInitialInterval
	for {
		result, err := c.pollQuery(ctx, queryID)
		if err != nil {
			return nil, err
		}

		switch result.Query.Status {
		case statusSuccessful:
			return parseQueryResult(result.Query.JSONResult, result.Query.SQL)
		case statusFailed:
			message := strings.TrimSpace(result.Query.Error)
			if message == "" {
				message = "the semantic layer reported the query failed but gave no reason"
			}
			return nil, &QueryFailedError{Message: message}
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("semantic layer query did not finish in time: %w", ctx.Err())
		case <-time.After(interval):
		}

		if interval = time.Duration(float64(interval) * pollBackoffFactor); interval > pollMaxInterval {
			interval = pollMaxInterval
		}
	}
}

func (c *Client) createQuery(ctx context.Context, args QueryArgs) (string, error) {
	metricInputs := make([]map[string]any, 0, len(args.Metrics))
	for _, name := range args.Metrics {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			metricInputs = append(metricInputs, map[string]any{"name": trimmed})
		}
	}

	variables := map[string]any{"metrics": metricInputs}

	if groupBy := buildGroupByInputs(args.GroupBy); len(groupBy) > 0 {
		variables["groupBy"] = groupBy
	}
	if where := buildWhereInputs(args.Where); len(where) > 0 {
		variables["where"] = where
	}
	if order := buildOrderByInputs(args.OrderBy, args.Metrics); len(order) > 0 {
		variables["orderBy"] = order
	}
	if args.Limit > 0 {
		variables["limit"] = args.Limit
	}

	var resp createQueryResponse
	if err := c.graphqlRequest(ctx, gqlCreateQuery, variables, &resp); err != nil {
		return "", err
	}
	if resp.CreateQuery.QueryID == "" {
		return "", fmt.Errorf("semantic layer accepted the query but returned no query ID")
	}
	return resp.CreateQuery.QueryID, nil
}

func (c *Client) pollQuery(ctx context.Context, queryID string) (*queryResultResponse, error) {
	var resp queryResultResponse
	if err := c.graphqlRequest(ctx, gqlQueryResult, map[string]any{"queryId": queryID}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// buildGroupByInputs converts group_by names into GroupByInput values,
// splitting a trailing time granularity into its own field.
func buildGroupByInputs(names []string) []map[string]any {
	inputs := make([]map[string]any, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		input := map[string]any{"name": name}
		if base, grain, ok := splitTimeGrain(name); ok {
			input["name"] = base
			input["grain"] = grain
		}
		inputs = append(inputs, input)
	}
	return inputs
}

// splitTimeGrain splits "metric_time__month" into "metric_time" and "MONTH".
// It reports false when the trailing segment is not a time granularity.
func splitTimeGrain(name string) (base, grain string, ok bool) {
	idx := strings.LastIndex(name, "__")
	if idx <= 0 {
		return "", "", false
	}
	suffix := strings.ToLower(name[idx+2:])
	grain, isGrain := timeGranularities[suffix]
	if !isGrain {
		return "", "", false
	}
	return name[:idx], grain, true
}

func buildWhereInputs(clauses []string) []map[string]any {
	inputs := make([]map[string]any, 0, len(clauses))
	for _, clause := range clauses {
		if trimmed := strings.TrimSpace(clause); trimmed != "" {
			inputs = append(inputs, map[string]any{"sql": trimmed})
		}
	}
	return inputs
}

// buildOrderByInputs converts order_by names into OrderByInput values.
//
// A name is ordered as a metric when it is one of the query's metrics, and as
// a group_by otherwise, which is how the dbt SDK resolves the same ambiguity.
// A leading "-" means descending.
func buildOrderByInputs(names, metrics []string) []map[string]any {
	metricSet := make(map[string]struct{}, len(metrics))
	for _, m := range metrics {
		metricSet[strings.TrimSpace(m)] = struct{}{}
	}

	inputs := make([]map[string]any, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		descending := strings.HasPrefix(name, "-")
		name = strings.TrimPrefix(name, "-")
		if name == "" {
			continue
		}

		input := map[string]any{"descending": descending}
		if _, isMetric := metricSet[name]; isMetric {
			input["metric"] = map[string]any{"name": name}
		} else {
			groupBy := map[string]any{"name": name}
			if base, grain, ok := splitTimeGrain(name); ok {
				groupBy["name"] = base
				groupBy["grain"] = grain
			}
			input["groupBy"] = groupBy
		}
		inputs = append(inputs, input)
	}
	return inputs
}

// tableResult is the pandas "table" JSON orientation the API returns.
type tableResult struct {
	Schema struct {
		Fields []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
		PrimaryKey []string `json:"primaryKey"`
	} `json:"schema"`
	Data []map[string]any `json:"data"`
}

// parseQueryResult turns the API's JSON result into columns and rows.
//
// The "table" orientation carries a synthetic row index in both the schema and
// every row. It is an artifact of the serialisation rather than query output,
// so it is stripped.
func parseQueryResult(raw, compiledSQL string) (*QueryResult, error) {
	if strings.TrimSpace(raw) == "" {
		return &QueryResult{Columns: []string{}, Data: []map[string]any{}, CompiledSQL: compiledSQL}, nil
	}

	// Decode numbers as json.Number rather than float64. Metric values are
	// often large counts, and float64 round-trips 4239559 back out as
	// 4.239559e+06, which is what the model would then read.
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	var table tableResult
	if err := decoder.Decode(&table); err != nil {
		return nil, fmt.Errorf("failed to decode semantic layer query result: %w", err)
	}

	indexFields := make(map[string]struct{}, len(table.Schema.PrimaryKey))
	for _, key := range table.Schema.PrimaryKey {
		if key == "index" {
			indexFields[key] = struct{}{}
		}
	}

	columns := make([]string, 0, len(table.Schema.Fields))
	for _, field := range table.Schema.Fields {
		if _, skip := indexFields[field.Name]; skip {
			continue
		}
		columns = append(columns, field.Name)
	}

	rows := make([]map[string]any, 0, len(table.Data))
	for _, row := range table.Data {
		for name := range indexFields {
			delete(row, name)
		}
		rows = append(rows, row)
	}

	return &QueryResult{
		Columns:     columns,
		Data:        rows,
		RowCount:    len(rows),
		CompiledSQL: compiledSQL,
	}, nil
}
