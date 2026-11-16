// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"context"
	"errors"
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

// errorMessage is deliberately narrower than the real error body so
// unexpected_new_field would trip DisallowUnknownFields if the non-2xx guard
// in strictAwareDecoder were ever removed; map[string]any would not catch
// that, since DisallowUnknownFields only rejects fields a struct doesn't
// declare.
type errorMessage struct {
	Message string `json:"message"`
}

func TestStrictAwareDecoder_IgnoresNon2xxEvenWhenStrict(t *testing.T) {
	ctx := WithStrictDecoding(context.Background())
	resp := newJSONResponse(ctx, http.StatusNotFound, `{"message":"not found","unexpected_new_field":"x"}`)

	decode := strictAwareDecoder(refusalAwareDecoder())
	var out errorMessage
	if err := decode(resp).Decode(&out); err != nil {
		t.Fatalf("a non-2xx response must not be strictly decoded, got: %v", err)
	}
}

// newResponseWithContentType is like newJSONResponse but sets (or omits) an
// arbitrary Content-Type, for exercising isJSONResponse's media-type branches.
func newResponseWithContentType(ctx context.Context, contentType, body string) *http.Response {
	req := httptest.NewRequest(http.MethodGet, "http://committee.test/committees/c", nil).WithContext(ctx)
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestStrictAwareDecoder_PlusJSONSuffixIsStrict(t *testing.T) {
	ctx := WithStrictDecoding(context.Background())
	resp := newResponseWithContentType(ctx, "application/vnd.committee+json", `{"name":"Example","unexpected_new_field":"x"}`)

	decode := strictAwareDecoder(refusalAwareDecoder())
	var out knownFields
	err := decode(resp).Decode(&out)
	if err == nil {
		t.Fatal("want an error for an unknown field under a +json content type")
	}
	if !strings.Contains(err.Error(), "unexpected_new_field") {
		t.Errorf("error should name the unknown field, got: %v", err)
	}
}

func TestStrictAwareDecoder_NonJSONContentTypeSkipsStrictDecoding(t *testing.T) {
	ctx := WithStrictDecoding(context.Background())
	resp := newResponseWithContentType(ctx, "text/plain", `{"name":"Example","unexpected_new_field":"x"}`)

	decode := strictAwareDecoder(refusalAwareDecoder())
	var out knownFields
	err := decode(resp).Decode(&out)
	// goahttp.ResponseDecoder doesn't know how to decode text/plain at all, so
	// this still errors; the point of this test is that it must not be a
	// strictUnknownFieldsError, since isJSONResponse should route a non-JSON
	// content type to the inner (non-strict) decoder regardless.
	var unknownFieldErr *strictUnknownFieldsError
	if errors.As(err, &unknownFieldErr) {
		t.Fatalf("a non-JSON content type must not be strictly decoded, got: %v", err)
	}
}

func TestStrictAwareDecoder_StrictWhenContentTypeAbsent(t *testing.T) {
	ctx := WithStrictDecoding(context.Background())
	resp := newResponseWithContentType(ctx, "", `{"name":"Example","unexpected_new_field":"x"}`)

	decode := strictAwareDecoder(refusalAwareDecoder())
	var out knownFields
	err := decode(resp).Decode(&out)
	if err == nil {
		t.Fatal("want an error for an unknown field when Content-Type is absent, since goahttp.ResponseDecoder defaults to JSON")
	}
	if !strings.Contains(err.Error(), "unexpected_new_field") {
		t.Errorf("error should name the unknown field, got: %v", err)
	}
}
