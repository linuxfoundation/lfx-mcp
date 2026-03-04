// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	projectservice "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/project_service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// tempTLFFetchProjectID is the hardcoded project ID used to verify token exchange.
const tempTLFFetchProjectID = "a27394a3-7a6c-4d0f-9e0f-692d8753924f"

// TempTLFFetchConfig holds configuration for the temp_tlf_fetch tool.
type TempTLFFetchConfig struct {
	LFXAPIURL           string
	TokenExchangeClient *lfxv2.TokenExchangeClient
}

var tempTLFFetchConfig *TempTLFFetchConfig

// SetTempTLFFetchConfig sets the configuration for the temp_tlf_fetch tool.
func SetTempTLFFetchConfig(cfg *TempTLFFetchConfig) {
	tempTLFFetchConfig = cfg
}

// RegisterTempTLFFetch registers the temp_tlf_fetch tool with the MCP server.
func RegisterTempTLFFetch(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "temp_tlf_fetch",
		Description: "Temporary tool: fetches a hardcoded TLF project via GetOneProjectBase to verify token exchange",
	}, handleTempTLFFetch)
}

// TempTLFFetchArgs defines the input parameters for the temp_tlf_fetch tool.
type TempTLFFetchArgs struct{}

// handleTempTLFFetch implements the temp_tlf_fetch tool logic.
func handleTempTLFFetch(ctx context.Context, req *mcp.CallToolRequest, _ TempTLFFetchArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if tempTLFFetchConfig == nil {
		logger.Error("temp_tlf_fetch tool not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: temp_tlf_fetch tool not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	// Extract raw MCP token from request for token exchange.
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

	// Attach token to context for automatic token exchange in LFX API calls.
	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	logger.Info("fetching project via GetOneProjectBase", "project_id", tempTLFFetchProjectID)

	// Create LFX v2 clients with token exchange enabled.
	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           tempTLFFetchConfig.LFXAPIURL,
		TokenExchangeClient: tempTLFFetchConfig.TokenExchangeClient,
	})
	if err != nil {
		logger.Error("failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to create LFX v2 clients: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	// Call GetOneProjectBase with the hardcoded project ID.
	projectID := tempTLFFetchProjectID
	result, err := clients.Project.GetOneProjectBase(ctx, &projectservice.GetOneProjectBasePayload{
		UID: &projectID,
	})
	if err != nil {
		logger.Error("GetOneProjectBase failed", "error", err, "project_id", tempTLFFetchProjectID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: GetOneProjectBase failed: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	// Pretty-print the result as JSON.
	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.Error("failed to marshal project result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("GetOneProjectBase succeeded", "project_id", tempTLFFetchProjectID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
