// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFriendlyAPIError_403(t *testing.T) {
	err := errors.New("[project-service get-one-project-base]: invalid response code 403")
	got := friendlyAPIError("failed to get project", err)
	want := "Failed to get project: " + accessDeniedMessage
	if got != want {
		t.Errorf("expected %q, got: %q", want, got)
	}
}

func TestFriendlyAPIError_403_embedded(t *testing.T) {
	// 403 buried deeper in a wrapped error string.
	err := errors.New("outer: inner: invalid response code 403: forbidden")
	got := friendlyAPIError("failed to do thing", err)
	want := "Failed to do thing: " + accessDeniedMessage
	if got != want {
		t.Errorf("expected %q, got: %q", want, got)
	}
}

func TestFriendlyAPIError_passthrough(t *testing.T) {
	err := errors.New("invalid response code 500: internal server error")
	got := friendlyAPIError("failed to get project", err)
	want := "Failed to get project: invalid response code 500: internal server error"
	if got != want {
		t.Errorf("expected %q, got: %q", want, got)
	}
}

func TestFriendlyAPIError_404_passthrough(t *testing.T) {
	err := errors.New("invalid response code 404: not found")
	got := friendlyAPIError("failed to get member", err)
	want := "Failed to get member: invalid response code 404: not found"
	if got != want {
		t.Errorf("expected %q, got: %q", want, got)
	}
}

func TestFriendlyAPIError_noPrefix(t *testing.T) {
	// Non-403 errors must NOT start with "Error: ".
	err := errors.New("connection refused")
	got := friendlyAPIError("failed to search projects", err)
	if len(got) >= 7 && got[:7] == "Error: " {
		t.Errorf("result must not start with 'Error: ', got: %q", got)
	}
}

func TestFriendlyAPIError_accessDeniedNoPrefix(t *testing.T) {
	// 403 result must NOT start with "Error: " either.
	err := errors.New("response code 403")
	got := friendlyAPIError("failed to get project", err)
	if len(got) >= 7 && got[:7] == "Error: " {
		t.Errorf("access denied message must not start with 'Error: ', got: %q", got)
	}
}

func TestSlugResolveError_notFound(t *testing.T) {
	// ErrProjectNotFound must map to accessDeniedMessage, not the raw sentinel.
	err := fmt.Errorf("%w for slug %q", lfxv2.ErrProjectNotFound, "aaif")
	got := slugResolveError("aaif", err)
	if !strings.Contains(got.Error(), accessDeniedMessage) {
		t.Errorf("expected accessDeniedMessage in error, got: %q", got)
	}
	// Must not expose internal implementation detail ("resolve", "not found", etc.).
	if strings.Contains(got.Error(), "resolve") || strings.Contains(got.Error(), "not found") {
		t.Errorf("internal slug resolution detail must not leak to user, got: %q", got)
	}
}

func TestSlugResolveError_transportError(t *testing.T) {
	// Non-not-found errors must NOT be rewritten to accessDeniedMessage.
	err := errors.New("connection refused")
	got := slugResolveError("aaif", err)
	if strings.Contains(got.Error(), accessDeniedMessage) {
		t.Errorf("transport error must not become accessDeniedMessage, got: %q", got)
	}
	if !strings.Contains(got.Error(), "connection refused") {
		t.Errorf("transport error text must be preserved, got: %q", got)
	}
}

func TestSlugResolveError_contextCanceled(t *testing.T) {
	// Context errors must NOT be rewritten to accessDeniedMessage.
	err := fmt.Errorf("query failed: %w", errors.New("context canceled"))
	got := slugResolveError("tlf", err)
	if strings.Contains(got.Error(), accessDeniedMessage) {
		t.Errorf("context error must not become accessDeniedMessage, got: %q", got)
	}
}

