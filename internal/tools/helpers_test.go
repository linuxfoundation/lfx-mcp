// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
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
