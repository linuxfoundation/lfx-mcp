// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client configuration for LFX v2 API services.
package lfxv2

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	goahttp "goa.design/goa/v3/http"

	projectclient "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/http/project_service/client"
	projectservice "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/project_service"
)

// ClientConfig holds configuration for LFX v2 API clients.
type ClientConfig struct {
	// APIDomain is the LFX API base domain.
	APIDomain string

	// HTTPClient is the HTTP client to use for API calls.
	// If nil, a default client with 30s timeout will be created.
	HTTPClient *http.Client
}

// Clients holds initialized LFX v2 API service clients.
type Clients struct {
	Project *projectservice.Client
}

// NewClients initializes and returns LFX v2 API service clients.
func NewClients(ctx context.Context, cfg ClientConfig) (*Clients, error) {
	if cfg.APIDomain == "" {
		return nil, fmt.Errorf("APIDomain is required")
	}

	// Create HTTP client if not provided.
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	// Initialize project service client.
	projectURL, err := url.Parse(cfg.APIDomain + "/projects")
	if err != nil {
		return nil, fmt.Errorf("failed to parse project service URL: %w", err)
	}

	projectHTTPClient := projectclient.NewClient(
		projectURL.Scheme,
		projectURL.Host,
		httpClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)

	projectClient := projectservice.NewClient(
		projectHTTPClient.GetProjects(),
		projectHTTPClient.CreateProject(),
		projectHTTPClient.GetOneProjectBase(),
		projectHTTPClient.GetOneProjectSettings(),
		projectHTTPClient.UpdateProjectBase(),
		projectHTTPClient.UpdateProjectSettings(),
		projectHTTPClient.DeleteProject(),
		projectHTTPClient.Readyz(),
		projectHTTPClient.Livez(),
	)

	return &Clients{
		Project: projectClient,
	}, nil
}
