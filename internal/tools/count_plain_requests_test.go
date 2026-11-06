// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plainCountForms are the count_lfx_resources argument shapes whose upstream
// request must not move when the tool's gate or type list changes: the type
// alone, the type with a parent ref, and the type with tags.
var plainCountForms = []struct {
	name string
	args func(resourceType string) CountLFXResourcesArgs
}{
	{"type", func(resourceType string) CountLFXResourcesArgs {
		return CountLFXResourcesArgs{Type: resourceType}
	}},
	{"type+parent", func(resourceType string) CountLFXResourcesArgs {
		return CountLFXResourcesArgs{Type: resourceType, Parent: "project:P1"}
	}},
	{"type+tags", func(resourceType string) CountLFXResourcesArgs {
		return CountLFXResourcesArgs{Type: resourceType, Tags: []string{"project_uid:P1", "project_slug:example"}}
	}},
}

// plainCountRequestLog runs every plain form over every countable type as a
// caller with full view and returns one line per call: the form, the type
// and the count request's query string, byte for byte as it was sent.
func plainCountRequestLog(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	for _, resourceType := range countableResourceTypes {
		for _, form := range plainCountForms {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count": 1, "has_more": false}`)
			res, _, err := handleCountLFXResources(fullViewCtx(), stubCallToolRequest(), form.args(resourceType))
			if err != nil || res == nil || res.IsError {
				t.Fatalf("%s %s: unexpected failure: %v %s", form.name, resourceType, err, allResultText(t, res))
			}
			reqs := api.RequestsTo(countPath)
			if len(reqs) != 1 {
				t.Fatalf("%s %s: expected one count request, got %d", form.name, resourceType, len(reqs))
			}
			sb.WriteString(form.name + " " + resourceType + " " + reqs[0].Method + " " + reqs[0].Path + "?" + reqs[0].RawQuery + "\n")
		}
	}
	return sb.String()
}

// TestCountLFXResources_PlainRequestsMatchMain pins the count request each
// plain form sends, per type, to the bytes captured from the handler at
// origin/main before the attachment types joined the tool. A type added
// later gets its lines appended to the golden with the same shape; a line
// already there never changes. The golden is
// testdata/count_plain_requests.golden; regenerate it only from a checkout
// whose behaviour is the one being pinned.
func TestCountLFXResources_PlainRequestsMatchMain(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "count_plain_requests.golden"))
	if err != nil {
		t.Fatalf("golden missing: %v", err)
	}
	if got := plainCountRequestLog(t); got != string(want) {
		t.Errorf("plain count requests differ from the pinned bytes:\n--- want\n%s--- got\n%s", want, got)
	}
}
