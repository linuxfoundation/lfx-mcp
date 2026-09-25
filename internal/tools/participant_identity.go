// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"strings"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// participantIdentity caches the immutable matching signals once per record.
// Username stays raw; email and name use Self Serve's trim/lower rules.
type participantIdentity struct {
	resource *querysvc.Resource
	data     map[string]any
	username string
	email    string
	name     string
}

// dedupeParticipants ports Self Serve's getPastMeetingParticipants at
// 188c18c32 (pairwise identity introduced in ee7822104): union-find with path
// halving over ALL pairs, followed by conflict splitting and encounter-order
// merging. The handler supplies the page or capped date-range record set.
func dedupeParticipants(resources []*querysvc.Resource) []*querysvc.Resource {
	entries := make([]participantIdentity, len(resources))
	parent := make([]int, len(resources))
	for i, resource := range resources {
		data, _ := resource.Data.(map[string]any)
		username, _ := data["username"].(string)
		email, _ := data["email"].(string)
		first, _ := data["first_name"].(string)
		last, _ := data["last_name"].(string)
		entries[i] = participantIdentity{
			resource: resource, data: data, username: username,
			email: lowerParticipantValue(trimParticipantValue(email)),
			name:  normalizedParticipantName(first, last),
		}
		parent[i] = i
	}
	find := func(index int) int {
		for parent[index] != index {
			parent[index] = parent[parent[index]]
			index = parent[index]
		}
		return index
	}
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if !sameParticipantIdentity(&entries[i], &entries[j]) {
				continue
			}
			rootI, rootJ := find(i), find(j)
			if rootI != rootJ {
				parent[rootI] = rootJ
			}
		}
	}

	// Slices preserve JavaScript Map insertion order: components and members
	// both follow their first encounter, not numerical root or map key order.
	componentIndex := make(map[int]int)
	var components [][]int
	for i := range entries {
		root := find(i)
		index, exists := componentIndex[root]
		if !exists {
			index = len(components)
			componentIndex[root] = index
			components = append(components, nil)
		}
		components[index] = append(components[index], i)
	}

	out := make([]*querysvc.Resource, 0, len(components))
	for _, component := range components {
		for _, group := range splitParticipantComponent(entries, component) {
			out = append(out, mergeParticipantGroup(entries, group))
		}
	}
	return out
}

// sameParticipantIdentity mirrors isSamePerson. Hard username disagreement
// wins over a shared email; email is checked before username asymmetry.
func sameParticipantIdentity(a, b *participantIdentity) bool {
	if a.username != "" && b.username != "" {
		return a.username == b.username
	}
	if a.email != "" && b.email != "" {
		return a.email == b.email
	}
	if a.username != "" || b.username != "" {
		return false
	}
	return a.name != "" && a.name == b.name
}

// trimParticipantValue matches JavaScript String.trim: BOM is whitespace,
// but U+0085 NEXT LINE is not. Go's strings.TrimSpace differs on both.
func trimParticipantValue(value string) string {
	return strings.Trim(value, "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff")
}

// lowerParticipantValue uses full Unicode lowercasing like JavaScript's
// toLowerCase (including expansions and final sigma), not Go's simple rune
// mapping. x/text is already pinned in this module's dependency graph.
func lowerParticipantValue(value string) string {
	return cases.Lower(language.Und).String(value)
}

// normalizedParticipantName mirrors normalizeParticipantName and the shared
// isUnresolvableParticipantName helper. Placeholder tokens determine whether
// a name is resolvable; they are not removed from an otherwise meaningful name.
func normalizedParticipantName(first, last string) string {
	meaningful := func(token string) bool {
		token = lowerParticipantValue(trimParticipantValue(token))
		return token != "" && token != "unknown" && token != "[unknown]"
	}
	if !meaningful(first) && !meaningful(last) {
		return ""
	}
	return lowerParticipantValue(trimParticipantValue(trimParticipantValue(first) + " " + trimParticipantValue(last)))
}

// splitParticipantComponent mirrors splitConflictingComponent AFTER the pure
// transitive closure. Ambiguous unkeyed records remain singletons, following
// keyed groups; they never lend their flags to a guessed identity.
func splitParticipantComponent(entries []participantIdentity, component []int) [][]int {
	usernames := make(map[string]bool)
	guestEmails := make(map[string]bool)
	for _, i := range component {
		entry := &entries[i]
		if entry.username != "" {
			usernames[entry.username] = true
		} else if entry.email != "" {
			guestEmails[entry.email] = true
		}
	}
	usernameConflict := len(usernames) > 1
	emailConflict := !usernameConflict && len(guestEmails) > 1
	if !usernameConflict && !emailConflict {
		return [][]int{component}
	}

	groupIndex := make(map[string]int)
	var groups, floaters [][]int
	for _, i := range component {
		key := entries[i].email
		if usernameConflict {
			key = entries[i].username
		}
		if key == "" {
			floaters = append(floaters, []int{i})
			continue
		}
		index, exists := groupIndex[key]
		if !exists {
			index = len(groups)
			groupIndex[key] = index
			groups = append(groups, nil)
		}
		groups[index] = append(groups[index], i)
	}
	return append(groups, floaters...)
}

func participantFlag(data map[string]any, field string) bool {
	value, _ := data[field].(bool)
	return value
}

func copyParticipantData(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for key, value := range data {
		out[key] = value
	}
	return out
}

// mergeParticipantGroup mirrors mergePastMeetingParticipantGroup: first
// attended record wins, flags OR together, and pickNonBlank fills optional
// fields (including email) without treating whitespace as a value.
func mergeParticipantGroup(entries []participantIdentity, group []int) *querysvc.Resource {
	first := &entries[group[0]]
	if first.data == nil {
		// An opaque/non-object resource has no matching identity signals and
		// is necessarily a singleton; preserve its original resource shape.
		return first.resource
	}
	resource := first.resource
	merged := copyParticipantData(first.data)
	for _, i := range group[1:] {
		next := &entries[i]
		preferred, other := merged, next.data
		if participantFlag(next.data, "is_attended") && !participantFlag(merged, "is_attended") {
			preferred, other = next.data, merged
			resource = next.resource
		}
		combined := copyParticipantData(preferred)
		for _, field := range []string{"is_attended", "is_invited", "host", "org_is_member", "org_is_project_member"} {
			combined[field] = participantFlag(merged, field) || participantFlag(next.data, field)
		}
		for _, field := range []string{"avatar_url", "job_title", "org_name", "username", "email"} {
			value, _ := preferred[field].(string)
			if trimParticipantValue(value) != "" {
				continue
			}
			// pickNonBlank returns the fallback as-is, even when blank. An
			// absent fallback is JS undefined and stays omitted in JSON.
			if fallback, exists := other[field]; exists {
				combined[field] = fallback
			} else {
				delete(combined, field)
			}
		}
		merged = combined
	}
	if email, found := participantMergedEmail(entries, group); found {
		merged["email"] = email
	}
	return &querysvc.Resource{Type: resource.Type, ID: resource.ID, Data: merged}
}

// participantMergedEmail mirrors pickMergedEmail: first invited nonblank
// email, otherwise first nonblank email, retaining the original stored value.
func participantMergedEmail(entries []participantIdentity, group []int) (string, bool) {
	fallback := ""
	for _, i := range group {
		entry := &entries[i]
		email, _ := entry.data["email"].(string)
		if trimParticipantValue(email) == "" {
			continue
		}
		if participantFlag(entry.data, "is_invited") {
			return email, true
		}
		if fallback == "" {
			fallback = email
		}
	}
	return fallback, fallback != ""
}
