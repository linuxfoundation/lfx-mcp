// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// attachmentDoc is a synthetic attachment index document of the given type
// carrying every field the meeting service publishes except file_url, which
// no caller gets back (attachmentDocWithFileURL adds it). The three user
// references carry e-mails; none is the caller's.
func attachmentDoc(resourceType, uid string) string {
	return `{
  "type": "` + resourceType + `",
  "id": "` + uid + `",
  "data": {
    "uid": "` + uid + `",
    "meeting_id": "77777777777",
    "meeting_and_occurrence_id": "77777777777-1771596000000",
    "project_uid": "11111111-1111-1111-1111-111111111111",
    "project_slug": "example",
    "type": "file",
    "category": "agenda",
    "link": "",
    "name": "Agenda",
    "description": "Agenda for the weekly sync",
    "source": "upload",
    "file_name": "agenda.pdf",
    "file_size": 2048,
    "file_uploaded": true,
    "file_upload_status": "completed",
    "file_content_type": "application/pdf",
    "file_uploaded_by": {"user_id": "u3", "username": "uploader", "email": "uploader@example.test", "name": "Uploader"},
    "file_uploaded_at": "2026-06-01T10:00:00Z",
    "committees": [{"uid": "66666666-6666-4666-8666-666666666666", "allowed_voting_statuses": ["Voting Rep"]}],
    "created_at": "2026-06-01T09:00:00Z",
    "modified_at": "2026-06-02T09:00:00Z",
    "created_by": {"user_id": "u1", "username": "creator", "email": "creator@example.test", "name": "Creator"},
    "updated_by": {"user_id": "u2", "username": "editor", "email": "editor@example.test", "name": "Editor"}
  }
}`
}

// attachmentDocWithFileURL is attachmentDoc with the storage key the index
// publishes as file_url.
func attachmentDocWithFileURL(resourceType, uid string) string {
	doc := attachmentDoc(resourceType, uid)
	return strings.Replace(doc, `"file_name": "agenda.pdf",`, `"file_name": "agenda.pdf",
    "file_url": "meetings/77777777777/attachments/`+uid+`/agenda.pdf",`, 1)
}

// attachmentPeopleFields are the user references trimmed without full view.
var attachmentPeopleFields = []string{"created_by", "updated_by", "file_uploaded_by"}

