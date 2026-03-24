// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	projectservice "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/project_service"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// projectResourceType is the resource type filter for project queries.
const projectResourceType = "project"

// ProjectConfig holds configuration shared by project tools.
type ProjectConfig struct {
	LFXAPIURL           string
	TokenExchangeClient *lfxv2.TokenExchangeClient
	DebugLogger         *slog.Logger
	// HTTPClient is the HTTP client to use for LFX API calls.
	// If nil, a default 30-second timeout client is created.
	HTTPClient *http.Client
}

var projectConfig *ProjectConfig

// SetProjectConfig sets the configuration for project tools.
func SetProjectConfig(cfg *ProjectConfig) {
	projectConfig = cfg
}

// RegisterSearchProjects registers the search_projects tool with the MCP server.
func RegisterSearchProjects(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "search_projects",
		Description: "Search for LFX projects by name using the LFX query service",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Projects",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleSearchProjects)
}

// RegisterGetProject registers the get_project tool with the MCP server.
func RegisterGetProject(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_project",
		Description: "Get an LFX project's base info and settings by its UID. Privileged project settings may be omitted if the caller lacks sufficient permissions.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Project",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetProject)
}

// SearchProjectsArgs defines the input parameters for the search_projects tool.
type SearchProjectsArgs struct {
	Name      string `json:"name" jsonschema:"Name or partial name of the project to search for"`
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetProjectArgs defines the input parameters for the get_project tool.
type GetProjectArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the project to retrieve"`
}

// handleSearchProjects implements the search_projects tool logic.
func handleSearchProjects(ctx context.Context, req *mcp.CallToolRequest, args SearchProjectsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(req)

	if projectConfig == nil {
		logger.Error("project tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: project tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.Error("failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           projectConfig.LFXAPIURL,
		TokenExchangeClient: projectConfig.TokenExchangeClient,
		DebugLogger:         projectConfig.DebugLogger,
		HTTPClient:          projectConfig.HTTPClient,
	})
	if err != nil {
		logger.Error("failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	resourceType := projectResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
		Sort:     "name_asc",
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.Info("searching projects", "name", args.Name, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.Error("QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search projects: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	type searchResult struct {
		Resources []*querysvc.Resource `json:"resources"`
		PageToken *string              `json:"page_token,omitempty"`
	}

	out := searchResult{
		Resources: result.Resources,
		PageToken: result.PageToken,
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.Error("failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("search_projects succeeded", "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetProject implements the get_project tool logic, fetching both base
// info and settings for the given project UID.
func handleGetProject(ctx context.Context, req *mcp.CallToolRequest, args GetProjectArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(req)

	if projectConfig == nil {
		logger.Error("project tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: project tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.UID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.Error("failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           projectConfig.LFXAPIURL,
		TokenExchangeClient: projectConfig.TokenExchangeClient,
		DebugLogger:         projectConfig.DebugLogger,
		HTTPClient:          projectConfig.HTTPClient,
	})
	if err != nil {
		logger.Error("failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("fetching project", "uid", args.UID)

	baseResult, err := clients.Project.GetOneProjectBase(ctx, &projectservice.GetOneProjectBasePayload{
		UID: &args.UID,
	})
	if err != nil {
		logger.Error("GetOneProjectBase failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get project: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	// Settings may be unavailable (e.g. insufficient permissions, or a response
	// decode failure); treat that as a partial result rather than a hard failure
	// so callers still get the base data they are authorised to see.
	var projectSettings *projectservice.ProjectSettings
	settingsResult, err := clients.Project.GetOneProjectSettings(ctx, &projectservice.GetOneProjectSettingsPayload{
		UID: &args.UID,
	})
	var settingsWarning string
	if err != nil {
		settingsWarning = fmt.Sprintf("WARNING: project settings unavailable - %s", lfxv2.ErrorMessage(err))
		logger.Warn("getting project settings failed, returning base only", "error", lfxv2.ErrorMessage(err), "uid", args.UID)
	} else {
		projectSettings = settingsResult.ProjectSettings
	}

	type projectResult struct {
		Base     *projectservice.ProjectBase     `json:"base"`
		Settings *projectservice.ProjectSettings `json:"settings,omitempty"`
	}

	out := projectResult{
		Base:     baseResult.Project,
		Settings: projectSettings,
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.Error("failed to marshal project result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_project succeeded", "uid", args.UID)

	content := []mcp.Content{}
	if settingsWarning != "" {
		content = append(content, &mcp.TextContent{Text: settingsWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})

	return &mcp.CallToolResult{
		Content: content,
	}, nil, nil
}