// TestNewToolLogger_NilSessionUsesServerHandlerOnly pins the guard that lets
// handlers be exercised directly in unit tests: with no MCP session the
// logger must not tee into mcp.LoggingHandler (whose Enabled dereferences
// the session) and must still deliver records to the server-side handler.
func TestNewToolLogger_NilSessionUsesServerHandlerOnly(t *testing.T) {
	var buf bytes.Buffer
	sys := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := WithLogger(context.Background(), sys)

	for name, req := range map[string]*mcp.CallToolRequest{
		"nil request": nil,
		"nil session": {},
		"no session":  {Session: nil, Extra: &mcp.RequestExtra{}},
	} {
		t.Run(name, func(t *testing.T) {
			buf.Reset()
			logger := newToolLogger(ctx, req)
			if _, isTee := logger.Handler().(*teeHandler); isTee {
				t.Fatal("without a session the logger must not tee into the MCP handler")
			}
			logger.InfoContext(ctx, "probe", "k", "v")
			if !strings.Contains(buf.String(), "probe") || !strings.Contains(buf.String(), "k=v") {
				t.Errorf("record did not reach the server-side handler: %q", buf.String())
			}
		})
	}
}

// TestUpstreamErrorText_GoaTypedErrorRendering distinguishes blank typed
// errors that need detail from nonblank errors whose text must be preserved.
func TestUpstreamErrorText_GoaTypedErrorRendering(t *testing.T) {
	blank := &querysvc.BadRequestError{Message: "invalid date_from"}
	nonblank := &committeeservice.ForbiddenError{Message: "organization grant required"}
	if blank.Error() != "" || nonblank.Error() != "Forbidden" {
		t.Fatal("fixture error methods do not match the vendored Goa contract")
	}

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"blank_direct", blank, "BadRequest: invalid date_from"},
		{"blank_wrapped", fmt.Errorf("outer: %w", blank), "outer: BadRequest: invalid date_from"},
		{"blank_nested", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", blank)), "outer: inner: BadRequest: invalid date_from"},
		{"nonblank_direct", nonblank, "Forbidden"},
		{"nonblank_wrapped", fmt.Errorf("outer: %w", nonblank), "outer: Forbidden"},
		{"nonblank_nested", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", nonblank)), "outer: inner: Forbidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := upstreamErrorText(tc.err); got != tc.want {
				t.Errorf("upstreamErrorText = %q, want %q", got, tc.want)
			}
			if got := friendlyAPIError("failed to get resource", tc.err); got != "Failed to get resource: "+tc.want {
				t.Errorf("friendlyAPIError = %q, want %q", got, "Failed to get resource: "+tc.want)
			}
		})
	}
}

// TestFriendlyAPIError_GoaTypedErrorsAreNotBlank pins that the Goa-generated
// typed errors (whose Error() returns "") reach the caller with their name and
// message instead of a bare "<Op>: ".
func TestFriendlyAPIError_GoaTypedErrorsAreNotBlank(t *testing.T) {
	for name, err := range map[string]error{
		"query bad request":   &querysvc.BadRequestError{Message: "date_from must be ISO 8601"},
		"query internal":      &querysvc.InternalServerError{Message: "search backend unavailable"},
		"committee not found": &committeeservice.NotFoundError{Message: "organization not found"},
		"committee 503":       &committeeservice.ServiceUnavailableError{Message: "try again"},
		"wrapped":             fmt.Errorf("outer: %w", &querysvc.BadRequestError{Message: "bad parent"}),
	} {
		t.Run(name, func(t *testing.T) {
			got := friendlyAPIError("failed to count resources", err)
			if got == "Failed to count resources: " || !strings.HasPrefix(got, "Failed to count resources: ") {
				t.Fatalf("blank or malformed message: %q", got)
			}
			body := strings.TrimPrefix(got, "Failed to count resources: ")
			if !strings.Contains(body, "Error") && !strings.Contains(body, "BadRequest") && !strings.Contains(body, "NotFound") && !strings.Contains(body, "Internal") && !strings.Contains(body, "ServiceUnavailable") {
				t.Errorf("Goa error name missing: %q", got)
			}
			for _, want := range []string{"ISO 8601", "unavailable", "not found", "try again", "bad parent"} {
				if strings.Contains(fmt.Sprintf("%+v", err), want) && !strings.Contains(got, want) {
					t.Errorf("upstream message %q missing from %q", want, got)
				}
			}
		})
	}
	// A blank non-Goa error still yields a non-blank sentence.
	if got := friendlyAPIError("failed to x", errors.New("")); got == "Failed to x: " {
		t.Errorf("blank error must not yield a blank message, got %q", got)
	}
}
