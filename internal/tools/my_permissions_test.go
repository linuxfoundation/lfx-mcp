// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	lfxauth "github.com/linuxfoundation/lfx-mcp/internal/auth"
	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func queueGrants(api *stubLFXAPI, grants ...string) {
	body, _ := json.Marshal(map[string]any{"grants": grants})
	api.Respond("/my-grants", string(body))
}

func TestMyPermissions_UnionNamesOrderDedupe(t *testing.T) {
	api := setupProjectTest(t)
	queueGrants(api, "project:z#writer@user:alice", "project:a#owner@user:alice", "project:a#auditor@user:alice", "project:a#owner@user:alice", "committee:wrong#member@user:alice", "malformed")
	queueGrants(api, "committee:c#member@user:alice")
	queueGrants(api, "project_membership:m#key_contact@user:alice")
	api.Respond(resourcesPath, page([]string{projectDoc("z", "z", "A name", "", ""), projectDoc("a", "a", "Z name", "", "")}, ""))
	api.Respond(resourcesPath, page([]string{`{"type":"committee","id":"c","data":{"uid":"c","name":"Council"}}`}, ""))

	res, out, err := handleListMyPermissions(context.Background(), stubCallToolRequest(), true)
	if err != nil {
		t.Fatal(err)
	}
	want := []myPermission{{"project", "z", "writer", "A name"}, {"project", "a", "auditor", "Z name"}, {"project", "a", "owner", "Z name"}, {"committee", "c", "member", "Council"}, {"project_membership", "m", "key_contact", ""}}
	if !reflect.DeepEqual(out.Permissions, want) {
		t.Fatalf("permissions=%+v, want %+v", out.Permissions, want)
	}
	if len(out.Warnings) != 0 || !strings.Contains(out.Coverage, "groups") {
		t.Fatalf("unexpected result: %+v", out)
	}
	assertPermissionsText(t, res, out)
	grants := api.RequestsTo("/my-grants")
	if len(grants) != 3 {
		t.Fatalf("got %d grant requests, want three", len(grants))
	}
	for i, typ := range []string{"project", "committee", "project_membership"} {
		r := grants[i]
		assertExchangedAuth(t, r)
		if r.Method != http.MethodGet || r.Query.Get("object_type") != typ || r.Query.Get("v") != "1" {
			t.Errorf("unexpected request: %+v", r)
		}
	}
	names := api.RequestsTo(resourcesPath)
	if len(names) != 2 {
		t.Fatalf("names calls=%d, want 2", len(names))
	}
	for _, r := range names {
		assertExchangedAuth(t, r)
	}
	if !reflect.DeepEqual(names[0].Query["filters_or"], []string{"uid:a", "uid:z"}) {
		t.Errorf("not a UID union: %v", names[0].Query)
	}
}

func assertPermissionsText(t *testing.T, res *mcp.CallToolResult, out myPermissionsResult) {
	t.Helper()
	if res == nil || res.IsError || len(res.Content) != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	want, _ := json.MarshalIndent(out, "", "  ")
	if !ok || text.Text != string(want) {
		t.Fatalf("text must equal indented structured output: %+v", res.Content)
	}
}

func TestMyPermissions_EmptyAndWireContract(t *testing.T) {
	for _, groups := range []bool{false, true} {
		t.Run(fmt.Sprint(groups), func(t *testing.T) {
			api := setupProjectTest(t)
			for range 3 {
				queueGrants(api)
			}
			tool, res := callToolWithToken(t, "list_my_permissions", func(s *mcp.Server) { RegisterListMyPermissions(s, groups) }, map[string]any{})
			if res.IsError {
				t.Fatalf("unexpected tool error: %+v", res)
			}
			if !schemaHasProperties(t, tool.OutputSchema, "permissions", "coverage", "warnings") {
				t.Fatalf("missing output schema: %v", tool.OutputSchema)
			}
			if len(tool.Description) > 2048 || tool.Annotations.Title != "List My Permissions" || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint != nil {
				t.Fatalf("bad tool surface: %+v", tool)
			}
			label := "committees"
			if groups {
				label = "groups"
			}
			if !strings.Contains(tool.Description, "projects, "+label+" and project memberships") || !strings.Contains(tool.Description, "a missing entry does not mean you lack access") {
				t.Fatalf("bad description: %s", tool.Description)
			}
			encoded, _ := json.Marshal(res.StructuredContent)
			var out myPermissionsResult
			if err := json.Unmarshal(encoded, &out); err != nil {
				t.Fatal(err)
			}
			if out.Permissions == nil || len(out.Permissions) != 0 || out.Coverage != fmt.Sprintf(myPermissionsCoverage, label) || len(out.Warnings) != 0 {
				t.Fatalf("bad empty result: %s", encoded)
			}
			assertPermissionsText(t, res, out)
			if len(api.Requests()) != 3 {
				t.Fatalf("empty grants must not query names: %v", api.Requests())
			}
		})
	}
}

