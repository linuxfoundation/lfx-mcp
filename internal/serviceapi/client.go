// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package serviceapi provides an HTTP client for internal LFX service APIs
// authenticated with OAuth2 client credentials (M2M bearer tokens). These
// services (e.g., Member Onboarding, LFX Lens) have no per-user authorization
// — the MCP server enforces access control via the V2 access-check endpoint
// before proxying requests here.
package serviceapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-mcp/internal/redact"
)

// TokenSource provides bearer tokens for authenticating requests to service APIs.
type TokenSource interface {
	GetToken(ctx context.Context) (string, error)
}

// Config holds configuration for a service API client.
type Config struct {
	// BaseURL is the root URL of the service API (e.g., "https://onboarding.lfx.internal").
	BaseURL string

	// TokenSource provides bearer tokens for request authentication.
	TokenSource TokenSource

	// HTTPClient is the HTTP client to use. If nil, a default client with 30s
	// timeout is created.
	HTTPClient *http.Client

	// DebugLogger enables wire-level request/response logging when non-nil.
	DebugLogger *slog.Logger
}

// Client is an HTTP client for an internal service API.
type Client struct {
	baseURL     string
	tokenSource TokenSource
	httpClient  *http.Client
	debugLogger *slog.Logger
}

// NewClient creates a new service API client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("BaseURL is required")
	}
	if cfg.TokenSource == nil {
		return nil, fmt.Errorf("TokenSource is required")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	if cfg.DebugLogger != nil {
		httpClient = wrapWithDebugTransport(httpClient, cfg.DebugLogger)
	}

	return &Client{
		baseURL:     strings.TrimSuffix(cfg.BaseURL, "/"),
		tokenSource: cfg.TokenSource,
		httpClient:  httpClient,
		debugLogger: cfg.DebugLogger,
	}, nil
}

// Get performs a GET request to the given path with optional query parameters.
// Returns the response body, HTTP status code, and any error.
func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, int, error) {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create GET request: %w", err)
	}

	return c.do(req)
}

// PostJSON performs a POST request with a JSON body.
// Returns the response body, HTTP status code, and any error.
func (c *Client) PostJSON(ctx context.Context, path string, body any) ([]byte, int, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal JSON body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create POST request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return c.do(req)
}

// PostMultipart performs a POST request with a multipart/form-data body.
// This is used for AgentOS endpoints that accept multipart form submissions.
// Returns the response body, HTTP status code, and any error.
func (c *Client) PostMultipart(ctx context.Context, path string, fields map[string]string) ([]byte, int, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, 0, fmt.Errorf("failed to write multipart field %q: %w", key, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, 0, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create multipart POST request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	return c.do(req)
}

// do executes an HTTP request with bearer token authentication.
func (c *Client) do(req *http.Request) ([]byte, int, error) {
	token, err := c.tokenSource.GetToken(req.Context())
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get bearer token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Response body close errors are not actionable after reading.

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, resp.StatusCode, nil
}

// wrapWithDebugTransport wraps an HTTP client with wire-level request/response logging.
func wrapWithDebugTransport(client *http.Client, logger *slog.Logger) *http.Client {
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Transport: &serviceDebugTransport{
			transport: base,
			logger:    logger,
		},
		Timeout: client.Timeout,
	}
}

// serviceDebugTransport logs the full HTTP wire dump of every request and
// response. The logged copy is masked as package redact describes; the
// request sent and the response returned are not modified.
type serviceDebugTransport struct {
	transport http.RoundTripper
	logger    *slog.Logger
}

// RoundTrip implements http.RoundTripper.
func (dt *serviceDebugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqDump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		dt.logger.Error("failed to dump outbound request", "error", err)
	} else {
		dt.logger.Debug("serviceapi outbound request", "dump", redact.WireDump(reqDump))
	}

	resp, err := dt.transport.RoundTrip(req)
	if err != nil {
		dt.logger.Error("serviceapi outbound request failed", "error", err, "url", redact.URL(req.URL.String()))
		return nil, err
	}

	// Read the body here rather than letting DumpResponse consume it: when the
	// read fails part-way, DumpResponse leaves the body drained and the
	// response is handed on as if intact, so the failure only surfaces from
	// whatever reads the body next, detached from this request. Reading first
	// turns it into this request's error, naming the path, and hands the
	// caller the full body.
	if resp.Body == nil {
		resp.Body = http.NoBody
	}
	body, readErr := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); cerr != nil {
		dt.logger.Warn("failed to close inbound response body", "error", cerr, "url", redact.URL(req.URL.String()))
	}
	if readErr != nil {
		dt.logger.Error("failed to read inbound response body", "error", readErr, "url", redact.URL(req.URL.String()))
		return nil, fmt.Errorf("reading %s response body: %w", req.URL.Path, readErr)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	respDump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		dt.logger.Error("failed to dump inbound response", "error", err)
	} else {
		dt.logger.Debug("serviceapi inbound response", "dump", redact.WireDump(respDump))
	}
	// However DumpResponse left resp.Body, hand the caller a reader over the
	// bytes read above.
	resp.Body = io.NopCloser(bytes.NewReader(body))

	return resp, nil
}
