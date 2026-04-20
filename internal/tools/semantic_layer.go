// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// Tool registration
// ---------------------------------------------------------------------------

// RegisterListMetrics registers the list_metrics tool.
func RegisterListMetrics(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_metrics_lfx_lens",
		Description: `List available LFX Insights metrics from the dbt Semantic Layer.

Data domains: memberships & revenue, code activities & contributions, maintainers, project health scores, project metadata, events & sponsorships, surveys & NPS, and education & certifications.

Use this as a first step to discover which metrics are available before querying data. Optionally filter by a search term matched against metric names and descriptions (e.g. "revenue", "contributors", "enrollment").`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Semantic Layer Metrics",
			ReadOnlyHint: true,
		},
	}, handleListMetrics)
}

// RegisterGetDimensions registers the get_dimensions tool.
func RegisterGetDimensions(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_dimensions_lfx_lens",
		Description: `Get the dimensions available for one or more LFX Insights metrics.

Dimensions are attributes you can group by or filter on when querying metrics — for example project_slug, metric_time, account_name, or tier_name. Available dimensions vary by data domain (e.g. membership metrics have tier_name, activity metrics have repository_url).

Always call this before query_metrics_lfx_lens to discover valid group_by and where fields. The qualified_name in the response is the exact string to use in group_by and where clauses.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Metric Dimensions",
			ReadOnlyHint: true,
		},
	}, handleGetDimensions)
}

// RegisterGetEntities registers the get_entities tool.
func RegisterGetEntities(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_entities_lfx_lens",
		Description: `Get the entities available for one or more LFX Insights metrics.

Entities represent real-world concepts (projects, users, memberships, enrollments) that link dimensions across semantic models. Each data domain has its own entity (e.g. asset_id for activities, enrollment_id for education). Use this to understand which entity prefixes to use in dimension qualified names.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Metric Entities",
			ReadOnlyHint: true,
		},
	}, handleGetEntities)
}

// RegisterListSavedQueries registers the list_saved_queries tool.
func RegisterListSavedQueries(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_saved_queries_lfx_lens",
		Description: `List pre-defined saved queries from the LFX Insights dbt Semantic Layer.

Saved queries are curated metric + dimension + filter combinations maintained by the data team, spanning all data domains (memberships, activities, maintainers, health, events, surveys, education). If one matches the user's question, prefer it over building a query from scratch — it will be faster and the results are guaranteed to be correct.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Saved Queries",
			ReadOnlyHint: true,
		},
	}, handleListSavedQueries)
}

// RegisterQueryMetrics registers the query_metrics tool.
func RegisterQueryMetrics(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "query_metrics_lfx_lens",
		Description: `Execute a structured metric query against the LFX Insights dbt Semantic Layer.

Data domains: memberships & revenue, code activities & contributions, maintainers, project health scores, project metadata, events & sponsorships, surveys & NPS, and education & certifications.

This is the preferred way to get quantitative LFX data (counts, revenue, scores, etc.) when you know which metrics and dimensions to use. It returns deterministic, pre-aggregated results much faster than ad-hoc SQL.

Workflow:
1. Call list_metrics_lfx_lens to find relevant metrics
2. Call get_dimensions_lfx_lens to find valid group_by/filter fields
3. Call this tool with the metric names, group_by dimensions, and optional filters

Use search_projects first to get the project slug, then filter with a where clause like: "{{ Dimension('entity__project_slug') }} = 'cncf'"

For ad-hoc or exploratory questions that don't map cleanly to a metric, use query_lfx_lens instead.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Query Semantic Layer Metrics",
			ReadOnlyHint: true,
		},
	}, handleQueryMetrics)
}

// ---------------------------------------------------------------------------
// Tool args
// ---------------------------------------------------------------------------

// ListMetricsArgs defines the input for list_metrics.
type ListMetricsArgs struct {
	Search string `json:"search,omitempty" jsonschema:"Optional search term to filter metrics by name or description (case-insensitive)"`
}

// GetDimensionsArgs defines the input for get_dimensions.
type GetDimensionsArgs struct {
	Metrics []string `json:"metrics" jsonschema:"Metric names to get dimensions for (required)"`
	Search  string   `json:"search,omitempty" jsonschema:"Optional search term to filter dimensions by name or description"`
}

// GetEntitiesArgs defines the input for get_entities.
type GetEntitiesArgs struct {
	Metrics []string `json:"metrics" jsonschema:"Metric names to get entities for (required)"`
	Search  string   `json:"search,omitempty" jsonschema:"Optional search term to filter entities by name or description"`
}

