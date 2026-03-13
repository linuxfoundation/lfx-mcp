// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

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
