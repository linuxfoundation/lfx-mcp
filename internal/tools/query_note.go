// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import "fmt"

// accessFilteredEmptyNote explains an empty page from any query-service-backed
// search tool. Most indexed resource types are access-controlled and the query
// service silently withholds resources the caller cannot see, so an empty page
// is ambiguous - agents have read it as "the record does not exist". The
// pagination token partially disambiguates: it is derived from the raw index
// page before access filtering (the query service's documented
// post-page-shrinkage pagination) and emitted only when that raw page was
// full, so an empty page WITH a token proves matches exist that the caller
// lacks permission to view. The converse does not hold: a final, partially
// filled raw page emits no token even if every record on it was withheld, so
// an empty page without a token cannot prove nothing matched - it can only be
// read as "nothing visible". The wording of both notes reflects exactly what
// each case proves. (The query service's withheld_count field, once deployed,
// will resolve the ambiguity positively.) Returns "" for non-empty pages.
func accessFilteredEmptyNote(what string, count int, hasPageToken bool) string {
	if count > 0 {
		return ""
	}
	if hasPageToken {
		return fmt.Sprintf("No %s are visible to your identity, but matching records exist: "+
			"you do not have permission to view them. These records are access-controlled; "+
			"this is a permissions gap, not missing data.", what)
	}
	return fmt.Sprintf("No %s are visible to your identity. Results are limited to records "+
		"you have permission to view, so matching records you cannot see may still exist - "+
		"treat this as no visible matches, not proof of absence.", what)
}