// attachmentSearchRequest runs one search as the given caller against a stub
// answering with the given page and returns the result and the one query
// request it made.
func attachmentSearchRequest(ctx context.Context, t *testing.T, pageBody string, args SearchMeetingAttachmentsArgs) (*mcp.CallToolResult, resourceSearchResult, stubAPIRequest) {
	t.Helper()
	api := setupMeetingLookupTest(t)
	api.Respond(resourcesPath, pageBody)
	res, out, err := handleSearchMeetingAttachments(ctx, stubCallToolRequest(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	reqs := api.RequestsTo(resourcesPath)
	if len(reqs) != 1 {
		t.Fatalf("expected one query request, got %d", len(reqs))
	}
	assertExchangedAuth(t, reqs[0])
	return res, out, reqs[0]
}

// TestSearchMeetingAttachments_PayloadMapping pins, per scope form, the
// query request the tool sends: the record type follows meeting_kind, the
// scope is a parent ref, the type filter is one tags_all entry, and name,
// sort, page_size and page_token pass through.
func TestSearchMeetingAttachments_PayloadMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		args SearchMeetingAttachmentsArgs
		want map[string][]string
	}{
		{
			"upcoming meeting",
			SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777", Type: "file", Name: "agenda", Sort: "updated_desc", PageSize: 25, PageToken: "tok"},
			map[string][]string{"type": {"v1_meeting_attachment"}, "parent": {"meeting:77777777777"}, "tags_all": {"type:file"}, "name": {"agenda"}, "sort": {"updated_desc"}, "page_size": {"25"}, "page_token": {"tok"}},
		},
		{
			"past meeting",
			SearchMeetingAttachmentsArgs{MeetingKind: "past", PastMeetingID: "77777777777-1771596000000", Type: "link", Name: "minutes"},
			map[string][]string{"type": {"v1_past_meeting_attachment"}, "parent": {"past_meeting:77777777777-1771596000000"}, "tags_all": {"type:link"}, "name": {"minutes"}, "sort": {"name_asc"}, "page_size": {"10"}},
		},
		{
			"project, upcoming kind",
			SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", ProjectUID: "P1", Type: "file", Name: "agenda"},
			map[string][]string{"type": {"v1_meeting_attachment"}, "parent": {"project:P1"}, "tags_all": {"type:file"}, "name": {"agenda"}, "sort": {"name_asc"}, "page_size": {"10"}},
		},
		{
			"project, past kind",
			SearchMeetingAttachmentsArgs{MeetingKind: "past", ProjectUID: "P1", Type: "link", Name: "minutes"},
			map[string][]string{"type": {"v1_past_meeting_attachment"}, "parent": {"project:P1"}, "tags_all": {"type:link"}, "name": {"minutes"}, "sort": {"name_asc"}, "page_size": {"10"}},
		},
		{
			"meeting id wins over project",
			SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777", ProjectUID: "P1"},
			map[string][]string{"type": {"v1_meeting_attachment"}, "parent": {"meeting:77777777777"}, "sort": {"name_asc"}, "page_size": {"10"}},
		},
		{
			"past meeting id wins over project",
			SearchMeetingAttachmentsArgs{MeetingKind: "past", PastMeetingID: "77777777777-1771596000000", ProjectUID: "P1"},
			map[string][]string{"type": {"v1_past_meeting_attachment"}, "parent": {"past_meeting:77777777777-1771596000000"}, "sort": {"name_asc"}, "page_size": {"10"}},
		},
		{
			"non-positive page_size defaults",
			SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", ProjectUID: "P1", PageSize: -3},
			map[string][]string{"type": {"v1_meeting_attachment"}, "parent": {"project:P1"}, "sort": {"name_asc"}, "page_size": {"10"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, r := attachmentSearchRequest(fullViewCtx(), t, page(nil, ""), tc.args)
			if r.Method != http.MethodGet || r.Path != resourcesPath {
				t.Fatalf("expected GET %s, got %s %s", resourcesPath, r.Method, r.Path)
			}
			tc.want["v"] = []string{"1"}
			for k, v := range tc.want {
				if got := r.Query[k]; !reflect.DeepEqual(got, v) {
					t.Errorf("query %s: want %v, got %v", k, v, got)
				}
			}
			for k := range r.Query {
				if _, ok := tc.want[k]; !ok {
					t.Errorf("unexpected query param %s=%v", k, r.Query[k])
				}
			}
		})
	}
}

// TestSearchMeetingAttachments_TypeFilterIsANDed pins that the type filter
// narrows the scope: it travels as tags_all, which the query service ANDs
// with the parent, never as tags.
func TestSearchMeetingAttachments_TypeFilterIsANDed(t *testing.T) {
	_, _, r := attachmentSearchRequest(context.Background(), t, page(nil, ""), SearchMeetingAttachmentsArgs{MeetingKind: "past", ProjectUID: "P1", Type: "link"})
	if got := r.Query["tags_all"]; !reflect.DeepEqual(got, []string{"type:link"}) {
		t.Errorf("type must travel as one tags_all entry, got %v", got)
	}
	if _, has := r.Query["tags"]; has {
		t.Errorf("type must never travel as tags: %v", r.Query["tags"])
	}
	// Without a type, no tag filter at all.
	_, _, r = attachmentSearchRequest(context.Background(), t, page(nil, ""), SearchMeetingAttachmentsArgs{MeetingKind: "past", ProjectUID: "P1"})
	if _, has := r.Query["tags_all"]; has {
		t.Errorf("no type, no tags_all: %v", r.Query["tags_all"])
	}
}

// TestSearchMeetingAttachments_RequestIsTheSameWithoutFullView pins that the
// view changes nothing upstream: the request bytes of a caller without full
// view equal those of a caller with it.
func TestSearchMeetingAttachments_RequestIsTheSameWithoutFullView(t *testing.T) {
	args := SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777", Type: "file", Name: "agenda", PageSize: 5}
	_, _, full := attachmentSearchRequest(fullViewCtx(), t, page(nil, ""), args)
	_, _, bare := attachmentSearchRequest(context.Background(), t, page(nil, ""), args)
	if full.RawQuery != bare.RawQuery {
		t.Errorf("request differs by view:\nfull %s\nbare %s", full.RawQuery, bare.RawQuery)
	}
}

// TestSearchMeetingAttachments_Refusals pins every argument set the tool
// refuses before any upstream call, with the text naming the allowed form.
func TestSearchMeetingAttachments_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args SearchMeetingAttachmentsArgs
		want string
	}{
		{"no scope, upcoming", SearchMeetingAttachmentsArgs{MeetingKind: "upcoming"}, attachmentScopeRefusal},
		{"no scope, past", SearchMeetingAttachmentsArgs{MeetingKind: "past", Type: "file", Name: "x"}, attachmentScopeRefusal},
		{"no kind", SearchMeetingAttachmentsArgs{MeetingID: "77777777777"}, attachmentKindRefusal},
		{"unknown kind", SearchMeetingAttachmentsArgs{MeetingKind: "recurring", ProjectUID: "P1"}, attachmentKindRefusal},
		{"meeting_id with past", SearchMeetingAttachmentsArgs{MeetingKind: "past", MeetingID: "77777777777"}, attachmentPastIDRefusal},
		{"meeting_id with past and project", SearchMeetingAttachmentsArgs{MeetingKind: "past", MeetingID: "77777777777", ProjectUID: "P1"}, attachmentPastIDRefusal},
		{"past_meeting_id with upcoming", SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", PastMeetingID: "77777777777-1771596000000"}, attachmentUpcomingIDRefusal},
		{"both ids, upcoming", SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777", PastMeetingID: "77777777777-1771596000000"}, attachmentUpcomingIDRefusal},
		{"both ids, past", SearchMeetingAttachmentsArgs{MeetingKind: "past", MeetingID: "77777777777", PastMeetingID: "77777777777-1771596000000"}, attachmentPastIDRefusal},
		{"bad type", SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777", Type: "video"}, attachmentTypeRefusal},
		{"bad type, project scope", SearchMeetingAttachmentsArgs{MeetingKind: "past", ProjectUID: "P1", Type: "File"}, attachmentTypeRefusal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMeetingLookupTest(t)
			for _, ctx := range []context.Context{fullViewCtx(), context.Background()} {
				res, out, err := handleSearchMeetingAttachments(ctx, stubCallToolRequest(), tc.args)
				if err == nil || err.Error() != tc.want {
					t.Errorf("want error %q, got %v", tc.want, err)
				}
				if res != nil || !reflect.DeepEqual(out, resourceSearchResult{}) {
					t.Errorf("a refusal carries no result or output: %v %+v", res, out)
				}
			}
			if n := len(api.Requests()); n != 0 {
				t.Errorf("a refusal reaches no upstream, got %d requests: %+v", n, api.Requests())
			}
			for _, banned := range []string{"@example.test", "exist", "withheld", "permission"} {
				if strings.Contains(tc.want, banned) {
					t.Errorf("refusal must not say %q: %s", banned, tc.want)
				}
			}
		})
	}
}

// TestSearchMeetingAttachments_RefusalsOverTheWire pins that a refusal is an
// error result with no structured content, so it cannot read as an empty
// page; the SDK rejects a missing meeting_kind from the published schema
// before the handler runs, and the handler rejects an unknown one.
func TestSearchMeetingAttachments_RefusalsOverTheWire(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"no scope", map[string]any{"meeting_kind": "upcoming"}, attachmentScopeRefusal},
		{"bad type", map[string]any{"meeting_kind": "past", "project_uid": "P1", "type": "video"}, attachmentTypeRefusal},
		{"missing kind", map[string]any{"meeting_id": "77777777777"}, "meeting_kind"},
		{"unknown kind", map[string]any{"meeting_kind": "recurring", "project_uid": "P1"}, attachmentKindRefusal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMeetingLookupTest(t)
			_, res := callToolWithToken(t, "search_meeting_attachments", RegisterSearchMeetingAttachments, tc.args)
			if !res.IsError {
				t.Fatalf("expected an error result, got %s", allResultText(t, res))
			}
			if res.StructuredContent != nil {
				t.Errorf("an error result must carry no structured content, got %v", res.StructuredContent)
			}
			if text := allResultText(t, res); !strings.Contains(text, tc.want) {
				t.Errorf("error text must name the problem (%q), got %q", tc.want, text)
			}
			if n := len(api.Requests()); n != 0 {
				t.Errorf("a refusal reaches no upstream, got %d requests", n)
			}
		})
	}
}

