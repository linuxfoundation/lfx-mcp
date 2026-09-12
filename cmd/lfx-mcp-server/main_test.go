// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

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
	"search_ocg_meetups",
	"list_ocg_meetup_filters",
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

// challengeScopes are the scopes the server advertises. They mirror what
// runHTTPServer passes to withChallengeScopes, which is the same slice it
// hands the Protected Resource Metadata document.
var challengeScopes = []string{"openid", "profile", "email", tools.ScopeRead, tools.ScopeManage}

// authChain builds the /mcp middleware stack exactly as runHTTPServer does:
// bearer verification, then the challenge-scope wrapper. The verifier accepts
// any token whose name is a key in scopesByToken.
func authChain(handler http.Handler, scopesByToken map[string][]string) http.Handler {
	verify := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		scopes, ok := scopesByToken[token]
		if !ok {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{
			UserID:     "test-user",
			Scopes:     scopes,
			Expiration: time.Now().Add(time.Hour),
		}, nil
	}
	middleware := auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: "https://mcp.example.com/.well-known/oauth-protected-resource",
	})
	return withChallengeScopes(middleware(handler), challengeScopes)
}

// TestChallengeScopes_TokensReachHandler pins the authorization behaviour that
// the challenge scopes must not disturb. read:all and manage:all are alternative
// grants, not a required pair — newServer decides per tool which one applies, and
// manage:all implies read. Advertising both scopes on the challenge must not turn
// them into a conjunction at the HTTP layer, which is what setting them on
// RequireBearerTokenOptions.Scopes would do.
func TestChallengeScopes_TokensReachHandler(t *testing.T) {
	tests := []struct {
		name   string
		token  string
		scopes []string
	}{
		{name: "read-only token", token: "reader", scopes: []string{tools.ScopeRead}},
		{name: "manage-only token", token: "manager", scopes: []string{tools.ScopeManage}},
		{name: "both scopes", token: "both", scopes: []string{tools.ScopeRead, tools.ScopeManage}},
	}

	byToken := make(map[string][]string, len(tests))
	for _, tt := range tests {
		byToken[tt.token] = tt.scopes
	}

	var reached bool
	handler := authChain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}), byToken)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reached = false
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d; %s was rejected before reaching the handler",
					rec.Code, http.StatusOK, tt.name)
			}
			if !reached {
				t.Error("handler was not reached")
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != "" {
				t.Errorf("WWW-Authenticate = %q on a successful response, want none", got)
			}
		})
	}
}

// TestChallengeScopes_Challenge pins the scope parameter onto the challenge an
// unauthenticated caller receives. Clients treat it as authoritative and only
// fall back to the metadata document when it is absent, so it has to carry the
// full advertised set rather than just the enforced ones.
func TestChallengeScopes_Challenge(t *testing.T) {
	handler := authChain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), map[string][]string{"reader": {tools.ScopeRead}})

	for _, tt := range []struct {
		name       string
		authHeader string
	}{
		{name: "no token"},
		{name: "unknown token", authHeader: "Bearer nope"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			// A duplicate header would leave a client parsing two conflicting
			// challenges, so the wrapper must rewrite in place rather than add.
			challenges := rec.Header().Values("WWW-Authenticate")
			if len(challenges) != 1 {
				t.Fatalf("got %d WWW-Authenticate headers, want 1: %q", len(challenges), challenges)
			}
			want := `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource", scope="openid profile email read:all manage:all"`
			if challenges[0] != want {
				t.Errorf("WWW-Authenticate =\n  %q\nwant\n  %q", challenges[0], want)
			}
		})
	}
}

// TestWithChallengeScopes pins the wrapper itself against the cases the
// middleware stack does not exercise: challenges the SDK does not emit, and
// responses that must be left alone.
func TestWithChallengeScopes(t *testing.T) {
	const scopeParam = `scope="read:all manage:all"`

	tests := []struct {
		name     string
		code     int
		existing []string
		want     []string
	}{
		{
			name:     "401 gains the scope parameter",
			code:     http.StatusUnauthorized,
			existing: []string{`Bearer realm="mcp"`},
			want:     []string{`Bearer realm="mcp", ` + scopeParam},
		},
		{
			// Insufficient-scope errors carry the same guidance as a 401.
			name:     "403 gains the scope parameter",
			code:     http.StatusForbidden,
			existing: []string{`Bearer error="insufficient_scope"`},
			want:     []string{`Bearer error="insufficient_scope", ` + scopeParam},
		},
		{
			// The SDK sets scope itself when configured to enforce scopes.
			// Appending a second one would make the challenge ambiguous.
			name:     "existing scope parameter is left alone",
			code:     http.StatusUnauthorized,
			existing: []string{`Bearer scope="read:all"`},
			want:     []string{`Bearer scope="read:all"`},
		},
		{
			// scope is defined for Bearer; other schemes have their own grammar.
			name:     "non-Bearer challenge is left alone",
			code:     http.StatusUnauthorized,
			existing: []string{`Basic realm="mcp"`},
			want:     []string{`Basic realm="mcp"`},
		},
		{
			name:     "success response is left alone",
			code:     http.StatusOK,
			existing: nil,
			want:     nil,
		},
		{
			name:     "challenge-less 401 stays challenge-less",
			code:     http.StatusUnauthorized,
			existing: nil,
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := withChallengeScopes(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for _, challenge := range tt.existing {
					w.Header().Add("WWW-Authenticate", challenge)
				}
				w.WriteHeader(tt.code)
			}), []string{tools.ScopeRead, tools.ScopeManage})

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

			if rec.Code != tt.code {
				t.Errorf("status = %d, want %d", rec.Code, tt.code)
			}
			if got := rec.Header().Values("WWW-Authenticate"); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WWW-Authenticate = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestWithChallengeScopes_NoScopes pins the empty case: with nothing to
// advertise the wrapper returns the handler untouched, so a deployment without
// configured scopes behaves as it did before.
func TestWithChallengeScopes_NoScopes(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if got := withChallengeScopes(handler, nil); reflect.ValueOf(got).Pointer() != reflect.ValueOf(handler).Pointer() {
		t.Error("withChallengeScopes wrapped the handler despite having no scopes to advertise")
	}
}

// TestChallengeScopeWriter_Flush pins Flusher support. The streamable HTTP
// transport sends server-sent events, so a wrapper that hides Flush from it
// would stall responses.
func TestChallengeScopeWriter_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	var w http.ResponseWriter = &challengeScopeWriter{ResponseWriter: rec, scopeParam: `scope="read:all"`}

	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("challengeScopeWriter does not implement http.Flusher")
	}
	flusher.Flush()
	if !rec.Flushed {
		t.Error("Flush did not reach the underlying ResponseWriter")
	}
}

