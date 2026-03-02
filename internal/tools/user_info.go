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
	OAuthDomain string
	HTTPClient  *http.Client
}

var userInfoConfig *UserInfoConfig

// SetUserInfoConfig sets the configuration for the user_info tool.
func SetUserInfoConfig(cfg *UserInfoConfig) {
	userInfoConfig = cfg
}

// RegisterUserInfo registers the user_info tool with the MCP server.
func RegisterUserInfo(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "user_info",
		Description: "Get the authenticated user's OpenID Connect profile by proxying to the /userinfo endpoint",
	}, handleUserInfo)
}

// handleUserInfo implements the user_info tool logic.
func handleUserInfo(ctx context.Context, req *mcp.CallToolRequest, args UserInfoArgs) (*mcp.CallToolResult, any, error) {
	if userInfoConfig == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: user_info tool not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if userInfoConfig.OAuthDomain == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: oauth.domain not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	// Extract Authorization header from request meta.
	authHeader := ""
	if req.Params.Meta != nil {
		if authMap, ok := req.Params.Meta["authorization"].(map[string]interface{}); ok {
			if headerVal, ok := authMap["header"].(string); ok {
				authHeader = headerVal
			}
		}
	}

	if authHeader == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: Authorization header required"},
			},
			IsError: true,
		}, nil, nil
	}

	// Call OAuth userinfo endpoint.
	userInfoURL := fmt.Sprintf("https://%s/userinfo", userInfoConfig.OAuthDomain)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
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
		// Return raw response if not valid JSON.
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(body)},
			},
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(userInfo, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(body)},
			},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