func TestMyPermissions_PartialFailureStatusMapping(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			api := setupProjectTest(t)
			queueGrants(api, "project:p#writer@user:alice")
			api.RespondStatus("/my-grants", status, "private upstream detail")
			queueGrants(api)
			api.Respond(resourcesPath, page(nil, ""))
			_, out, err := handleListMyPermissions(context.Background(), stubCallToolRequest(), true)
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Permissions) != 1 || len(out.Warnings) != 1 {
				t.Fatalf("bad partial result: %+v", out)
			}
			wantDetail := fmt.Sprintf("my-grants returned status %d: private upstream detail", status)
			switch status {
			case 401:
				wantDetail = unauthorizedMessage
			case 403, 404:
				wantDetail = accessDeniedMessage
			}
			if out.Warnings[0] != "Could not list your group roles: "+wantDetail {
				t.Errorf("warning=%q", out.Warnings[0])
			}
		})
	}
}

func TestMyPermissions_AllFailHasNoStructuredSuccess(t *testing.T) {
	for _, statuses := range [][]int{{401, 401, 401}, {403, 403, 403}, {404, 404, 404}, {403, 404, 403}, {500, 500, 500}, {401, 403, 500}, {400, 500, 503}} {
		t.Run(fmt.Sprint(statuses), func(t *testing.T) {
			api := setupProjectTest(t)
			for _, status := range statuses {
				api.RespondStatus("/my-grants", status, "private upstream detail")
			}
			_, res := callToolWithToken(t, "list_my_permissions", func(s *mcp.Server) { RegisterListMyPermissions(s, false) }, map[string]any{})
			if !res.IsError || res.StructuredContent != nil {
				t.Fatalf("must be tool error without structured content: %+v", res)
			}
			text := strings.TrimSpace(allResultText(t, res))
			details := make([]string, len(statuses))
			for i, status := range statuses {
				switch status {
				case 401:
					details[i] = unauthorizedMessage
				case 403, 404:
					details[i] = accessDeniedMessage
				default:
					details[i] = fmt.Sprintf("my-grants returned status %d: private upstream detail", status)
				}
			}
			want := "Failed to list your permissions: "
			if details[0] == details[1] && details[1] == details[2] {
				want += details[0]
			} else {
				want += "Could not list your project roles: " + details[0] + " Could not list your committee roles: " + details[1] + " Could not list your project membership roles: " + details[2]
			}
			if text != want {
				t.Errorf("error=%q, want %q", text, want)
			}
		})
	}
}

func TestMyPermissions_MachineAndAPIKeyDoNotCallUpstream(t *testing.T) {
	for _, extra := range []map[string]any{{lfxauth.MachineAccountExtraKey: true, "raw_token": stubMCPToken}, {lfxauth.APIKeyAuthExtraKey: true}} {
		api := setupProjectTest(t)
		// Queue successful empty responses: the test must fail if the guard is removed,
		// not accidentally pass on an unrelated upstream error.
		for range 3 {
			queueGrants(api)
		}
		req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{Extra: extra}}}
		_, _, err := handleListMyPermissions(context.Background(), req, false)
		if err == nil || !strings.Contains(err.Error(), "signed-in person") {
			t.Fatalf("machine not refused: %v", err)
		}
		if len(api.Requests()) != 0 {
			t.Fatalf("machine made upstream calls: %v", api.Requests())
		}
	}
}