// TestNewServer_Tools1AreReadScoped pins the TOOLS-1 registrations: the two
// new tools carry the caller's own visibility through the exchanged token, so
// they are listed for any read-scoped caller (staff or not) and absent for a
// token without read scope.
func TestNewServer_Tools1AreReadScoped(t *testing.T) {
	tools1 := []string{"count_lfx_resources", "get_org_committee_seats"}
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

// TestNewServer_ScopeBlindClientGetsAdvertisedScopes covers clients that ignore
// the scopes advertised in the PRM and the WWW-Authenticate challenge, and so
// present a valid token carrying no MCP scope. They are treated as having
// requested the advertised set, since they offer no way to choose scopes.
func TestNewServer_ScopeBlindClientGetsAdvertisedScopes(t *testing.T) {
	const readTool = "count_lfx_resources"
	const manageTool = "create_committee"
	enabled := []string{readTool, manageTool}

	codexToken := func() *auth.TokenInfo {
		return &auth.TokenInfo{
			Scopes: []string{"offline_access"},
			Extra: map[string]any{
				tools.ClaimClientID: "https://chatgpt.com/oauth/codex/IrVFZga_egXz/client.json",
			},
		}
	}

	t.Run("advertising both scopes grants both", func(t *testing.T) {
		listed := listedToolsFor(t, enabled, codexToken())
		if !listed[readTool] {
			t.Errorf("%s must be listed for a scope-blind client", readTool)
		}
		if !listed[manageTool] {
			t.Errorf("%s must be listed for a scope-blind client when manage:all is advertised", manageTool)
		}
	})

	t.Run("advertising only read grants only read", func(t *testing.T) {
		cfg := Config{
			Tools:  enabled,
			MCPAPI: MCPAPIConfig{Scopes: []string{"openid", tools.ScopeRead}},
		}
		listed := listedToolsForConfig(t, cfg, codexToken())
		if !listed[readTool] {
			t.Errorf("%s must be listed when read:all is advertised", readTool)
		}
		if listed[manageTool] {
			t.Errorf("%s must not be listed when manage:all is not advertised", manageTool)
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
		if listed[readTool] || listed[manageTool] {
			t.Errorf("no tools may be listed for an unrecognised client with no MCP scopes, got %v", listed)
		}
	})
}

func TestHasScopeParam(t *testing.T) {
	tests := []struct {
		name      string
		challenge string
		want      bool
	}{
		{
			name:      "no scope parameter",
			challenge: `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`,
			want:      false,
		},
		{
			name:      "scope parameter present",
			challenge: `Bearer resource_metadata="https://example.com/prm", scope="openid read:all"`,
			want:      true,
		},
		{
			name:      "scope is the first parameter",
			challenge: `Bearer scope="openid"`,
			want:      true,
		},
		{
			// The reason for boundary matching: a quoted value mentioning
			// scope= must not suppress the parameter we add.
			name:      "scope mentioned inside a quoted value",
			challenge: `Bearer error="invalid_token", error_description="try scope=read:all"`,
			want:      false,
		},
		{
			name:      "different parameter ending in scope",
			challenge: `Bearer max_scope="read:all"`,
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasScopeParam(tc.challenge); got != tc.want {
				t.Errorf("hasScopeParam(%q) = %v, want %v", tc.challenge, got, tc.want)
			}
		})
	}
}

// TestHasScopeParam_CaseInsensitive covers auth parameter names being
// case-insensitive per RFC 9110.
func TestHasScopeParam_CaseInsensitive(t *testing.T) {
	if !hasScopeParam(`Bearer realm="x", Scope="openid"`) {
		t.Error(`hasScopeParam did not match an uppercase Scope parameter`)
	}
}
