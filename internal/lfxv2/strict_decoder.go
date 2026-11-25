// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client utilities for LFX v2 APIs, including strict
// response decoding for fetch-merge-put writes.
package lfxv2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	goahttp "goa.design/goa/v3/http"
)

// strictDecodingContextKey is the context key used by WithStrictDecoding.
type strictDecodingContextKey struct{}

// WithStrictDecoding returns a context that, when used for an LFX v2 API call
// whose client was built with strictAwareDecoder, causes a successful (2xx)
// JSON response to be decoded with json.Decoder.DisallowUnknownFields.
//
// This exists for fetch-merge-put write handlers: they GET a resource, merge
// the caller's fields over it, and PUT the whole thing back. If the service
// has added a field the pinned client module predates, the default decoder
// silently drops it, the merge never carries it forward, and the PUT erases
// it — with a valid ETag and no error. Calling this before such a GET turns
// that silent data loss into a refused write instead.
//
// Plain read tools must not set this: a newer, read-only field added upstream
// would otherwise block them too. Set it only immediately before the GET that
// precedes a client-side merge-and-PUT.
func WithStrictDecoding(ctx context.Context) context.Context {
	return context.WithValue(ctx, strictDecodingContextKey{}, true)
}

// isStrictDecoding reports whether ctx was marked by WithStrictDecoding.
func isStrictDecoding(ctx context.Context) bool {
	strict, _ := ctx.Value(strictDecodingContextKey{}).(bool)
	return strict
}

// unknownFieldErrorPrefix is the text encoding/json's DisallowUnknownFields
// prefixes its error with; there is no typed error to match against.
const unknownFieldErrorPrefix = "json: unknown field "

// strictUnknownFieldsError is returned by strictJSONDecoder.Decode when the
// response contains a field the client's generated type does not know about.
// Its text is deliberately explicit about the risk, since it ends up inside a
// Goa decoding_error and from there inside a tool's error result.
type strictUnknownFieldsError struct {
	err error
}

// Error describes the refusal and keeps the underlying encoding/json message.
func (e *strictUnknownFieldsError) Error() string {
	field := strings.TrimPrefix(e.err.Error(), unknownFieldErrorPrefix)
	return fmt.Sprintf(
		"the service returned a field this server version does not know about (%s); refusing to use the response to avoid erasing it on write",
		field,
	)
}

// Unwrap returns the underlying encoding/json error.
func (e *strictUnknownFieldsError) Unwrap() error {
	return e.err
}

// strictJSONDecoder is a goahttp.Decoder that decodes JSON with
// DisallowUnknownFields, turning an unknown field into a
// strictUnknownFieldsError.
//
// original is the body as the service sent it and body what the inner
// decoder factory left to decode, which differs when that factory rewrites
// the body (the meeting client's record cleaning removes members). The
// field names are checked on original, so a member the rewrite removed is
// still seen; the values are decoded from body.
type strictJSONDecoder struct {
	original []byte
	body     io.Reader
}

// Decode implements goahttp.Decoder.
func (d strictJSONDecoder) Decode(v any) error {
	if err := checkFieldNames(d.original, v); err != nil {
		return err
	}
	return strictDecode(d.body, v)
}

// strictDecode decodes body into v with DisallowUnknownFields.
func strictDecode(body io.Reader, v any) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if strings.HasPrefix(err.Error(), unknownFieldErrorPrefix) {
			return &strictUnknownFieldsError{err: err}
		}
		return err
	}
	return nil
}

// checkFieldNames reports a member name in body, at any depth, that the type
// v points to does not know. It looks at names only: every value that is not
// an object or an array is replaced by null before decoding, so a value the
// type would reject cannot hide a name that follows it. A body that is not
// JSON is left for the value decode to report.
func checkFieldNames(body []byte, v any) error {
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() != reflect.Pointer {
		return nil
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	names, err := json.Marshal(namesOnly(doc))
	if err != nil {
		return err
	}
	return strictDecode(bytes.NewReader(names), reflect.New(t.Elem()).Interface())
}

// namesOnly returns doc with its objects and arrays kept and every other
// value replaced by null.
func namesOnly(doc any) any {
	switch t := doc.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for name, value := range t {
			out[name] = namesOnly(value)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, value := range t {
			out[i] = namesOnly(value)
		}
		return out
	default:
		return nil
	}
}

// strictAwareDecoder wraps a Goa response decoder factory (such as one
// returned by refusalAwareDecoder) so that a successful (2xx) JSON response to
// a request whose context was marked by WithStrictDecoding is decoded with
// DisallowUnknownFields instead of the inner factory's decoder. Every other
// response — non-2xx status, non-JSON content, or a request not marked
// strict — is decoded exactly as the inner factory would.
//
// The inner factory still runs for a strict response, because it may rewrite
// the body before the values are decoded. The field names are checked on the
// body as it arrived, read here before the inner factory sees it.
func strictAwareDecoder(inner func(*http.Response) goahttp.Decoder) func(*http.Response) goahttp.Decoder {
	return func(resp *http.Response) goahttp.Decoder {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return inner(resp)
		}
		if resp.Request == nil || !isStrictDecoding(resp.Request.Context()) || !isJSONResponse(resp) {
			return inner(resp)
		}
		original, err := io.ReadAll(resp.Body)
		if err != nil {
			return errDecoder{err: err}
		}
		resp.Body = io.NopCloser(bytes.NewReader(original))
		inner(resp)
		return strictJSONDecoder{original: original, body: resp.Body}
	}
}

// isJSONResponse reports whether resp's Content-Type is (or defaults to)
// application/json, the only content type DisallowUnknownFields applies to.
//
// This only inspects the literal header, not Goa's content-negotiation
// fallback: goahttp.ResponseEncoder (the server side of every Goa HTTP
// service, including every LFX v2 service this module's clients call) always
// writes an explicit Content-Type on every response, defaulting to
// application/json only when negotiation doesn't resolve to something else —
// it never leaves the header absent or sets an unrecognized value that then
// falls back to JSON on the wire. A response can only arrive here without a
// recognized JSON content type if the handler's own Goa design declares a
// different one for that response (checked for the committee-service GETs
// this module marks strict: none do), or an intermediary (proxy, gateway)
// rewrites or strips the header after the service sets it. Matching Goa's
// internal fallback selection here would mask that second case — the one
// scenario where the Content-Type header genuinely doesn't describe the
// body — behind another assumption instead of surfacing it. If this decoder
// is reused against a Goa service whose design sets a non-default
// Content-Type on a GET this fetch-merge-put pattern reads, verify that
// endpoint's actual response the same way before trusting isJSONResponse's
// absent-header default for it.
func isJSONResponse(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		// goahttp.ResponseDecoder defaults to JSON when unset; this only
		// happens here if something between the service and this client
		// stripped the header the service set, not because the service left
		// it off.
		return true
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	// +json is accepted to match goahttp.ResponseEncoder's own fallback
	// (e.g. "application/vnd.committee+json" encodes the same as
	// application/json), not because any LFX v2 service in use today
	// declares such a type.
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}
