// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	lfxauth "github.com/linuxfoundation/lfx-mcp/internal/auth"
	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Keep the supported types and their ordering in one place. Organisation
// roles are deliberately deferred pending the source decision for a later change.
const myPermissionTypes = "project,committee,project_membership"

const myPermissionsCoverage = "Only direct grants on projects, %s and project memberships are listed. Organisation grants, inherited roles, team-based roles, public access and access implied by higher roles are not listed; a missing entry does not mean you lack access. Names are best-effort and may be omitted when unavailable, not visible to you, or beyond the lookup limit."

// ListMyPermissionsArgs has no parameters: the caller's identity defines the query.
type ListMyPermissionsArgs struct{}

type myPermissionsResult struct {
	Permissions []myPermission `json:"permissions"`
	Coverage    string         `json:"coverage"`
	Warnings    []string       `json:"warnings,omitempty"`
}

type myPermission struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
	Role string `json:"role"`
	Name string `json:"name,omitempty"`
}

// RegisterListMyPermissions registers the signed-in person's direct roles tool.
func RegisterListMyPermissions(server *mcp.Server, committeesAsGroups bool) {
	committeeLabel := "committees"
	if committeesAsGroups {
		committeeLabel = "groups"
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_my_permissions",
		Description: fmt.Sprintf("Lists the roles granted directly to you on projects, %s and project memberships (key contact). Roles inherited from a parent project or organisation, held through a team, or implied by a higher role are not listed, so a missing entry does not mean you lack access.", committeeLabel),
		Annotations: &mcp.ToolAnnotations{Title: "List My Permissions", ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ ListMyPermissionsArgs) (*mcp.CallToolResult, myPermissionsResult, error) {
		return handleListMyPermissions(ctx, req, committeesAsGroups)
	})
}

func handleListMyPermissions(ctx context.Context, req *mcp.CallToolRequest, groups bool) (*mcp.CallToolResult, myPermissionsResult, error) {
	logger := newToolLogger(ctx, req)
	var tokenInfo *auth.TokenInfo
	if req.Extra != nil {
		tokenInfo = req.Extra.TokenInfo
	}
	isAPIKey := false
	if tokenInfo != nil {
		isAPIKey, _ = tokenInfo.Extra[lfxauth.APIKeyAuthExtraKey].(bool)
	}
	if IsMachineAccount(tokenInfo) || isAPIKey {
		return nil, myPermissionsResult{}, toolError("List My Permissions requires a signed-in person; machine accounts and API keys are not supported.")
	}
	if projectConfig == nil || projectConfig.Clients == nil || projectConfig.Clients.AccessCheck == nil {
		logger.ErrorContext(ctx, "my permissions tool not configured")
		return nil, myPermissionsResult{}, toolError("Error: my permissions tool not configured")
	}
	clients := projectConfig.Clients
	ctx, err := clients.TokenFromRequest(ctx, tokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to resolve LFX authentication", "error", err)
		return nil, myPermissionsResult{}, toolError(friendlyAPIError("failed to resolve LFX authentication", err))
	}
	token, err := clients.GetExchangedToken(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to get LFX token", "error", err)
		return nil, myPermissionsResult{}, toolError(friendlyAPIError("failed to get LFX token", err))
	}
	committeeLabel := "committees"
	if groups {
		committeeLabel = "groups"
	}
	out := myPermissionsResult{Permissions: []myPermission{}, Coverage: fmt.Sprintf(myPermissionsCoverage, committeeLabel)}
	types := strings.Split(myPermissionTypes, ",")
	var failures []error
	counts := make(map[string]int, len(types))
	for _, typ := range types {
		counts[typ] = 0
		grants, err := clients.AccessCheck.MyGrants(ctx, token, typ)
		if err != nil {
			logger.ErrorContext(ctx, "listing direct roles failed", "object_type", typ, "error", err)
			label := strings.ReplaceAll(typ, "_", " ")
			if typ == "committee" && groups {
				label = "group"
			}
			out.Warnings = append(out.Warnings, "Could not list your "+label+" roles: "+apiErrorDetail(err))
			failures = append(failures, err)
			continue
		}
		rows := make([]myPermission, 0, len(grants))
		seen := make(map[lfxv2.MyGrant]bool, len(grants))
		for _, grant := range grants {
			if !seen[grant] {
				seen[grant] = true
				rows = append(rows, myPermission{Type: grant.Type, UID: grant.UID, Role: grant.Role})
			}
		}
		resolvePermissionNames(ctx, clients, logger, typ, rows)
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Name != rows[j].Name {
				return rows[i].Name < rows[j].Name
			}
			if rows[i].UID != rows[j].UID {
				return rows[i].UID < rows[j].UID
			}
			return rows[i].Role < rows[j].Role
		})
		out.Permissions = append(out.Permissions, rows...)
		counts[typ] = len(rows)
	}
	logger.InfoContext(ctx, "listed direct roles", "counts_by_type", counts)
	if len(failures) == len(types) {
		// Do not publish a success-shaped empty result when nothing succeeded.
		// Collapse only identical caller-facing errors: status 0 also covers
		// unrelated 400/5xx/network errors, whose details must not be lost.
		detail := apiErrorDetail(failures[0])
		sameDetail := true
		for _, err := range failures[1:] {
			if apiErrorDetail(err) != detail {
				sameDetail = false
			}
		}
		if sameDetail {
			return nil, myPermissionsResult{}, toolError(friendlyAPIError("failed to list your permissions", failures[0]))
		}
		return nil, myPermissionsResult{}, toolError("Failed to list your permissions: " + strings.Join(out.Warnings, " "))
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, myPermissionsResult{}, toolError("Error: failed to format permissions")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, out, nil
}

// Resolve at most one page of names per type, for a deterministic subset of
// UIDs. QueryResources applies the caller's visibility; missing names reveal
// nothing about why a record was absent. Memberships have no display-name field
// in their indexer contract, so do not invent one from a company or tier name.
func resolvePermissionNames(ctx context.Context, clients *lfxv2.Clients, logger *slog.Logger, typ string, rows []myPermission) {
	if len(rows) == 0 || (typ != "project" && typ != "committee") {
		return
	}
	const limit = 100
	uidSet := make(map[string]bool)
	for _, row := range rows {
		uidSet[row.UID] = true
	}
	uids := make([]string, 0, len(uidSet))
	for uid := range uidSet {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	if len(uids) > limit {
		uids = uids[:limit]
	}
	filters := make([]string, len(uids))
	selected := make(map[string]bool, len(uids))
	for i, uid := range uids {
		filters[i] = "uid:" + uid
		selected[uid] = true
	}
	result, err := clients.QuerySvc.QueryResources(ctx, &querysvc.QueryResourcesPayload{
		Version: "1", Type: &typ, FiltersOr: filters, PageSize: limit, Sort: "name_asc",
	})
	if err != nil {
		logger.ErrorContext(ctx, "permission names unavailable", "object_type", typ, "error", err)
		return
	}
	names := make(map[string]string)
	for _, resource := range result.Resources {
		if resource == nil || resource.Type == nil || *resource.Type != typ {
			continue
		}
		data, ok := resource.Data.(map[string]any)
		if !ok {
			continue
		}
		uid, _ := data["uid"].(string)
		if uid == "" && resource.ID != nil {
			uid = strings.TrimPrefix(*resource.ID, typ+":")
		}
		if selected[uid] {
			if name, ok := data["name"].(string); ok {
				names[uid] = name
			}
		}
	}
	for i := range rows {
		rows[i].Name = names[rows[i].UID]
	}
}
