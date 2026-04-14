// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client utilities for interacting with LFX v2 APIs, including OAuth2 token exchange.
package lfxv2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AccessCheckClient calls the V2 access-check endpoint to verify that the
// authenticated user has a particular relationship to a resource.
//
// The access-check endpoint is backed by OpenFGA and evaluates relationships
// transitively. For example, an owner of a parent project is implicitly a
// writer on child projects.
type AccessCheckClient struct {
	apiURL     string
	httpClient *http.Client
}

// accessCheckRequest is the JSON body sent to the access-check endpoint.
type accessCheckRequest struct {
	Requests []string `json:"requests"`
}

// accessCheckResponse is the JSON body returned by the access-check endpoint.
type accessCheckResponse struct {
	Results []string `json:"results"`
}

// NewAccessCheckClient creates a client for V2 access-check calls.
//
// apiURL is the base URL of the V2 API (e.g., "https://lfx-api.v2.cluster.lfx.dev").
// httpClient should be a plain HTTP client — the user's exchanged V2 token is
// passed explicitly per-request, not via an auth interceptor.
func NewAccessCheckClient(apiURL string, httpClient *http.Client) *AccessCheckClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &AccessCheckClient{
		apiURL:     strings.TrimSuffix(apiURL, "/"),
		httpClient: httpClient,
	}
}

// CheckAccess sends a batch of access-check requests and returns the results
// as a map from request string to granted status.
//
// token is the user's exchanged V2 bearer token (not the MCP token or API key).
// requests are formatted as "object:id#relation" (e.g., "project:uuid#writer").
//
// The V2 access-check endpoint returns results in an unordered list. Each
// result is a tab-delimited string in the format:
//
//	<request>@<user>\ttrue|false
//
// This method parses those results and matches them back to the original
// requests. The returned map is keyed by the original request string.
func (c *AccessCheckClient) CheckAccess(ctx context.Context, token string, requests []string) (map[string]bool, error) {
	body, err := json.Marshal(accessCheckRequest{Requests: requests})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal access-check request: %w", err)
	}

	url := c.apiURL + "/access-check?v=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create access-check request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("access-check request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Response body close errors are not actionable after reading.

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read access-check response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("access-check returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result accessCheckResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse access-check response: %w", err)
	}

	if len(result.Results) != len(requests) {
		return nil, fmt.Errorf("access-check returned %d results for %d requests", len(result.Results), len(requests))
	}

	parsed := make(map[string]bool, len(result.Results))
	for _, r := range result.Results {
		req, allowed, err := parseAccessResult(r)
		if err != nil {
			return nil, err
		}
		parsed[req] = allowed
	}

	return parsed, nil
}

// CheckProjectAccess verifies the user has the specified relation to a project.
//
// Returns nil if access is allowed. Returns an error describing the denial if
// access is denied or if the check fails.
func (c *AccessCheckClient) CheckProjectAccess(ctx context.Context, token string, projectUUID, relation string) error {
	request := fmt.Sprintf("project:%s#%s", projectUUID, relation)

	results, err := c.CheckAccess(ctx, token, []string{request})
	if err != nil {
		return fmt.Errorf("access check failed: %w", err)
	}

	allowed, ok := results[request]
	if !ok {
		return fmt.Errorf("access check did not return a result for %s", request)
	}
	if !allowed {
		return fmt.Errorf("access denied: user does not have %s relation to project %s", relation, projectUUID)
	}

	return nil
}

// parseAccessResult extracts the original request and granted/denied status
// from a V2 access-check result string. The format is:
//
//	<request>@<user>\t<true|false>
//
// For example: "project:uuid#writer@user:auth0|alice\ttrue"
func parseAccessResult(result string) (request string, allowed bool, err error) {
	parts := strings.SplitN(result, "\t", 2)
	if len(parts) != 2 {
		return "", false, fmt.Errorf("unexpected access-check result format (no tab delimiter): %q", result)
	}

	// The left side is "<request>@<user_type>:<user_id>".
	// Split on the last "@" to extract the original request.
	atIdx := strings.LastIndex(parts[0], "@")
	if atIdx < 0 {
		return "", false, fmt.Errorf("unexpected access-check result format (no @ delimiter): %q", result)
	}

	request = parts[0][:atIdx]
	allowed = parts[1] == "true"

	return request, allowed, nil
}
