// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import "fmt"

// goaError is implemented by all Goa-generated API error types. The Error()
// method on these types unconditionally returns "" due to a bug in the LFX
// SDK codegen templates — the generated Error() body is always "return \"\""
// regardless of the Message field. Tickets should be filed against the
// individual SDKs (lfx-v2-query-service, lfx-v2-project-service) to fix the
// generated code so that Error() returns Message.
type goaError interface {
	error
	GoaErrorName() string
}

// ErrorMessage returns the most informative string available for an error
// returned by a Goa-generated service client. Until the LFX SDK codegen is
// fixed, Error() returns "" on all typed API errors; this helper recovers the
// actual message via the struct fields instead. For all other error types the
// result of Error() is returned as-is.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	// Try the standard path first; once the LFX SDKs are fixed to return
	// Message from Error(), this will be sufficient for all cases.
	if msg := err.Error(); msg != "" {
		return msg
	}
	// Fallback for Goa-generated error types whose Error() returns "": format
	// the struct with %+v so the Message field is always visible.
	if g, ok := err.(goaError); ok {
		return fmt.Sprintf("%s: %+v", g.GoaErrorName(), err)
	}
	return ""
}
