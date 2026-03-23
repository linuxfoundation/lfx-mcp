// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newToolLogger returns a logger that writes to both the MCP client session
// (so the AI sees log output) and the server's default system logger (so
// operators see it in server stdout/stderr).
//
// Use this for all tool log calls that matter to operators — errors, warnings,
// and key informational events. For messages that are only useful to the MCP
// client (e.g. "N results were filtered by permissions"), call
// newToolLogger(req) directly instead.
func newToolLogger(req *mcp.CallToolRequest) *slog.Logger {
	mcpHandler := mcp.NewLoggingHandler(req.Session, nil)
	sysHandler := slog.Default().Handler()
	return slog.New(&teeHandler{mcp: mcpHandler, sys: sysHandler})
}

// teeHandler is an slog.Handler that forwards every record to two handlers.
type teeHandler struct {
	mcp slog.Handler
	sys slog.Handler
}

func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.mcp.Enabled(ctx, level) || t.sys.Enabled(ctx, level)
}

func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	// Best-effort: forward to both, return the first non-nil error.
	var firstErr error
	if t.sys.Enabled(ctx, r.Level) {
		if err := t.sys.Handle(ctx, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if t.mcp.Enabled(ctx, r.Level) {
		if err := t.mcp.Handle(ctx, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{mcp: t.mcp.WithAttrs(attrs), sys: t.sys.WithAttrs(attrs)}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{mcp: t.mcp.WithGroup(name), sys: t.sys.WithGroup(name)}
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
