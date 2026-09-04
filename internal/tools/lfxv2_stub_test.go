// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubMCPToken is the raw MCP bearer token the tests present to handlers.
const stubMCPToken = "mcp-user-token"

// stubExchangedToken is the LFX API token the fake token endpoint issues in
// exchange for stubMCPToken. Handlers must reach the API with this token.
const stubExchangedToken = "exchanged-lfx-token"

// stubTokenPath is the path the fake token endpoint is served on.
const stubTokenPath = "/oauth/token"

// stubAPIRequest is one request the fake LFX API observed.
type stubAPIRequest struct {
	Method        string
	Path          string
	Query         url.Values
	Authorization string
}

// stubAPIResponse is a canned response for one API request.
type stubAPIResponse struct {
	Status int
	Body   string
}

// stubLFXAPI is a fake LFX API domain plus token endpoint. It records every
// API request and answers with the responses queued per path via Respond, in
// FIFO order; when a path has no queued response it replies 404 so a test
// that forgot to script a call fails loudly rather than silently succeeding.
type stubLFXAPI struct {
	t       *testing.T
	mu      sync.Mutex
	server  *httptest.Server
	queues  map[string][]stubAPIResponse
	reqs    []stubAPIRequest
	Clients *lfxv2.Clients
}

// newStubLFXAPI starts the fake API and builds a real lfxv2.Clients against
// it, with a real TokenExchangeClient pointed at the fake token endpoint, so
// the auth interceptor and the exchanged Authorization header are exercised
// for real rather than bypassed.
func newStubLFXAPI(t *testing.T) *stubLFXAPI {
	t.Helper()
	s := &stubLFXAPI{t: t, queues: map[string][]stubAPIResponse{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)

	tokenClient, err := lfxv2.NewTokenExchangeClient(lfxv2.TokenExchangeConfig{
		TokenEndpoint:    s.server.URL + stubTokenPath,
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		SubjectTokenType: "urn:test:mcp",
		Audience:         "urn:test:lfx",
	})
	if err != nil {
		t.Fatalf("token exchange client: %v", err)
	}
	clients, err := lfxv2.NewClients(context.Background(), lfxv2.ClientConfig{
		APIDomain:           s.server.URL,
		TokenExchangeClient: tokenClient,
	})
	if err != nil {
		t.Fatalf("lfxv2 clients: %v", err)
	}
	s.Clients = clients
	return s
}

func (s *stubLFXAPI) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == stubTokenPath {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": stubExchangedToken,
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
		return
	}

	s.mu.Lock()
	s.reqs = append(s.reqs, stubAPIRequest{
		Method:        r.Method,
		Path:          r.URL.Path,
		Query:         r.URL.Query(),
		Authorization: r.Header.Get("Authorization"),
	})
	queue := s.queues[r.URL.Path]
	var resp stubAPIResponse
	if len(queue) > 0 {
		resp = queue[0]
		s.queues[r.URL.Path] = queue[1:]
	} else {
		resp = stubAPIResponse{Status: http.StatusNotFound, Body: `{"message":"no stubbed response for ` + r.URL.Path + `"}`}
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.Status)
	_, _ = w.Write([]byte(resp.Body))
}

// Respond queues a 200 response for path.
func (s *stubLFXAPI) Respond(path, body string) {
	s.RespondStatus(path, http.StatusOK, body)
}

// RespondStatus queues a response with an explicit status for path.
func (s *stubLFXAPI) RespondStatus(path string, status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queues[path] = append(s.queues[path], stubAPIResponse{Status: status, Body: body})
}

// Requests returns every API request seen so far, in order (token endpoint
// calls excluded).
func (s *stubLFXAPI) Requests() []stubAPIRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stubAPIRequest, len(s.reqs))
	copy(out, s.reqs)
	return out
}

// RequestsTo returns the API requests for one path.
func (s *stubLFXAPI) RequestsTo(path string) []stubAPIRequest {
	var out []stubAPIRequest
	for _, r := range s.Requests() {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// LastRequest returns the most recent API request, failing when none was made.
func (s *stubLFXAPI) LastRequest() stubAPIRequest {
	s.t.Helper()
	reqs := s.Requests()
	if len(reqs) == 0 {
		s.t.Fatal("expected at least one API request")
	}
	return reqs[len(reqs)-1]
}

// stubCallToolRequest builds a CallToolRequest carrying stubMCPToken the way
// the HTTP auth middleware does, so lfxv2.ExtractMCPToken succeeds.
func stubCallToolRequest() *mcp.CallToolRequest {
	return &mcp.CallToolRequest{
		Extra: &mcp.RequestExtra{
			TokenInfo: &auth.TokenInfo{
				Extra: map[string]any{"raw_token": stubMCPToken},
			},
		},
	}
}

// assertExchangedAuth fails unless the request reached the API with the
// exchanged LFX token, not the raw MCP token.
func assertExchangedAuth(t *testing.T, r stubAPIRequest) {
	t.Helper()
	if r.Authorization != "Bearer "+stubExchangedToken {
		t.Errorf("expected Authorization %q, got %q", "Bearer "+stubExchangedToken, r.Authorization)
	}
	if strings.Contains(r.Authorization, stubMCPToken) {
		t.Errorf("raw MCP token leaked to the API: %q", r.Authorization)
	}
}

// resultJSON parses the last text content block of a tool result as JSON.
func resultJSON(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected a result with content")
	}
	text, ok := res.Content[len(res.Content)-1].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[len(res.Content)-1])
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text.Text)
	}
	return out
}

// allResultText concatenates every text content block of a tool result.
func allResultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("expected a result")
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
