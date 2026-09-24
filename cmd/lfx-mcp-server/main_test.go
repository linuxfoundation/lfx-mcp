// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	lfxauth "github.com/linuxfoundation/lfx-mcp/internal/auth"
	"github.com/linuxfoundation/lfx-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSplitTrimmed(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "empty string is unset",
			input: "",
			want:  []string{},
		},
		{
			name:  "single value",
			input: "hello_world",
			want:  []string{"hello_world"},
		},
		{
			name:  "multiple values",
			input: "a,b,c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "values with surrounding whitespace",
			input: "a, b ,  c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:    "lone comma is malformed",
			input:   ",",
			wantErr: true,
		},
		{
			name:    "leading comma is malformed",
			input:   ",a,b",
			wantErr: true,
		},
		{
			name:    "trailing comma is malformed",
			input:   "a,b,",
			wantErr: true,
		},
		{
			name:    "embedded empty entry is malformed",
			input:   "a,,b",
			wantErr: true,
		},
		{
			name:    "whitespace-only entry is malformed",
			input:   "a, ,b",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitTrimmed(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitTrimmed(%q) = %#v, nil, want an error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitTrimmed(%q) returned unexpected error: %v", tt.input, err)
			}
			if got == nil {
				t.Fatal("splitTrimmed returned nil, want non-nil slice")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitTrimmed(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

// staffOnlyTools are the lens-backed tools and the guidance that documents
// them: they read cross-project warehouse data, so newServer registers them
// for LF staff only, exactly alike — a tool this list grows by is gated the
// day it is added, not when someone notices.
var staffOnlyTools = []string{
	"query_lfx_lens",
	"explore_lfx_semantic_layer",
	"query_lfx_semantic_layer",
	"query_lfx_standard_metrics",
	"read_lfx_semantic_layer_guidance",
	"read_lfx_standard_metrics_guidance",
}

// listedTools is the tools/list a caller holding token sees from a server
// with every name in staffOnlyTools enabled.
func listedTools(t *testing.T, token *auth.TokenInfo) map[string]bool {
	t.Helper()
	return listedToolsFor(t, staffOnlyTools, token)
}

// listedToolsFor is listedTools over an explicit enabled-tool list.
func listedToolsFor(t *testing.T, enabled []string, token *auth.TokenInfo) map[string]bool {
	t.Helper()
	return listedToolsForConfig(t, Config{Tools: enabled}, token)
}

func listedToolsForConfig(t *testing.T, cfg Config, token *auth.TokenInfo) map[string]bool {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	server := newServer(cfg, "test", token)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect failed: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	res, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	listed := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		listed[tool.Name] = true
	}
	return listed
}

// TestNewServer_LensToolsAreStaffOnly pins the gate: a read-scoped caller who
// is not LF staff sees none of the lens-backed tools or their guidance, and a
// read-scoped staff caller sees all of them.
func TestNewServer_LensToolsAreStaffOnly(t *testing.T) {
	reader := &auth.TokenInfo{Scopes: []string{tools.ScopeRead}}
	staff := &auth.TokenInfo{
		Scopes: []string{tools.ScopeRead},
		Extra:  map[string]any{tools.ClaimLFStaff: true},
	}

	forReader := listedTools(t, reader)
	forStaff := listedTools(t, staff)
	for _, name := range staffOnlyTools {
		if forReader[name] {
			t.Errorf("%s is listed for a non-staff reader; it must be staff-only", name)
		}
		if !forStaff[name] {
			t.Errorf("%s is not listed for a staff reader", name)
		}
	}
}

// TestNewServer_LensToolsAreStaffOnly_MachineAccounts pins the M2M side of the
// staff gate: a read-scoped caller flagged as a machine account (via
// auth.MachineAccountExtraKey, set at verification time for M2M JWTs — see
// lfxauth.IsMachineToken) sees the lens-backed tools even without the
// lf_staff claim, but the machine marker alone does not substitute for the
// read/manage scope requirement.
func TestNewServer_LensToolsAreStaffOnly_MachineAccounts(t *testing.T) {
	machineReader := &auth.TokenInfo{
		Scopes: []string{tools.ScopeRead},
		Extra:  map[string]any{lfxauth.MachineAccountExtraKey: true},
	}
	machineNoScope := &auth.TokenInfo{
		Extra: map[string]any{lfxauth.MachineAccountExtraKey: true},
	}

	forMachineReader := listedTools(t, machineReader)
	forMachineNoScope := listedTools(t, machineNoScope)
	for _, name := range staffOnlyTools {
		if !forMachineReader[name] {
			t.Errorf("%s is not listed for a read-scoped machine account", name)
		}
		if forMachineNoScope[name] {
			t.Errorf("%s is listed for a machine account with no read/manage scope; the machine marker must not bypass scope checks", name)
		}
	}
}

// TestNewServer_Tools1AreReadScoped pins the TOOLS-1 registrations: these
// tools carry the caller's own visibility through the exchanged token, so
// they are listed for any read-scoped caller (staff or not) and absent for a
// token without read scope.
func TestNewServer_Tools1AreReadScoped(t *testing.T) {
	tools1 := []string{"count_lfx_resources", "get_org_committee_seats", "audit_committee_coverage"}
	reader := &auth.TokenInfo{Scopes: []string{tools.ScopeRead}}
	noScope := &auth.TokenInfo{Scopes: []string{}}

	forReader := listedToolsFor(t, tools1, reader)
	forNoScope := listedToolsFor(t, tools1, noScope)
	for _, name := range tools1 {
		if !forReader[name] {
			t.Errorf("%s must be listed for a non-staff read-scoped caller", name)
		}
		if forNoScope[name] {
			t.Errorf("%s must not be listed without read scope", name)
		}
	}
}

// TestNewServer_ManageToolsAreListedForReaders pins the step-up model: write
// tools (tools.ManageScopeTools) are registered for any caller holding at
// least read:all, not just manage:all, so a client can discover the tool and
// its schema before completing an OAuth step-up. A caller with no read scope
// at all still sees nothing.
func TestNewServer_ManageToolsAreListedForReaders(t *testing.T) {
	const manageTool = "create_committee"
	reader := &auth.TokenInfo{Scopes: []string{tools.ScopeRead}}
	manager := &auth.TokenInfo{Scopes: []string{tools.ScopeManage}}
	noScope := &auth.TokenInfo{Scopes: []string{}}

	forReader := listedToolsFor(t, []string{manageTool}, reader)
	forManager := listedToolsFor(t, []string{manageTool}, manager)
	forNoScope := listedToolsFor(t, []string{manageTool}, noScope)

	if !forReader[manageTool] {
		t.Errorf("%s must be listed for a read:all-only caller", manageTool)
	}
	if !forManager[manageTool] {
		t.Errorf("%s must be listed for a manage:all caller", manageTool)
	}
	if forNoScope[manageTool] {
		t.Errorf("%s must not be listed without read or manage scope", manageTool)
	}
}

// TestRequireManageScopeHTTP_BlocksWithoutManageScope pins the HTTP-layer
// enforcement: a tools/call POST for a manage:all-gated tool from a
// read:all-only caller gets a 403 with an insufficient_scope challenge,
// without reaching the MCP handler.
func TestRequireManageScopeHTTP_BlocksWithoutManageScope(t *testing.T) {
	rec, ok := callManageScopeTool(t, "create_committee", []string{tools.ScopeRead})
	if ok {
		t.Fatalf("handler must not run for a read:all-only caller")
	}
	assertInsufficientScope(t, rec, "create_committee")
}

// TestRequireManageScopeHTTP_BlocksGroupModeAlias pins that the group-mode
// alias for a manage:all tool (create_group, the group-mode name for
// create_committee) is blocked identically to its canonical committee-mode
// name.
func TestRequireManageScopeHTTP_BlocksGroupModeAlias(t *testing.T) {
	rec, ok := callManageScopeTool(t, "create_group", []string{tools.ScopeRead})
	if ok {
		t.Fatalf("handler must not run for a read:all-only caller")
	}
	assertInsufficientScope(t, rec, "create_group")
}

// TestRequireManageScopeHTTP_AllowsWithManageScope pins that a caller holding
// manage:all reaches the handler instead of being blocked.
func TestRequireManageScopeHTTP_AllowsWithManageScope(t *testing.T) {
	_, ok := callManageScopeTool(t, "create_committee", []string{tools.ScopeManage})
	if !ok {
		t.Fatalf("handler must run for a manage:all caller")
	}
}

// callManageScopeTool drives requireManageScopeHTTP directly with a
// synthetic tools/call POST for toolName and a bearer token carrying scopes.
// The test verifier below treats the bearer value as a comma-separated scope
// list, so it can be exercised without real JWT verification. It returns the
// recorder and whether the wrapped handler ran.
func callManageScopeTool(t *testing.T, toolName string, scopes []string) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	verifyToken := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{Scopes: strings.Split(token, ","), Expiration: time.Now().Add(time.Hour)}, nil
	}
	authMiddleware := auth.RequireBearerToken(verifyToken, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: "https://example.test/.well-known/oauth-protected-resource",
	})

	var handlerRan bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerRan = true
		w.WriteHeader(http.StatusOK)
	})

	handler := authMiddleware(requireManageScopeHTTP(Config{}, "https://example.test/.well-known/oauth-protected-resource", next))

	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, toolName)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+strings.Join(scopes, ","))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec, handlerRan
}