func TestMyPermissions_StaticStdioToken(t *testing.T) {
	api := setupProjectTest(t)
	clients, err := lfxv2.NewClients(context.Background(), lfxv2.ClientConfig{APIDomain: api.server.URL, StaticLFXToken: "static-token"})
	if err != nil {
		t.Fatal(err)
	}
	projectConfig = &ProjectConfig{Clients: clients}
	queueGrants(api, "project:p#writer@user:alice")
	queueGrants(api)
	queueGrants(api)
	api.Respond(resourcesPath, page([]string{projectDoc("p", "p", "Project", "", "")}, ""))
	_, out, err := handleListMyPermissions(context.Background(), &mcp.CallToolRequest{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(api.Requests()) != 4 || len(out.Permissions) != 1 || out.Permissions[0].Name != "Project" {
		t.Fatalf("missing grants or names: %+v", out)
	}
	for _, r := range api.Requests() {
		if r.Authorization != "Bearer static-token" {
			t.Fatal("static token not used")
		}
	}
}

func TestMyPermissions_NamesUseDataUIDWithIDFallback(t *testing.T) {
	api := setupProjectTest(t)
	queueGrants(api, "project:p#writer@user:alice", "project:q#auditor@user:alice")
	queueGrants(api)
	queueGrants(api)
	api.Respond(resourcesPath, page([]string{
		`{"type":"project","id":"index-id","data":{"uid":"p","name":"Canonical UID"}}`,
		`{"type":"project","id":"project:q","data":{"name":"Fallback ID"}}`,
	}, ""))
	_, out, err := handleListMyPermissions(context.Background(), stubCallToolRequest(), false)
	if err != nil {
		t.Fatal(err)
	}
	want := []myPermission{{"project", "p", "writer", "Canonical UID"}, {"project", "q", "auditor", "Fallback ID"}}
	if !reflect.DeepEqual(out.Permissions, want) {
		t.Fatalf("got %+v, want %+v", out.Permissions, want)
	}
}

func TestMyPermissions_NamesAreBoundedAndOptional(t *testing.T) {
	api := setupProjectTest(t)
	var grants []string
	for i := 104; i >= 0; i-- {
		grants = append(grants, fmt.Sprintf("project:p%03d#writer@user:alice", i))
	}
	queueGrants(api, grants...)
	queueGrants(api, "committee:c#member@user:alice")
	queueGrants(api)
	api.Respond(resourcesPath, page([]string{projectDoc("p000", "a", "Visible name", "", ""), projectDoc("p104", "b", "Outside subset", "", ""), projectDoc("unrequested", "x", "Unrequested name", "", ""), `{"type":"committee","id":"p001","data":{"name":"Wrong type"}}`}, "more"))
	api.RespondStatus(resourcesPath, 403, "unavailable")
	res, out, err := handleListMyPermissions(context.Background(), stubCallToolRequest(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Permissions) != 106 {
		t.Fatalf("names failure lost grants: %d", len(out.Permissions))
	}
	named := 0
	for _, p := range out.Permissions {
		if p.Name != "" {
			named++
			if p.UID != "p000" || p.Name != "Visible name" {
				t.Fatalf("unexpected name: %+v", p)
			}
		}
	}
	if named != 1 {
		t.Errorf("named=%d", named)
	}
	// Spec order is name then UID within a type, including empty names.
	// A lookup failure may change names, but a given result is deterministic.
	if out.Permissions[0].UID != "p001" || out.Permissions[103].UID != "p104" || out.Permissions[104].UID != "p000" || out.Permissions[105].Type != "committee" {
		t.Errorf("partial-name ordering is wrong: first=%+v, last=%+v", out.Permissions[0], out.Permissions[103:])
	}
	if !strings.Contains(out.Coverage, "beyond the lookup limit") {
		t.Error("coverage must disclose best-effort bounded names")
	}
	names := api.RequestsTo(resourcesPath)
	if len(names) != 2 || len(names[0].Query["filters_or"]) != 100 || names[0].Query.Get("page_size") != "100" || names[0].Query.Get("page_token") != "" {
		t.Fatalf("unbounded name queries: %+v", names)
	}
	filters := names[0].Query["filters_or"]
	if filters[0] != "uid:p000" || filters[99] != "uid:p099" {
		t.Errorf("name lookup subset must be deterministic: %v", filters)
	}
	for i, typ := range []string{"project", "committee"} {
		if names[i].Query.Get("type") != typ || names[i].Query.Get("sort") != "name_asc" {
			t.Errorf("wrong name query type/sort: %+v", names[i])
		}
	}
	assertPermissionsText(t, res, out)
}
