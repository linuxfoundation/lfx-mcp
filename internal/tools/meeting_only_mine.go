// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// onlyMineNeedsLoginMessage is the error text when only_mine is asked for by a
// caller that is not a signed-in person. The query service answers
// filter_grants=direct from the tuples stored for the request's own
// principal; a machine or API-key principal holds no meeting registrations,
// so its answer would be an empty page that reads as "no meetings" rather
// than a refusal.
const onlyMineNeedsLoginMessage = "Error: only_mine lists the meetings registered to a signed-in person's LFX login; this caller is a machine or API-key identity, which has no meetings of its own. Leave only_mine unset, or call as the person."

// applyOnlyMine narrows a meeting or past-meeting search to the records the
// caller holds a direct grant on, the same query-service mechanism LFX Self
// Serve's My Meetings uses (filter_grants=direct): the service reads the
// tuples stored for the caller's principal on the record type and uses them
// as a pre-filter, then applies its usual access check. On v1_meeting those
// tuples are host and participant, written per registrant with an LFX
// username; on v1_past_meeting they are host, invitee and attendee, written
// per participant with a username. organizer is derived, never stored, so an
// organizer who is not a registrant is absent here, as in LFX Self Serve.
//
// A machine or API-key caller gets a tool error: the service would answer
// from the client's own, empty, grants. A request with no token at all
// (stdio) passes through; the service decides from the configured token.
func applyOnlyMine(payload *querysvc.QueryResourcesPayload, tokenInfo *auth.TokenInfo) error {
	if IsMachineAccount(tokenInfo) || IsAPIKeyCaller(tokenInfo) {
		return toolError(onlyMineNeedsLoginMessage)
	}
	direct := "direct"
	payload.FilterGrants = &direct
	return nil
}
