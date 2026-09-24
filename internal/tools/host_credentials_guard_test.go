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
// The serialized field name host_key is matched too, so a handler cannot
// read or return data["host_key"] from a query result unnoticed. The known
// existing references are listed in hostCredentialAllowedLines.
var hostCredentialSourcePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)host_?credential`),
	regexp.MustCompile(`\bHostKey\b`),
	regexp.MustCompile(`\bhost_key\b`),
}

// hostCredentialAllowedLines are the existing source lines the scan accepts,
// keyed by path relative to the repository root and matched with runs of
// whitespace collapsed (see normalizeSourceLine), so gofmt realigning a map
// does not break a match. Each one is reviewed and does not return host
// credentials to a caller. Each entry covers one occurrence, and an entry
// that matches no line fails the test, so it must be removed when its line
// goes away. Never add one without the gating decision the scan asks for.
var hostCredentialAllowedLines = map[string][]string{
	// The meeting result field list removes host_key from meeting results.
	"internal/tools/meeting_result_fields.go": {`"host_key": {},`},
}

// meetingClientWiringFile builds the meeting-service client, wiring every
// endpoint whether or not a tool calls it. A wiring line for a host-key
// producer (see hostKeyProducerPatterns) is accepted there, and only there;
// the set follows the pinned client, so it needs no entry here and cannot go
// stale when an upgrade removes the producer.
const meetingClientWiringFile = "internal/lfxv2/client.go"

// meetingClientWiringLine matches one endpoint wiring line in
// meetingClientWiringFile, capturing the method name.
var meetingClientWiringLine = regexp.MustCompile(`^meetingHTTPClient\.(\w+)\(\),$`)

// normalizeSourceLine trims a source line and collapses internal runs of
// whitespace to one space.
func normalizeSourceLine(line string) string {
	return strings.Join(strings.Fields(line), " ")
}

// hostKeyProducerPatterns returns a pattern for every meeting-service client
// method whose response carries a HostKey field, at any depth, and for every
// response type that declares one, and the names of those methods. They are
// derived from the pinned client by reflection, so a method or type added
// upstream is covered without editing this test. An empty result is valid:
// from meeting-service v0.12.7 the host key is no longer on any client
// response. The derivation is checked against a probe type instead, so a
// broken derivation still fails.
func hostKeyProducerPatterns(t *testing.T) ([]*regexp.Regexp, map[string]bool) {
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

	// The probe nests the field the way generated responses do (a pointer to
	// a slice of structs). If the derivation cannot find it, an empty list
	// from the client would mean nothing.
	type probeHostKeyHolder struct{ HostKey *string }
	type probeResponse struct{ Items []*probeHostKeyHolder }
	if !carries(reflect.TypeOf(&probeResponse{}), make(map[reflect.Type]bool)) {
		t.Fatal("the HostKey derivation does not find a nested HostKey field in a probe type; " +
			"it is broken, and the guard would miss a fetch path")
	}
	delete(typeNames, "probeHostKeyHolder")

	var patterns []*regexp.Regexp
	methods := make(map[string]bool)
	client := reflect.TypeOf(&meetingservice.Client{})
	for i := 0; i < client.NumMethod(); i++ {
		m := client.Method(i)
		for j := 0; j < m.Type.NumOut(); j++ {
			if carries(m.Type.Out(j), make(map[reflect.Type]bool)) {
				patterns = append(patterns, regexp.MustCompile(`\b`+m.Name+`\b`))
				methods[m.Name] = true
				break
			}
		}
	}
	if len(methods) == 0 {
		t.Log("no meeting-service client method returns a HostKey field; only the fixed patterns apply")
	}
	for name := range typeNames {
		patterns = append(patterns, regexp.MustCompile(`\b`+name+`\b`))
	}
	return patterns, methods
}

// TestHostCredentials_NoSourceReference is a deliberately crude source scan.
// It exists so that a tool cannot start counting, searching or fetching
// meeting host credentials as a side effect of an unrelated change. If it
// fails, the change is adding a path to host credentials: that needs a
// deliberate gating decision in newServer (who may call it, under which
// scope and staff gate) and a reviewed update to this guard, not a tweak to
// the patterns.
func TestHostCredentials_NoSourceReference(t *testing.T) {
	producerPatterns, producers := hostKeyProducerPatterns(t)
	patterns := append(slices.Clone(hostCredentialSourcePatterns), producerPatterns...)
	// Scan every non-test Go file under internal and cmd, so a helper package
	// that wraps a host-credential fetch is caught as well as a tool.
	repoRoot := filepath.Join("..", "..")
	roots := []string{filepath.Join(repoRoot, "internal"), filepath.Join(repoRoot, "cmd")}
	scanned := 0
	// Each allowlisted line may be used once. Unused entries are reported, so a
	// removed line cannot leave an entry behind that would later hide an
	// identical new reference.
	unused := make(map[string]map[string]int, len(hostCredentialAllowedLines))
	for file, lines := range hostCredentialAllowedLines {
		unused[file] = make(map[string]int, len(lines))
		for _, line := range lines {
			unused[file][normalizeSourceLine(line)]++
		}
	}
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
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			allowed := unused[rel]
			for i, line := range strings.Split(string(src), "\n") {
				normalized := normalizeSourceLine(line)
				if allowed[normalized] > 0 {
					allowed[normalized]--
					continue
				}
				if rel == meetingClientWiringFile {
					if m := meetingClientWiringLine.FindStringSubmatch(normalized); m != nil && producers[m[1]] {
						continue
					}
				}
				for _, re := range patterns {
					if re.MatchString(line) {
						t.Errorf("%s:%d refers to meeting host credentials (%s): %q\n"+
							"No tool may count, search or fetch meeting host credentials without a deliberate gating change; "+
							"see TestHostCredentials_NoSourceReference.", rel, i+1, re, normalized)
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
	for file, lines := range unused {
		for line, n := range lines {
			if n > 0 {
				t.Errorf("hostCredentialAllowedLines entry %s: %q matched no source line; "+
					"remove the entry now that the line is gone", file, line)
			}
		}
	}
}
