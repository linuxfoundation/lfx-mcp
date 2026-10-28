// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newJSONResponse builds an *http.Response whose Request carries ctx, for
// exercising a decoder factory the way a Goa client would call it.
func newJSONResponse(ctx context.Context, status int, body string) *http.Response {
	req := httptest.NewRequest(http.MethodGet, "http://committee.test/committees/c", nil).WithContext(ctx)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

type knownFields struct {
	Name string `json:"name"`
}

func TestStrictAwareDecoder_RefusesUnknownFieldWhenStrict(t *testing.T) {
	ctx := WithStrictDecoding(context.Background())
	resp := newJSONResponse(ctx, http.StatusOK, `{"name":"Example","unexpected_new_field":"x"}`)

	decode := strictAwareDecoder(refusalAwareDecoder())
	var out knownFields
	err := decode(resp).Decode(&out)
	if err == nil {
		t.Fatal("want an error for an unknown field under strict decoding")
	}
	if !strings.Contains(err.Error(), "unexpected_new_field") {
		t.Errorf("error should name the unknown field, got: %v", err)
	}
	if !strings.Contains(err.Error(), "refusing to use the response") {
		t.Errorf("error should explain the refusal, got: %v", err)
	}
}

func TestStrictAwareDecoder_AllowsKnownFieldsWhenStrict(t *testing.T) {
	ctx := WithStrictDecoding(context.Background())
	resp := newJSONResponse(ctx, http.StatusOK, `{"name":"Example"}`)

	decode := strictAwareDecoder(refusalAwareDecoder())
	var out knownFields
	if err := decode(resp).Decode(&out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "Example" {
		t.Errorf("name: want Example, got %q", out.Name)
	}
}

func TestStrictAwareDecoder_TolerantWithoutStrictContext(t *testing.T) {
	resp := newJSONResponse(context.Background(), http.StatusOK, `{"name":"Example","unexpected_new_field":"x"}`)

	decode := strictAwareDecoder(refusalAwareDecoder())
	var out knownFields
	if err := decode(resp).Decode(&out); err != nil {
		t.Fatalf("a request not marked strict must tolerate unknown fields, got: %v", err)
	}
	if out.Name != "Example" {
		t.Errorf("name: want Example, got %q", out.Name)
	}
}

func TestStrictAwareDecoder_IgnoresNon2xxEvenWhenStrict(t *testing.T) {
	ctx := WithStrictDecoding(context.Background())
	resp := newJSONResponse(ctx, http.StatusNotFound, `{"message":"not found","unexpected_new_field":"x"}`)

	decode := strictAwareDecoder(refusalAwareDecoder())
	var out map[string]any
	if err := decode(resp).Decode(&out); err != nil {
		t.Fatalf("a non-2xx response must not be strictly decoded, got: %v", err)
	}
}
