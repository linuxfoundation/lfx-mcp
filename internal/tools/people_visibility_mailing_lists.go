// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
//
// This file holds the people rule for mailing-list members (see
// people_visibility.go for the shared principles): a caller without full view
// is shown the members of a mailing list only when it manages the list
// (writer) or audits it (auditor) on groupsio_mailing_list:<uid>, and its own
// subscriptions on any list, matched by username or e-mail as meeting
// registrants and participants are. That is the platform's intended rule; LFX
// Self Serve's screen currently shows a public list's members to any
// signed-in user, so on this point the MCP server follows the intended rule
// ahead of the platform fix.
package tools

import (
	"context"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// mailingListMemberCountRefusal is the tool error refusing a groupsio_member
// count a caller without full view cannot be shown. It names the allowed
// form without countRefusal's reference to LFX Self Serve, because this rule
// is stricter than what Self Serve shows today.
const mailingListMemberCountRefusal = "Error: counting " + mailingListMemberResourceType + " is available only with parent=" + mailingListResourceType + ":<uid> for a mailing list you manage or audit."

// mailingListVisibilityUnavailableMessage is the fail-closed tool error of
// the mailing-list member rule, used in place of
// peopleVisibilityUnavailableMessage, which speaks of what LFX Self Serve
// shows: this rule is the platform's intended one, not today's screen.
const mailingListVisibilityUnavailableMessage = "Error: could not confirm which mailing lists you manage or audit; try again."

// mailingListLookupCapRefusal refuses a project-wide member search whose
// project has more mailing lists than the lookup reads.
const mailingListLookupCapRefusal = "Error: this project has too many mailing lists to check at once: set mailing_list_id."

// mailingListFilterCapRefusal refuses a project-wide member search whose
// lists shown to the caller exceed one filter clause.
const mailingListFilterCapRefusal = "Error: this project has too many mailing lists you manage or audit to search at once: set mailing_list_id."

// mailingListMemberListShown decides, for each mailing list UID, whether the
// caller is shown its members: writer or auditor on groupsio_mailing_list,
// checked as the caller in one access-check batch. Both relations are asked,
// because in the access model auditor does not include writer. Any failure
// is returned and the caller fails closed.
func mailingListMemberListShown(ctx context.Context, clients *lfxv2.Clients, listUIDs []string) (map[string]bool, error) {
	uids := dedupeStrings(listUIDs)
	shown := make(map[string]bool, len(uids))
	if len(uids) == 0 {
		return shown, nil
	}
	reqs := make([]string, 0, 2*len(uids))
	for _, uid := range uids {
		reqs = append(reqs, "groupsio_mailing_list:"+uid+"#writer", "groupsio_mailing_list:"+uid+"#auditor")
	}
	relations, err := clients.CheckRelations(ctx, reqs)
	if err != nil {
		return nil, err
	}
	for _, uid := range uids {
		shown[uid] = relations["groupsio_mailing_list:"+uid+"#writer"] || relations["groupsio_mailing_list:"+uid+"#auditor"]
	}
	return shown, nil
}

// projectMailingListUIDs returns the UIDs of the project's mailing lists
// visible to the caller, from the groupsio_mailing_list records whose parent
// is the project, read as the caller. It returns errPeopleVisibilityCap when
// the lookup needs more than peopleLookupMaxPages pages.
func projectMailingListUIDs(ctx context.Context, clients *lfxv2.Clients, projectUID string) ([]string, error) {
	resourceType := mailingListResourceType
	var uids []string
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= peopleLookupMaxPages {
			return nil, errPeopleVisibilityCap
		}
		result, err := clients.QuerySvc.QueryResources(ctx, &querysvc.QueryResourcesPayload{
			Version:   "1",
			Type:      &resourceType,
			Parent:    strPtr("project:" + projectUID),
			PageSize:  peopleLookupPageSize,
			Sort:      "name_asc",
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range result.Resources {
			uid := dataString(resourceData(r), "uid")
			if uid == "" && r != nil && r.ID != nil {
				// The query service's ID is the object ref,
				// groupsio_mailing_list:<uid>.
				uid = strings.TrimPrefix(*r.ID, mailingListResourceType+":")
			}
			if uid != "" {
				uids = append(uids, uid)
			}
		}
		if !hasPageToken(result.PageToken) {
			return dedupeStrings(uids), nil
		}
		pageToken = result.PageToken
	}
}

// mailingListMemberFilters is the filters_or clause that narrows a
// groupsio_member query to the members of the shown lists and to the
// caller's own records, own being participantNarrowing's identity terms. The
// query service ANDs the clause, as one OR group, with the query's tags and
// name, so it reads (a shown list) OR (the caller) within the given scope.
// It is empty when the caller is shown no list and carries no identity; the
// query is then not sent. ok is false when the clause would exceed
// peopleFilterChunk terms; the search is then refused rather than read
// unnarrowed.
func mailingListMemberFilters(uids []string, shown map[string]bool, own []string) (filters []string, ok bool) {
	for _, uid := range uids {
		if shown[uid] {
			filters = append(filters, "mailing_list_uid:"+uid)
		}
	}
	filters = append(filters, own...)
	if len(filters) > peopleFilterChunk {
		return nil, false
	}
	return filters, true
}

// filterMailingListMembers keeps, unchanged, the groupsio_member records of
// the lists the caller is shown and the caller's own records (isOwnRecord);
// any other record, including one without a mailing_list_uid, is not shown.
// It is the second check behind the narrowed query.
func filterMailingListMembers(resources []*querysvc.Resource, shown map[string]bool, tokenInfo *auth.TokenInfo) []*querysvc.Resource {
	out := make([]*querysvc.Resource, 0, len(resources))
	for _, r := range resources {
		data := resourceData(r)
		if shown[dataString(data, "mailing_list_uid")] || isOwnRecord(data, tokenInfo) {
			out = append(out, r)
		}
	}
	return out
}