// ListSavedQueriesArgs defines the input for list_saved_queries (no args needed).
type ListSavedQueriesArgs struct{}

// QueryMetricsArgs defines the input for query_metrics.
type QueryMetricsArgs struct {
	Metrics []string `json:"metrics" jsonschema:"Metric names to query (required, at least one)"`
	GroupBy []string `json:"group_by,omitempty" jsonschema:"Dimension qualified_names to group by (from get_dimensions)"`
	Where   []string `json:"where,omitempty" jsonschema:"MetricFlow filter expressions, e.g. {{ Dimension('entity__col') }} = 'value'"`
	OrderBy []string `json:"order_by,omitempty" jsonschema:"Fields to order by (prefix with - for descending)"`
	Limit   int      `json:"limit,omitempty" jsonschema:"Maximum rows to return (1-500)"`
}

// ---------------------------------------------------------------------------
// Tool handlers
// ---------------------------------------------------------------------------

func handleListMetrics(ctx context.Context, _ *mcp.CallToolRequest, args ListMetricsArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	params := url.Values{}
	if args.Search != "" {
		params.Set("search", args.Search)
	}

	body, statusCode, err := lensConfig.ServiceClient.Get(ctx, "/lfx-lens/semantic-layer/metrics", params)
	if err != nil {
		return nil, nil, fmt.Errorf("list_metrics API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))}},
			IsError: true,
		}, nil, nil
	}

	// Pretty-print the JSON response.
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
	pretty, _ := json.MarshalIndent(raw, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(pretty)}},
	}, nil, nil
}

func handleGetDimensions(ctx context.Context, _ *mcp.CallToolRequest, args GetDimensionsArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	if len(args.Metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: at least one metric name is required"}},
			IsError: true,
		}, nil, nil
	}

	params := url.Values{}
	params.Set("metrics", strings.Join(args.Metrics, ","))
	if args.Search != "" {
		params.Set("search", args.Search)
	}

	body, statusCode, err := lensConfig.ServiceClient.Get(ctx, "/lfx-lens/semantic-layer/dimensions", params)
	if err != nil {
		return nil, nil, fmt.Errorf("get_dimensions API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))}},
			IsError: true,
		}, nil, nil
	}

	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
	pretty, _ := json.MarshalIndent(raw, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(pretty)}},
	}, nil, nil
}

func handleGetEntities(ctx context.Context, _ *mcp.CallToolRequest, args GetEntitiesArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	if len(args.Metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: at least one metric name is required"}},
			IsError: true,
		}, nil, nil
	}

	params := url.Values{}
	params.Set("metrics", strings.Join(args.Metrics, ","))
	if args.Search != "" {
		params.Set("search", args.Search)
	}

	body, statusCode, err := lensConfig.ServiceClient.Get(ctx, "/lfx-lens/semantic-layer/entities", params)
	if err != nil {
		return nil, nil, fmt.Errorf("get_entities API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))}},
			IsError: true,
		}, nil, nil
	}

	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
	pretty, _ := json.MarshalIndent(raw, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(pretty)}},
	}, nil, nil
}

func handleListSavedQueries(ctx context.Context, _ *mcp.CallToolRequest, _ ListSavedQueriesArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	body, statusCode, err := lensConfig.ServiceClient.Get(ctx, "/lfx-lens/semantic-layer/saved-queries", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("list_saved_queries API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))}},
			IsError: true,
		}, nil, nil
	}

	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
	pretty, _ := json.MarshalIndent(raw, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(pretty)}},
	}, nil, nil
}

func handleQueryMetrics(ctx context.Context, _ *mcp.CallToolRequest, args QueryMetricsArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	if len(args.Metrics) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: at least one metric name is required"}},
			IsError: true,
		}, nil, nil
	}

	// Build the request body matching the Lens API QueryRequest schema.
	reqBody := map[string]any{
		"metrics": args.Metrics,
	}
	if len(args.GroupBy) > 0 {
		reqBody["group_by"] = args.GroupBy
	}
	if len(args.Where) > 0 {
		reqBody["where"] = args.Where
	}
	if len(args.OrderBy) > 0 {
		reqBody["order_by"] = args.OrderBy
	}
	if args.Limit > 0 {
		reqBody["limit"] = args.Limit
	}

	body, statusCode, err := lensConfig.ServiceClient.PostJSON(ctx, "/lfx-lens/semantic-layer/query", reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("query_metrics API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error (HTTP %d): %s", statusCode, string(body))}},
			IsError: true,
		}, nil, nil
	}

	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
	pretty, _ := json.MarshalIndent(raw, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(pretty)}},
	}, nil, nil
}