// resourceDataOf returns the Data of the one resource on a result page.
func resourceDataOf(t *testing.T, out resourceSearchResult) map[string]any {
	t.Helper()
	if len(out.Resources) != 1 {
		t.Fatalf("expected one resource, got %d", len(out.Resources))
	}
	return out.Resources[0].Data
}

// TestSearchMeetingAttachments_FileURLIsNeverReturned pins that file_url,
// the upload's storage key, is removed for every caller and from both
// record types, while the rest of the record stays for a caller with full
// view.
func TestSearchMeetingAttachments_FileURLIsNeverReturned(t *testing.T) {
	for _, tc := range []struct {
		name string
		args SearchMeetingAttachmentsArgs
		typ  string
	}{
		{"upcoming", SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777"}, meetingAttachmentResourceType},
		{"past", SearchMeetingAttachmentsArgs{MeetingKind: "past", PastMeetingID: "77777777777-1771596000000"}, pastMeetingAttachmentResourceType},
	} {
		for _, view := range []struct {
			name string
			ctx  context.Context
		}{{"full view", fullViewCtx()}, {"no full view", context.Background()}} {
			t.Run(tc.name+" "+view.name, func(t *testing.T) {
				res, out, _ := attachmentSearchRequest(view.ctx, t, page([]string{attachmentDocWithFileURL(tc.typ, "att-1")}, ""), tc.args)
				data := resourceDataOf(t, out)
				if _, has := data["file_url"]; has {
					t.Errorf("file_url must be removed, got %v", data["file_url"])
				}
				if strings.Contains(allResultText(t, res), "attachments/att-1/agenda.pdf") {
					t.Errorf("the storage key reached the text block:\n%s", allResultText(t, res))
				}
				for _, key := range []string{"uid", "name", "type", "link", "file_name", "file_size", "file_content_type", "file_upload_status", "file_uploaded", "file_uploaded_at", "description", "category", "source", "committees", "created_at", "modified_at", "meeting_id", "project_uid", "project_slug"} {
					if _, has := data[key]; !has {
						t.Errorf("%s must stay", key)
					}
				}
			})
		}
	}
}

// TestSearchMeetingAttachments_PeopleFieldsFollowFullView pins the people
// trim: without full view the three user references are removed and no
// e-mail reaches the caller; with full view the record is returned as the
// index holds it, minus file_url.
func TestSearchMeetingAttachments_PeopleFieldsFollowFullView(t *testing.T) {
	for _, tc := range []struct {
		name string
		args SearchMeetingAttachmentsArgs
		typ  string
	}{
		{"upcoming", SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", ProjectUID: "P1"}, meetingAttachmentResourceType},
		{"past", SearchMeetingAttachmentsArgs{MeetingKind: "past", ProjectUID: "P1"}, pastMeetingAttachmentResourceType},
	} {
		t.Run(tc.name+" without full view", func(t *testing.T) {
			res, out, _ := attachmentSearchRequest(context.Background(), t, page([]string{attachmentDocWithFileURL(tc.typ, "att-1")}, ""), tc.args)
			assertNoEmail(t, allResultText(t, res))
			data := resourceDataOf(t, out)
			for _, key := range attachmentPeopleFields {
				if _, has := data[key]; has {
					t.Errorf("%s must be removed without full view", key)
				}
			}
			if data["link"] == nil || data["name"] != "Agenda" || data["file_name"] != "agenda.pdf" {
				t.Errorf("the attachment's own fields stay: %v", data)
			}
		})
		t.Run(tc.name+" with full view", func(t *testing.T) {
			res, out, _ := attachmentSearchRequest(fullViewCtx(), t, page([]string{attachmentDocWithFileURL(tc.typ, "att-1")}, ""), tc.args)
			data := resourceDataOf(t, out)
			for _, key := range attachmentPeopleFields {
				if _, has := data[key]; !has {
					t.Errorf("%s must stay with full view", key)
				}
			}
			text := allResultText(t, res)
			for _, email := range []string{"creator@example.test", "editor@example.test", "uploader@example.test"} {
				if !strings.Contains(text, email) {
					t.Errorf("full view must keep %s", email)
				}
			}
			// Golden: the fixture minus file_url, nothing else changed.
			var fixture struct {
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal([]byte(attachmentDocWithFileURL(tc.typ, "att-1")), &fixture); err != nil {
				t.Fatal(err)
			}
			delete(fixture.Data, "file_url")
			if !reflect.DeepEqual(data, fixture.Data) {
				t.Errorf("full view must return the record minus file_url:\nwant %v\ngot  %v", fixture.Data, data)
			}
		})
	}
}

// TestSearchMeetingAttachments_MissingFlagBehavesAsNoFullView pins that a
// handler reached without the view middleware narrows rather than widens.
func TestSearchMeetingAttachments_MissingFlagBehavesAsNoFullView(t *testing.T) {
	res, _, _ := attachmentSearchRequest(context.Background(), t, page([]string{attachmentDoc(meetingAttachmentResourceType, "att-1")}, ""), SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777"})
	assertNoEmail(t, allResultText(t, res))
}

// TestSearchMeetingAttachments_NonMapDataIsLeftAlone pins that a record
// whose Data is not an object passes through the trims under _raw.
func TestSearchMeetingAttachments_NonMapDataIsLeftAlone(t *testing.T) {
	_, out, _ := attachmentSearchRequest(context.Background(), t, `{"resources": [{"type": "v1_meeting_attachment", "id": "att-1", "data": ["odd"]}]}`, SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777"})
	if got := resourceDataOf(t, out)["_raw"]; !reflect.DeepEqual(got, []any{"odd"}) {
		t.Errorf("non-object data must be kept under _raw, got %v", got)
	}
}

// TestSearchMeetingAttachments_Warnings pins the shared search-warning
// contract: an empty first page carries the not-proof-of-absence warning, an
// empty continuation page none, a page with a token the continue warning, and
// the text block equals the structured output.
func TestSearchMeetingAttachments_Warnings(t *testing.T) {
	args := SearchMeetingAttachmentsArgs{MeetingKind: "past", ProjectUID: "P1"}

	res, out, _ := attachmentSearchRequest(context.Background(), t, page(nil, ""), args)
	if got := textWarnings(t, res, out.Warnings); !reflect.DeepEqual(got, searchWarnings("meeting attachments", 0, 10, false, false)) {
		t.Errorf("empty first page: got %q", got)
	}

	cont := args
	cont.PageToken = "prev"
	res, out, _ = attachmentSearchRequest(context.Background(), t, page(nil, ""), cont)
	textWarnings(t, res, out.Warnings)
	if _, has := resultJSON(t, res)["warnings"]; has {
		t.Errorf("an empty continuation page carries no warnings key: %s", allResultText(t, res))
	}

	res, out, _ = attachmentSearchRequest(context.Background(), t, page(nil, "next"), cont)
	if got := textWarnings(t, res, out.Warnings); !reflect.DeepEqual(got, searchWarnings("meeting attachments", 0, 10, true, false)) || got == nil {
		t.Errorf("empty continuation page with a token: got %q", got)
	}

	sized := args
	sized.PageSize = 3
	res, out, _ = attachmentSearchRequest(context.Background(), t, page([]string{attachmentDoc(pastMeetingAttachmentResourceType, "att-1")}, "next"), sized)
	if got := textWarnings(t, res, out.Warnings); !reflect.DeepEqual(got, searchWarnings("meeting attachments", 1, 3, true, false)) || got == nil {
		t.Errorf("partial page: got %q", got)
	}
	if out.PageToken != "next" {
		t.Errorf("page token must be passed through, got %q", out.PageToken)
	}
}

// TestSearchMeetingAttachments_UpstreamErrorIsFriendly pins the upstream
// status mapping and that a failed call publishes no structured content.
func TestSearchMeetingAttachments_UpstreamErrorIsFriendly(t *testing.T) {
	api := setupMeetingLookupTest(t)
	api.RespondStatus(resourcesPath, http.StatusForbidden, `{"message":"forbidden"}`)
	res, out, err := handleSearchMeetingAttachments(context.Background(), stubCallToolRequest(), SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777"})
	if err == nil || !strings.Contains(err.Error(), accessDeniedMessage) {
		t.Errorf("403 must map to the access-denied wording, got %v", err)
	}
	if res != nil || !reflect.DeepEqual(out, resourceSearchResult{}) {
		t.Errorf("a failure carries no result or output: %v %+v", res, out)
	}
}

// TestSearchMeetingAttachments_NotConfigured pins the configuration guard.
func TestSearchMeetingAttachments_NotConfigured(t *testing.T) {
	prev := meetingConfig
	meetingConfig = nil
	t.Cleanup(func() { meetingConfig = prev })
	_, _, err := handleSearchMeetingAttachments(context.Background(), stubCallToolRequest(), SearchMeetingAttachmentsArgs{MeetingKind: "upcoming", MeetingID: "77777777777"})
	if err == nil || err.Error() != "Error: meeting tools not configured" {
		t.Errorf("want the not-configured error, got %v", err)
	}
}

// TestSearchMeetingAttachments_Description pins the tool's name, title,
// read-only hint, meeting_kind as the one required parameter, the words the
// description must and must not carry, and its byte size under the schema
// budget. The tool has no terminology mode: its one registration serves
// both. The schema generator in use publishes the whole jsonschema tag as
// the description (an enum= clause would leak as text, not as a schema
// enum), so the kind's two values are stated in the text and enforced by
// the handler.
func TestSearchMeetingAttachments_Description(t *testing.T) {
	tool := listRegisteredTool(t, "search_meeting_attachments", RegisterSearchMeetingAttachments)
	t.Logf("search_meeting_attachments description: %d UTF-8 bytes", len(tool.Description))
	if n := len(tool.Description); n > schemaDescriptionBudget {
		t.Errorf("description is %d bytes, budget %d", n, schemaDescriptionBudget)
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.Title != "Search Meeting Attachments" {
		t.Errorf("annotations must be read-only with the title, got %+v", tool.Annotations)
	}
	for _, want := range []string{"meeting_kind is required", "upcoming (set meeting_id)", "past (set past_meeting_id", "set project_uid to list the attachments of every meeting of that kind in the project", "type (file or link)", "meetings you can view, as LFX Self Serve shows them", "download links are not included"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description missing %q", want)
		}
	}
	for _, banned := range []string{"Insights", "%", "staff", "committee", "group", "document", "Documents"} {
		if strings.Contains(tool.Description, banned) {
			t.Errorf("description must not contain %q", banned)
		}
	}
	if got := schemaRequired(t, tool); !reflect.DeepEqual(got, []string{"meeting_kind"}) {
		t.Errorf("meeting_kind is the one required parameter, got %v", got)
	}
	for property, want := range map[string]string{
		"meeting_kind":    "(required) upcoming or past",
		"meeting_id":      "Upcoming meeting ID (meeting_kind=upcoming)",
		"past_meeting_id": "Past meeting ID, the meeting_and_occurrence_id value, e.g. 91461158520-1771596000000 (meeting_kind=past)",
		"project_uid":     "Project UID; lists attachments of all meetings of that kind in the project (ignored when meeting_id or past_meeting_id is set)",
		"type":            "file or link",
	} {
		if got := schemaPropertyDescription(t, tool, property); got != want {
			t.Errorf("%s: want %q, got %q", property, want, got)
		}
	}
	total := 0
	for _, property := range schemaProperties(t, tool) {
		total += len(schemaPropertyDescription(t, tool, property))
	}
	t.Logf("search_meeting_attachments parameter descriptions: %d UTF-8 bytes in total, %d on meeting_kind", total, len(schemaPropertyDescription(t, tool, "meeting_kind")))
}
