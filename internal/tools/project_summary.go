// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	projectservice "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/project_service"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterGetProjectSummary registers the get_project_summary tool.
func RegisterGetProjectSummary(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name: "get_project_summary",
		Description: "Get a rolled-up fact sheet for an LFX project: child project count, " +
			"committee count, working group count, meeting count, plus base project metadata " +
			"(stage, legal entity type, formation date, funding model). " +
			"Useful for PMO complexity scoring and cost modeling.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Project Summary",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetProjectSummary)
}

// GetProjectSummaryArgs defines input parameters for get_project_summary.
type GetProjectSummaryArgs struct {
	UID string `json:"uid" jsonschema:"The v2 UID of the project to retrieve dimensions for"`
}

// ProjectSummary is the rolled-up fact sheet returned by get_project_summary.
type ProjectSummary struct {
	UID               *string  `json:"uid,omitempty"`
	Name              *string  `json:"name,omitempty"`
	Stage             *string  `json:"stage,omitempty"`
	LegalEntityType   *string  `json:"legal_entity_type,omitempty"`
	FormationDate     *string  `json:"formation_date,omitempty"`
	FundingModel      []string `json:"funding_model,omitempty"`
	ChildProjectCount *uint64  `json:"child_project_count,omitempty"`
	CommitteeCount    *uint64  `json:"committee_count,omitempty"`
	WorkingGroupCount *uint64  `json:"working_group_count,omitempty"`
	MeetingCount      *uint64  `json:"meeting_count,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
}

// handleGetProjectSummary returns a rolled-up complexity fact sheet for a project.
func handleGetProjectSummary(ctx context.Context, req *mcp.CallToolRequest, args GetProjectSummaryArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if projectConfig == nil {
		return errorResult("project tools not configured"), nil, nil
	}

	if args.UID == "" {
		return errorResult("uid is required"), nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to extract MCP token: %v", err)), nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           projectConfig.LFXAPIURL,
		TokenExchangeClient: projectConfig.TokenExchangeClient,
		DebugLogger:         projectConfig.DebugLogger,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))), nil, nil
	}

	parent := "project:" + args.UID
	version := "1"

	childType := projectResourceType
	commType := committeeResourceType
	mtgType := meetingResourceType

	var (
		childCount     *querysvc.QueryResourcesCountResult
		committeeCount *querysvc.QueryResourcesCountResult
		wgCount        *querysvc.QueryResourcesCountResult
		meetingCount   *querysvc.QueryResourcesCountResult
		mu             sync.Mutex
		warnings       []string
		wg             sync.WaitGroup
	)

	addWarning := func(msg string) {
		mu.Lock()
		warnings = append(warnings, msg)
		mu.Unlock()
	}

	wg.Add(4)

	go func() {
		defer wg.Done()
		res, err := clients.QuerySvc.QueryResourcesCount(ctx, &querysvc.QueryResourcesCountPayload{
			Version: version, Type: &childType, Parent: &parent,
		})
		if err != nil {
			logger.Warn("failed to count child projects", "error", lfxv2.ErrorMessage(err))
			addWarning("child_project_count unavailable: " + lfxv2.ErrorMessage(err))
			return
		}
		mu.Lock()
		childCount = res
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		res, err := clients.QuerySvc.QueryResourcesCount(ctx, &querysvc.QueryResourcesCountPayload{
			Version: version, Type: &commType, Parent: &parent,
		})
		if err != nil {
			logger.Warn("failed to count committees", "error", lfxv2.ErrorMessage(err))
			addWarning("committee_count unavailable: " + lfxv2.ErrorMessage(err))
			return
		}
		mu.Lock()
		committeeCount = res
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		res, err := clients.QuerySvc.QueryResourcesCount(ctx, &querysvc.QueryResourcesCountPayload{
			Version: version, Type: &commType, Parent: &parent,
			Filters: []string{"category:Working Group"},
		})
		if err != nil {
			logger.Warn("failed to count working groups", "error", lfxv2.ErrorMessage(err))
			addWarning("working_group_count unavailable: " + lfxv2.ErrorMessage(err))
			return
		}
		mu.Lock()
		wgCount = res
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		res, err := clients.QuerySvc.QueryResourcesCount(ctx, &querysvc.QueryResourcesCountPayload{
			Version: version, Type: &mtgType, Parent: &parent,
		})
		if err != nil {
			logger.Warn("failed to count meetings", "error", lfxv2.ErrorMessage(err))
			addWarning("meeting_count unavailable: " + lfxv2.ErrorMessage(err))
			return
		}
		mu.Lock()
		meetingCount = res
		mu.Unlock()
	}()

	wg.Wait()

	// Get base project info
	baseResult, err := clients.Project.GetOneProjectBase(ctx, &projectservice.GetOneProjectBasePayload{
		UID: &args.UID,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get project: %s", lfxv2.ErrorMessage(err))), nil, nil
	}

	p := baseResult.Project
	dims := ProjectSummary{
		UID:             p.UID,
		Name:            p.Name,
		Stage:           p.Stage,
		LegalEntityType: p.LegalEntityType,
		FundingModel:    p.FundingModel,
		FormationDate:   p.FormationDate,
		Warnings:        warnings,
	}

	if childCount != nil {
		dims.ChildProjectCount = &childCount.Count
	}

	if committeeCount != nil {
		dims.CommitteeCount = &committeeCount.Count
	}

	if wgCount != nil {
		dims.WorkingGroupCount = &wgCount.Count
	}

	if meetingCount != nil {
		dims.MeetingCount = &meetingCount.Count
	}

	prettyJSON, err := json.MarshalIndent(dims, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("failed to format result: %v", err)), nil, nil
	}

	logger.Info("get_project_summary succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// errorResult is a convenience helper for returning a tool error response.
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "Error: " + msg},
		},
		IsError: true,
	}
}
