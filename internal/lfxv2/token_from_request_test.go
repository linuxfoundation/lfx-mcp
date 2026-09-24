// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

func TestTokenFromRequest_StaticTokenAcceptsNilTokenInfo(t *testing.T) {
	c := &Clients{staticLFXToken: "static-token-value"}

	ctx := context.Background()
	gotCtx, err := c.TokenFromRequest(ctx, nil)
	if err != nil {
		t.Fatalf("a configured static token must accept a nil TokenInfo (stdio has no MCP OAuth), got error: %v", err)
	}
	if gotCtx != ctx {
		t.Error("with a static token configured, the input context must be returned unchanged: the auth interceptor uses the static token directly, not one derived from ctx")
	}
	if mcpTokenFromContext(gotCtx) != "" {
		t.Error("a static-token context must not carry an MCP token: none was extracted or needed")
	}
}

func TestTokenFromRequest_StaticTokenSentUnchangedWithoutExchange(t *testing.T) {
	c := &Clients{staticLFXToken: "static-token-value"}

	got, err := c.GetExchangedToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "static-token-value" {
		t.Errorf("a configured static token must be returned as-is, without token exchange: got %q", got)
	}
}

func TestTokenFromRequest_NoAuthenticationSourceReturnsError(t *testing.T) {
	c := &Clients{}

	ctx := context.Background()
	gotCtx, err := c.TokenFromRequest(ctx, nil)
	if err == nil {
		t.Fatal("omitting both a TokenInfo and a static token must return an error")
	}
	if gotCtx != ctx {
		t.Error("even on error, TokenFromRequest must return the original context, not nil: callers pass the returned context straight into context-aware logging (e.g. logger.ErrorContext), which panics on a nil context")
	}
}

func TestTokenFromRequest_ExtractMCPTokenErrorReturnsOriginalContext(t *testing.T) {
	c := &Clients{}

	ctx := context.Background()
	// A non-nil TokenInfo with no Extra map makes ExtractMCPToken fail.
	gotCtx, err := c.TokenFromRequest(ctx, &auth.TokenInfo{})
	if err == nil {
		t.Fatal("a TokenInfo with no Extra map must fail token extraction")
	}
	if gotCtx != ctx {
		t.Error("on an MCP-token extraction error, TokenFromRequest must still return the original context, not nil, for the same context-aware-logging reason as the no-authentication-source case")
	}
}

func TestTokenFromRequest_OAuthTokenAttachesMCPTokenToContext(t *testing.T) {
	c := &Clients{}

	tokenInfo := &auth.TokenInfo{Extra: map[string]any{"raw_token": "mcp-bearer-token"}}
	gotCtx, err := c.TokenFromRequest(context.Background(), tokenInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mcpTokenFromContext(gotCtx); got != "mcp-bearer-token" {
		t.Errorf("the extracted MCP token must be attached to the returned context: got %q", got)
	}
}

func TestGetExchangedToken_NoStaticTokenAndNoContextTokenReturnsError(t *testing.T) {
	c := &Clients{}

	if _, err := c.GetExchangedToken(context.Background()); err == nil {
		t.Fatal("with no static token configured and no MCP token in context, GetExchangedToken must return an error")
	}
}

// Sanity check that the two "no authentication configured" errors are distinct
// user-facing messages rather than accidental duplicates or wrapped copies of
// each other, since they cover different call paths (TokenFromRequest vs.
// GetExchangedToken).
func TestTokenFromRequestAndGetExchangedToken_ErrorsAreDistinct(t *testing.T) {
	c := &Clients{}

	_, tokenFromRequestErr := c.TokenFromRequest(context.Background(), nil)
	_, getExchangedTokenErr := c.GetExchangedToken(context.Background())

	if tokenFromRequestErr == nil || getExchangedTokenErr == nil {
		t.Fatal("both calls must fail with no authentication source configured")
	}
	if errors.Is(tokenFromRequestErr, getExchangedTokenErr) || tokenFromRequestErr.Error() == getExchangedTokenErr.Error() {
		t.Error("the two errors should carry distinct messages since they originate from different call paths")
	}
}
