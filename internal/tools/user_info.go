// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// UserInfoArgs defines the input parameters for the user_info tool.
type UserInfoArgs struct {
	// No input parameters - uses Authorization header from request context.
}

// UserInfoConfig holds configuration for the user_info tool.
type UserInfoConfig struct {
	UserInfoEndpoint string // Full userinfo endpoint URL (e.g., https://example.auth0.com/userinfo).
	HTTPClient       *http.Client
}

var userInfoConfig *UserInfoConfig

// SetUserInfoConfig sets the configuration for the user_info tool.
func SetUserInfoConfig(cfg *UserInfoConfig) {
	userInfoConfig = cfg
}

// RegisterUserInfo registers the user_info tool with the MCP server.
func RegisterUserInfo(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "user_info",
		Description: "Get the authenticated user's OpenID Connect profile by proxying to the /userinfo endpoint",
		Annotations: &mcp.ToolAnnotations{
			Title:        "User Info",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleUserInfo)
}

// handleUserInfo implements the user_info tool logic.
func handleUserInfo(ctx context.Context, req *mcp.CallToolRequest, _ UserInfoArgs) (*mcp.CallToolResult, any, error) {
	// Create MCP logger that sends logs to the client.
	logger := newToolLogger(req)

	if userInfoConfig == nil {
		logger.Error("user_info tool not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: user_info tool not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if userInfoConfig.UserInfoEndpoint == "" {
		logger.Error("userinfo endpoint not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: userinfo endpoint not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("fetching user info from OAuth provider")

	// Extract raw token from TokenInfo.Extra (populated by JWT verifier).
	var rawToken string
	if req.Extra.TokenInfo != nil && req.Extra.TokenInfo.Extra != nil {
		if token, ok := req.Extra.TokenInfo.Extra["raw_token"].(string); ok {
			rawToken = token
		}
	}

	if rawToken == "" {
		logger.Error("raw token not found in request")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: Authentication token required"},
			},
			IsError: true,
		}, nil, nil
	}

	// Construct Authorization header.
	authHeader := "Bearer " + rawToken

	// Call OAuth userinfo endpoint.
	logger.Debug("sending request to userinfo endpoint", "url", userInfoConfig.UserInfoEndpoint)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", userInfoConfig.UserInfoEndpoint, nil)
	if err != nil {
		logger.Error("failed to create HTTP request", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error creating request: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	httpReq.Header.Set("Authorization", authHeader)

	client := userInfoConfig.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error calling OAuth userinfo: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read response body", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error reading response: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	// Check for non-200 status.
	if resp.StatusCode != http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("OAuth returned status %d: %s", resp.StatusCode, string(body))},
			},
			IsError: true,
		}, nil, nil
	}

	// Parse and pretty-print JSON.
	var userInfo map[string]interface{}
	if err := json.Unmarshal(body, &userInfo); err != nil {
		logger.Error("failed to parse JSON response", "error", err)
		// Return raw response if not valid JSON.
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(body)},
			},
		}, nil, nil
	}

	// Return pretty-printed JSON.
	prettyJSON, err := json.MarshalIndent(userInfo, "", "  ")
	if err != nil {
		logger.Error("failed to format JSON response", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error formatting response: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("user info retrieved successfully", "sub", userInfo["sub"])

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
