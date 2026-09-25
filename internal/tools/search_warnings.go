// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"fmt"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
)

// resourceSearchResult is the output type shared by the query-backed search
// tools whose result is a page of resources and nothing else. Its fields are
// plain values, not pointers or any, so the published output schema does not
// mark the token or an item's Type and ID as nullable, and does not publish
// Data as a bare true schema (see lfx-mcp#154). The slices still publish as
// nullable arrays: the schema generator reflects every Go slice that way.
type resourceSearchResult struct {
	Resources []searchResource `json:"resources"`
	PageToken string           `json:"page_token,omitempty"`
	Warnings  []string         `json:"warnings,omitempty"`
}

// searchResource is one query service resource in a resourceSearchResult. The
// capitalised keys match the query service's own resource JSON.
type searchResource struct {
	Type string         `json:"Type"`
	ID   string         `json:"ID"`
	Data map[string]any `json:"Data,omitempty"`
}

// newSearchResource converts a query service resource. Data that is not a JSON
// object is kept under "_raw" so no data is silently lost.
func newSearchResource(r *querysvc.Resource) searchResource {
	out := searchResource{Type: derefStr(r.Type), ID: derefStr(r.ID)}
	switch d := r.Data.(type) {
	case nil:
	case map[string]any:
		out.Data = d
	default:
		out.Data = map[string]any{"_raw": d}
	}
	return out
}

// newSearchResources converts a page of query service resources, dropping nil
// entries.
func newSearchResources(rs []*querysvc.Resource) []searchResource {
	resources := make([]searchResource, 0, len(rs))
	for _, r := range rs {
		if r != nil {
			resources = append(resources, newSearchResource(r))
		}
	}
	return resources
}

// newResourceSearchResult builds the result for one query service page:
// it converts the resources, dropping nil entries, and fills the warnings from
// the records actually returned. noun, pageSize and continuation are passed
// through to searchWarnings.
func newResourceSearchResult(noun string, result *querysvc.QueryResourcesResult, pageSize int, continuation bool) resourceSearchResult {
	resources := newSearchResources(result.Resources)
	return resourceSearchResult{
		Resources: resources,
		PageToken: derefStr(result.PageToken),
		Warnings:  searchWarnings(noun, len(resources), pageSize, hasPageToken(result.PageToken), continuation),
	}
}

// notVisibleText returns "no <what> is visible to you; ...": it states that no
// such record is visible to the caller, never that one exists. The query
// service leaves out records the caller cannot view, so an empty answer cannot
// tell a record that does not exist from one the caller may not see.
func notVisibleText(what string) string {
	return fmt.Sprintf("no %s is visible to you; it may not exist, or it may not be shared with you", what)
}

// lookupNotVisibleMessage is the error text of a query-backed lookup by UID
// that returned no record. label names the record ("meeting", "past meeting
// summary"). A lookup that calls an LFX v2 service other than the query
// service reports that service's 404 through friendlyAPIError, which gives it
// accessDeniedMessage.
func lookupNotVisibleMessage(label, uid string) string {
	return fmt.Sprintf("Error: %s. Check the UID, or ask someone with access to confirm it.", notVisibleText(label+" with UID "+uid))
}

// hasPageToken reports whether a pagination token is present and non-empty.
func hasPageToken(p *string) bool {
	return p != nil && *p != ""
}

// searchWarnings returns the warnings for one page of a query-backed search
// tool, for its top-level warnings key. The query service returns only the
// records the caller can view, and by design an empty page looks the same
// whether nothing matched or nothing matching is visible; it also walks past
// pages where the caller can see nothing, so an empty page with a token only
// means more raw pages remain. The warnings therefore state only what the
// caller can see and what to do next. They never count, or claim the
// existence of, records the caller cannot see.
//
// noun names the records ("meetings", "groups"); visible is the number of
// records on the page; pageSize is the effective page size requested;
// hasToken is whether the response carries a next-page token; continuation is
// whether the request itself carried a page token.
func searchWarnings(noun string, visible, pageSize int, hasToken, continuation bool) []string {
	switch {
	case visible > 0 && hasToken && visible < pageSize:
		return []string{fmt.Sprintf("This page lists only %s visible to you, so it can hold fewer than page_size; more pages remain: continue with page_token, increase page_size, or narrow your filters.", noun)}
	case visible == 0 && hasToken:
		return []string{fmt.Sprintf("This page lists no %s, but more pages remain: continue with page_token. Results cover only records you can view.", noun)}
	case visible == 0 && !continuation:
		return []string{fmt.Sprintf("No %s matching these filters are visible to you; results cover only records you can view, so this is not proof of absence.", noun)}
	}
	return nil
}
