// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"log/slog"
	"reflect"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
)

// accessChecker checks upstream relations using the caller's exchanged token.
type accessChecker interface {
	CheckAccess(ctx context.Context, token string, requests []string) (map[string]bool, error)
}

// accessCheckerConfigured treats typed-nil implementations as absent too.
func accessCheckerConfigured(checker accessChecker) bool {
	if checker == nil {
		return false
	}
	value := reflect.ValueOf(checker)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

// preparePastMeetingSummaryResults removes the unused provider payload and
// retains pending summary text only for organizers, matching LFX Self Serve.
// Resources stay in the page so pagination and its warning remain unchanged.
func (cfg *MeetingConfig) preparePastMeetingSummaryResults(ctx context.Context, logger *slog.Logger, resources []*querysvc.Resource) {
	type pendingSummary struct {
		data    map[string]any
		request string
	}
	var pending []pendingSummary
	var requests []string
	seen := make(map[string]bool)

	for _, resource := range resources {
		if resource == nil {
			continue
		}
		data, ok := resource.Data.(map[string]any)
		if !ok {
			continue
		}
		delete(data, "zoom_webhook_event")
		requiresApproval, _ := data["requires_approval"].(bool)
		approved, _ := data["approved"].(bool)
		if !requiresApproval || approved {
			continue
		}

		var request string
		if id, _ := data["meeting_and_occurrence_id"].(string); id != "" {
			request = "v1_past_meeting:" + id + "#organizer"
			if !seen[request] {
				seen[request] = true
				requests = append(requests, request)
			}
		}
		pending = append(pending, pendingSummary{data: data, request: request})
	}

	var granted map[string]bool
	if len(requests) > 0 && accessCheckerConfigured(cfg.AccessChecker) && cfg.Clients != nil {
		v2Token, err := cfg.Clients.GetExchangedToken(ctx)
		if err != nil {
			logger.WarnContext(ctx, "failed to obtain V2 token for summary access; pending content withheld")
		} else {
			results, err := cfg.AccessChecker.CheckAccess(ctx, v2Token, requests)
			if err != nil {
				// Upstream errors can include request details; log only the outcome.
				logger.WarnContext(ctx, "failed to check summary organizer access; pending content withheld")
			} else {
				granted = results
			}
		}
	}

	for _, summary := range pending {
		if summary.request != "" && granted[summary.request] {
			continue
		}
		delete(summary.data, "content")
		delete(summary.data, "edited_content")
		summary.data["content_withheld"] = "awaiting organizer approval"
	}
}
