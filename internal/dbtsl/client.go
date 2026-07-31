// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package dbtsl provides a client for the dbt Semantic Layer API.
//
// The dbt Semantic Layer is the governed definition of every LFX Insights
// metric. This client talks to it directly over the GraphQL API, for both
// metadata (metrics, dimensions) and query execution.
//
// That is a deliberate divergence from the two Python reference
// implementations, lfx-lens and dbt Labs' own dbt-mcp server, which use the
// dbtsl SDK and split the transport: GraphQL for metadata, Arrow Flight over
// gRPC for execution. There is no Go SDK for the dbt Semantic Layer, and
// reproducing the Flight path would mean taking on Arrow, gRPC and session
// lifecycle for no benefit at the volumes this server queries. Callers are
// capped at 500 rows, comfortably inside the GraphQL API's 1024-row page, so
// pagination never engages.
//
// Access to metrics is gated by an allowlist (see allowlist.go). Dimension
// value discovery is gated on the caller supplying the metrics the dimension
// belongs to, because a dimension-only query does not consult the metric
// allowlist at all.
package dbtsl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultTimeout bounds a single GraphQL round trip. Query execution
	// polls, so its overall bound comes from the caller's context instead.
	defaultTimeout = 30 * time.Second

	oneWeek = 7 * 24 * time.Hour

	// metricsCacheTTL and dimensionsCacheTTL: metadata only changes on a dbt
	// deploy, so a long TTL is safe.
	metricsCacheTTL    = oneWeek
	dimensionsCacheTTL = oneWeek

	// dimensionValuesCacheTTL is much shorter: values track the warehouse
	// rather than the dbt deploy, so they cannot share the metadata TTL.
	dimensionValuesCacheTTL = time.Hour
)

// Config holds the settings needed to reach the dbt Semantic Layer.
type Config struct {
	// Host is the Semantic Layer hostname, without scheme or path,
	// e.g. "tj283.semantic-layer.us1.dbt.com".
	Host string
	// EnvironmentID is the dbt environment to query.
	EnvironmentID string
	// Token is the dbt service token.
	Token string
	// HTTPClient is optional. When nil a client with defaultTimeout is used.
	//
	// Do not pass a client wrapped in the serviceapi debug transport: it dumps
	// the Authorization header, which would print this long-lived service
	// token into logs whenever debug traffic is enabled.
	HTTPClient *http.Client
}

// Client queries the dbt Semantic Layer.
type Client struct {
	graphqlURL    string
	environmentID int64
	token         string
	httpClient    *http.Client

	metricsCache         *ttlCache[string, []MetricInfo]
	dimensionsCache      *ttlCache[string, []DimensionInfo]
	dimensionValuesCache *ttlCache[string, []string]
}

// NewClient validates cfg and returns a ready client.
func NewClient(cfg Config) (*Client, error) {
	host := strings.TrimSpace(cfg.Host)
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")
	if host == "" {
		return nil, fmt.Errorf("dbt Semantic Layer host is required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("dbt Semantic Layer token is required")
	}
	envID := strings.TrimSpace(cfg.EnvironmentID)
	if envID == "" {
		return nil, fmt.Errorf("dbt Semantic Layer environment ID is required")
	}
	parsedEnvID, err := strconv.ParseInt(envID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("dbt Semantic Layer environment ID %q is not a number: %w", envID, err)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return &Client{
		graphqlURL:           "https://" + host + "/api/graphql",
		environmentID:        parsedEnvID,
		token:                cfg.Token,
		httpClient:           httpClient,
		metricsCache:         newTTLCache[string, []MetricInfo](metricsCacheTTL, 50),
		dimensionsCache:      newTTLCache[string, []DimensionInfo](dimensionsCacheTTL, 100),
		dimensionValuesCache: newTTLCache[string, []string](dimensionValuesCacheTTL, 200),
	}, nil
}

// ClearCaches drops every cached metadata and dimension value entry.
func (c *Client) ClearCaches() {
	c.metricsCache.clear()
	c.dimensionsCache.clear()
	c.dimensionValuesCache.clear()
}

// graphqlError is one entry in a GraphQL response's errors array.
type graphqlError struct {
	Message string `json:"message"`
}

// graphqlResponse is the envelope every GraphQL reply arrives in.
type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphqlError  `json:"errors"`
}

// graphqlRequest executes query against the Semantic Layer and decodes the
// response's data field into out.
//
// environmentId is injected into variables automatically, since every
// operation in this API takes it.
func (c *Client) graphqlRequest(ctx context.Context, query string, variables map[string]any, out any) error {
	vars := make(map[string]any, len(variables)+1)
	for k, v := range variables {
		vars[k] = v
	}
	vars["environmentId"] = c.environmentID

	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("failed to encode GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphqlURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to build GraphQL request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("semantic layer request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read semantic layer response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("semantic layer returned HTTP %d: %s", resp.StatusCode, truncateForError(string(body)))
	}

	var envelope graphqlResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("failed to decode semantic layer response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		messages := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			msg := e.Message
			if msg == "" {
				msg = "Unknown error"
			}
			messages = append(messages, msg)
		}
		return fmt.Errorf("semantic layer GraphQL error: %s", strings.Join(messages, "; "))
	}
	if len(envelope.Data) == 0 {
		return fmt.Errorf("semantic layer returned no data")
	}

	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("failed to decode semantic layer data: %w", err)
	}
	return nil
}

// truncateForError keeps an upstream error body short enough to be readable
// when it is surfaced to a model.
func truncateForError(s string) string {
	const limit = 512
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
