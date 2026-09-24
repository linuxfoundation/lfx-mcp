// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	meetingservice "github.com/linuxfoundation/lfx-v2-meeting-service/gen/meeting_service"
)

// meetingHostCredentialsResourceType is the query-service type the meeting
// service indexes meeting host keys under. It is a separate document type
// with its own, narrower access relation on the parent meeting, and no tool
// in this server reads it. It is spelled out here, in a test file, so that
// non-test source never needs to mention it.
const meetingHostCredentialsResourceType = "v1_meeting_host_credentials"

// TestHostCredentials_NotCountable pins that count_lfx_resources, the only
// tool whose query-service type comes from the caller, rejects the host
// credentials type before any upstream call.
func TestHostCredentials_NotCountable(t *testing.T) {
	if slices.Contains(countableResourceTypes, meetingHostCredentialsResourceType) {
		t.Fatalf("countableResourceTypes contains %q; meeting host credentials are not a countable type. "+
			"Adding them is a gating change that needs its own review, not a list edit", meetingHostCredentialsResourceType)
	}

	api := setupCountTest(t)
	res, _, err := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{
		Type:   meetingHostCredentialsResourceType,
		Parent: "meeting:00000000000",
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("count_lfx_resources accepted type %q; it must reject it", meetingHostCredentialsResourceType)
	}
	if !strings.Contains(allResultText(t, res), "is not countable") {
		t.Errorf("expected the not-countable error, got %q", allResultText(t, res))
	}
	if n := len(api.Requests()); n != 0 {
		t.Errorf("a rejected type must not reach the query service, got %d request(s)", n)
	}
}

// hostCredentialSourcePatterns match the ways non-test Go source could start
// reading meeting host credentials: the dedicated document type (and any
// identifier built on it) and the host key field. The meeting-service client
// calls whose response carries the host key, and those response types, are
// added by hostKeyProducerPatterns.
//
// The bare "host_key" field name is deliberately not matched: the meeting
// result field list names it, and that list is not what this guard relies on.
var hostCredentialSourcePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)host_?credential`),
	regexp.MustCompile(`\bHostKey\b`),
}

// hostKeyProducerPatterns returns a pattern for every meeting-service client
// method whose response carries a HostKey field, at any depth, and for every
// response type that declares one. They are derived from the pinned client by
// reflection, so a method or type added upstream is covered without editing
// this test.
func hostKeyProducerPatterns(t *testing.T) []*regexp.Regexp {
	t.Helper()
	typeNames := make(map[string]bool)
	var carries func(rt reflect.Type, seen map[reflect.Type]bool) bool
	carries = func(rt reflect.Type, seen map[reflect.Type]bool) bool {
		switch rt.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			return carries(rt.Elem(), seen)
		case reflect.Struct:
		default:
			return false
		}
		if seen[rt] {
			return false
		}
		seen[rt] = true
		found := false
		if _, ok := rt.FieldByName("HostKey"); ok {
			if rt.Name() != "" {
				typeNames[rt.Name()] = true
			}
			found = true
		}
		for i := 0; i < rt.NumField(); i++ {
			if carries(rt.Field(i).Type, seen) {
				found = true
			}
		}
		return found
	}

	var patterns []*regexp.Regexp
	client := reflect.TypeOf(&meetingservice.Client{})
	for i := 0; i < client.NumMethod(); i++ {
		m := client.Method(i)
		for j := 0; j < m.Type.NumOut(); j++ {
			if carries(m.Type.Out(j), make(map[reflect.Type]bool)) {
				patterns = append(patterns, regexp.MustCompile(`\b`+m.Name+`\b`))
				break
			}
		}
	}
	if len(patterns) == 0 {
		t.Fatal("no meeting-service client method returns a HostKey field; " +
			"the derivation is broken or the upstream contract changed, and the guard would miss the fetch path")
	}
	for name := range typeNames {
		patterns = append(patterns, regexp.MustCompile(`\b`+name+`\b`))
	}
	return patterns
}

// TestHostCredentials_NoSourceReference is a deliberately crude source scan.
// It exists so that a tool cannot start counting, searching or fetching
// meeting host credentials as a side effect of an unrelated change. If it
// fails, the change is adding a path to host credentials: that needs a
// deliberate gating decision in newServer (who may call it, under which
// scope and staff gate) and a reviewed update to this guard, not a tweak to
// the patterns.
func TestHostCredentials_NoSourceReference(t *testing.T) {
	patterns := append(slices.Clone(hostCredentialSourcePatterns), hostKeyProducerPatterns(t)...)
	roots := []string{".", filepath.Join("..", "..", "cmd")}
	scanned := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			for i, line := range strings.Split(string(src), "\n") {
				for _, re := range patterns {
					if re.MatchString(line) {
						t.Errorf("%s:%d refers to meeting host credentials (%s): %q\n"+
							"No tool may count, search or fetch meeting host credentials without a deliberate gating change; "+
							"see TestHostCredentials_NoSourceReference.", path, i+1, re, strings.TrimSpace(line))
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	if scanned == 0 {
		t.Fatal("no Go source was scanned; the guard would pass vacuously")
	}
}