// assertInsufficientScope asserts rec is the HTTP 403 insufficient_scope
// challenge requireManageScopeHTTP returns for toolName.
func assertInsufficientScope(t *testing.T, rec *httptest.ResponseRecorder, toolName string) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	got := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(got, `error="insufficient_scope"`) {
		t.Errorf("WWW-Authenticate missing insufficient_scope: %q", got)
	}
	if !strings.Contains(got, `scope="`+tools.ScopeManage+`"`) {
		t.Errorf("WWW-Authenticate missing required scope: %q", got)
	}
	if !strings.Contains(got, toolName) {
		t.Errorf("WWW-Authenticate missing tool name %q: %q", toolName, got)
	}
}

// TestNewServer_ScopeBlindClientGetsAdvertisedScopes covers clients that ignore
// the scopes advertised in the PRM and so present a valid token carrying no
// MCP scope. They are treated as having requested the advertised set, since
// they offer no way to choose scopes.
func TestNewServer_ScopeBlindClientGetsAdvertisedScopes(t *testing.T) {
	const readTool = "count_lfx_resources"
	enabled := []string{readTool}

	codexToken := func() *auth.TokenInfo {
		return &auth.TokenInfo{
			Scopes:     []string{"offline_access"},
			Expiration: time.Now().Add(time.Hour),
			Extra: map[string]any{
				tools.ClaimClientID: "https://chatgpt.com/oauth/codex/IrVFZga_egXz/client.json",
			},
		}
	}

	t.Run("advertising read grants read", func(t *testing.T) {
		listed := listedToolsFor(t, enabled, codexToken())
		if !listed[readTool] {
			t.Errorf("%s must be listed for a scope-blind client", readTool)
		}
	})

	t.Run("unrecognised client gets nothing", func(t *testing.T) {
		other := &auth.TokenInfo{
			Scopes: []string{"offline_access"},
			Extra: map[string]any{
				tools.ClaimClientID: "https://example.com/oauth/client.json",
			},
		}
		listed := listedToolsFor(t, enabled, other)
		if listed[readTool] {
			t.Errorf("no tools may be listed for an unrecognised client with no MCP scopes, got %v", listed)
		}
	})

	// A scope-blind client with no configured cfg.MCPAPI.Scopes falls back to
	// tools.DefaultScopes (read:all and manage:all), matching the default
	// behavior any other client gets by requesting both scopes up front. It
	// must be able to both discover and call a write tool without a step-up
	// error, the same as a compliant client that requested manage:all.
	t.Run("default fallback grants manage:all", func(t *testing.T) {
		const manageTool = "create_committee"

		verifyToken := func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
			return codexToken(), nil
		}
		authMiddleware := auth.RequireBearerToken(verifyToken, &auth.RequireBearerTokenOptions{
			ResourceMetadataURL: "https://example.test/.well-known/oauth-protected-resource",
		})
		var handlerRan bool
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerRan = true
			w.WriteHeader(http.StatusOK)
		})
		handler := authMiddleware(requireManageScopeHTTP(Config{}, "https://example.test/.well-known/oauth-protected-resource", next))

		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, manageTool)
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer irrelevant")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if !handlerRan {
			t.Fatalf("expected %s not to be blocked by the manage:all step-up for a scope-blind client, got status %d", manageTool, rec.Code)
		}
	})
}
