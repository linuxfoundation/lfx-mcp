// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// loggerContextKey is the unexported key type used to store a contextual logger.
type loggerContextKey struct{}

// WithLogger returns a new context with the given logger stored in it.
// The middleware calls this before invoking the tool handler so that
// session_id and mcp_method are pre-bound on every log record.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, l)
}

// loggerFromContext retrieves the contextual logger stored by WithLogger, or
// falls back to slog.Default() when no logger has been stored.
func loggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// newToolLogger returns the server-side contextual logger bound by the
// receiving middleware (session_id/mcp_method pre-bound from context). Use
// logger.XxxContext(ctx, ...) so the active OTel span's trace_id/span_id are
// injected into every log record.
//
// MCP client-side logging (the logging/setLevel capability and
// notifications/message) is a deprecated protocol feature as of the
// 2026-07-28 revision; log to stderr/OTel instead of the client session.
func newToolLogger(ctx context.Context, _ *mcp.CallToolRequest) *slog.Logger {
	return loggerFromContext(ctx)
}

// boolPtr returns a pointer to the given bool value. Used for optional
// annotation fields that distinguish between "unset" and "false".
func boolPtr(b bool) *bool {
	return &b
}

// strPtr returns a pointer to the given string value. Used for optional
// payload fields that distinguish between "unset" and "zero value".
func strPtr(s string) *string {
	return &s
}

// dedupeStrings returns in without repeated values, first occurrence kept.
func dedupeStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// chunkStrings splits in into consecutive slices of at most size elements,
// preserving order. A size below one yields the whole input as one chunk;
// an empty input yields no chunks.
func chunkStrings(in []string, size int) [][]string {
	if len(in) == 0 {
		return nil
	}
	if size < 1 {
		return [][]string{in}
	}
	out := make([][]string, 0, (len(in)+size-1)/size)
	for start := 0; start < len(in); start += size {
		end := start + size
		if end > len(in) {
			end = len(in)
		}
		out = append(out, in[start:end])
	}
	return out
}

// accessDeniedMessage is the user-facing message returned when a downstream
// API call is rejected with HTTP 403.
const accessDeniedMessage = "this resource may not exist or you may not have enough access to complete this operation. Request support @ https://support.lfx.dev"

// slugResolveError maps a slug resolver error to a user-facing error.
// When the error is ErrProjectNotFound (the slug returned no results from the
// query service), it surfaces accessDeniedMessage consistent with other
// access-denied paths. All other errors (transport failures, context
// cancellation, etc.) are wrapped and returned as-is so operators can
// diagnose operational issues.
func slugResolveError(slug string, err error) error {
	if errors.Is(err, lfxv2.ErrProjectNotFound) {
		return fmt.Errorf("failed to get project %q: %s", slug, accessDeniedMessage)
	}
	return fmt.Errorf("failed to resolve project slug %q: %w", slug, err)
}

// goaTypedError is the shape shared by the Goa-generated typed errors.
// Some types have an empty Error() and need GoaErrorName and Message to
// describe the failure; others already provide error text.
type goaTypedError interface {
	error
	GoaErrorName() string
}

// upstreamErrorText returns a non-blank description of err. Only when the
// matched Goa typed error's Error() is empty does it synthesize the name and
// Message, preserving any wrapping context. Nonblank error text is unchanged.
func upstreamErrorText(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	var typed goaTypedError
	if errors.As(err, &typed) && typed.Error() == "" {
		detail := typed.GoaErrorName()
		if msg := goaMessage(typed); msg != "" {
			detail += ": " + msg
		}
		// Wrapping ("outer: %w") leaves a dangling prefix because the inner
		// Error() is ""; append the typed detail so it is never lost.
		text = strings.TrimSpace(text)
		if text == "" || text == ":" {
			return detail
		}
		return strings.TrimSuffix(text, ":") + ": " + detail
	}
	if text == "" {
		return "upstream request failed with no message"
	}
	return text
}

// goaMessage extracts the Message field from a Goa typed error without
// importing every client's error type. All of them are `struct{ Message string }`.
func goaMessage(e goaTypedError) string {
	type messenger interface{ GetMessage() string }
	if m, ok := e.(messenger); ok {
		return m.GetMessage()
	}
	// Goa does not generate a getter; the struct is `struct{ Message string }`.
	// %v/%+v would call the blank Error(), so use %#v, which renders
	// `&pkg.Type{Message:"<text>"}`, and unquote the field.
	s := fmt.Sprintf("%#v", e)
	const key = `Message:"`
	i := strings.Index(s, key)
	if i < 0 {
		return ""
	}
	rest := s[i+len(key):]
	if j := strings.LastIndex(rest, `"}`); j >= 0 {
		rest = rest[:j]
	}
	unquoted, err := strconv.Unquote(`"` + rest + `"`)
	if err != nil {
		return rest
	}
	return unquoted
}

// If the error contains "response code 403" it returns a user-friendly
// access-denied message instead of the raw internal error string.
// The op argument is a short description of the operation (e.g.
// "failed to get project") and is prefixed to both 403 and non-403 error messages.
// Goa typed errors, whose Error() is blank, are rendered through
// upstreamErrorText so the caller never sees an empty message.
func friendlyAPIError(op string, err error) string {
	if len(op) > 0 {
		op = strings.ToUpper(op[:1]) + op[1:]
	}
	text := upstreamErrorText(err)
	if strings.Contains(text, "response code 403") {
		return op + ": " + accessDeniedMessage
	}
	return op + ": " + text
}
