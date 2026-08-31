// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import "fmt"

// accessFilteredEmptyNote explains an empty page from any query-service-backed
// search tool. Most indexed resource types are access-controlled and the query
// service silently withholds resources the caller cannot see, so an empty page
// is ambiguous - agents have read it as "the record does not exist". The
// pagination token disambiguates: it is derived from the raw index page before
// access filtering (the query service's documented post-page-shrinkage
// pagination), so an empty page WITH a token means matches exist that the
// caller lacks permission to view, while an empty page without one means
// nothing matched on this page. Returns "" for non-empty pages.
func accessFilteredEmptyNote(what string, count int, hasPageToken bool) string {
	if count > 0 {
		return ""
	}
	if hasPageToken {
		return fmt.Sprintf("No %s are visible to your identity, but matching records exist: "+
			"you do not have permission to view them. These records are access-controlled; "+
			"this is a permissions gap, not missing data.", what)
	}
	return fmt.Sprintf("No %s matched. Note: results are limited to records "+
		"your identity has permission to view.", what)
}
