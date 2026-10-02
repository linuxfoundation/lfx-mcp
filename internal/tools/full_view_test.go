// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"testing"

	lfxauth "github.com/linuxfoundation/lfx-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// fullViewTokenCases is the table of caller kinds every full-view test walks:
// name, token, whether registration treats it as staff, whether people tools
// give it the full view.
var fullViewTokenCases = []struct {
	name     string
	token    *auth.TokenInfo
	staff    bool
	fullView bool
}{
	{name: "nil token (stdio / no auth servers)", token: nil, staff: true, fullView: true},
	{name: "lf_staff true", token: &auth.TokenInfo{Scopes: []string{ScopeRead}, Extra: map[string]any{ClaimLFStaff: true}}, staff: true, fullView: true},
	{name: "lf_staff as the string \"true\"", token: &auth.TokenInfo{Scopes: []string{ScopeRead}, Extra: map[string]any{ClaimLFStaff: "true"}}, staff: false, fullView: false},
	{name: "lf_staff false", token: &auth.TokenInfo{Scopes: []string{ScopeRead}, Extra: map[string]any{ClaimLFStaff: false}}, staff: false, fullView: false},
	{name: "machine account", token: &auth.TokenInfo{Scopes: []string{ScopeRead}, Extra: map[string]any{lfxauth.MachineAccountExtraKey: true}}, staff: true, fullView: true},
	{name: "static API key", token: &auth.TokenInfo{Scopes: []string{ScopeRead, ScopeManage}, Extra: map[string]any{lfxauth.APIKeyAuthExtraKey: true}}, staff: false, fullView: true},
	{name: "API key flag as a string", token: &auth.TokenInfo{Scopes: []string{ScopeRead}, Extra: map[string]any{lfxauth.APIKeyAuthExtraKey: "true"}}, staff: false, fullView: false},
	{name: "plain reader", token: &auth.TokenInfo{Scopes: []string{ScopeRead}, Extra: map[string]any{"raw_token": "x", "username": "reader"}}, staff: false, fullView: false},
	{name: "reader with nil Extra", token: &auth.TokenInfo{Scopes: []string{ScopeRead}}, staff: false, fullView: false},
}

func TestIsFullViewCaller_Table(t *testing.T) {
	for _, tc := range fullViewTokenCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsStaffCaller(tc.token); got != tc.staff {
				t.Errorf("IsStaffCaller = %v, want %v", got, tc.staff)
			}
			if got := IsFullViewCaller(tc.token); got != tc.fullView {
				t.Errorf("IsFullViewCaller = %v, want %v", got, tc.fullView)
			}
			// API keys are the only kind that is full view without being
			// staff: the staff-only Lens tools stay absent for them.
			if IsAPIKeyCaller(tc.token) && IsStaffCaller(tc.token) {
				t.Error("an API-key caller must not count as staff")
			}
		})
	}
}

func TestHasFullView_AbsentIsFalse(t *testing.T) {
	if HasFullView(context.Background()) {
		t.Fatal("a context without the flag must not have full view")
	}
	if HasFullView(WithFullView(context.Background(), false)) {
		t.Fatal("an explicit false must not have full view")
	}
	if !HasFullView(WithFullView(context.Background(), true)) {
		t.Fatal("an explicit true must have full view")
	}
	// A later WithFullView(false) narrows a context that had true.
	if HasFullView(WithFullView(WithFullView(context.Background(), true), false)) {
		t.Fatal("the innermost value must win")
	}
}

func TestCallerIdentityHelpers(t *testing.T) {
	if callerUsername(nil) != "" || callerEmail(nil) != "" {
		t.Fatal("nil token must yield no identity")
	}
	tok := &auth.TokenInfo{Extra: map[string]any{"username": "reader", ClaimEmail: "  Reader@Example.test "}}
	if got := callerUsername(tok); got != "reader" {
		t.Errorf("callerUsername = %q", got)
	}
	if got := callerEmail(tok); got != "Reader@Example.test" {
		t.Errorf("callerEmail = %q (must be trimmed, case preserved)", got)
	}
	if callerEmail(&auth.TokenInfo{Extra: map[string]any{ClaimEmail: 42}}) != "" {
		t.Error("a non-string email claim must yield no email")
	}
}
