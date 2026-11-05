// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
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
type strictJSONDecoder struct {
	body io.Reader
}

// Decode implements goahttp.Decoder.
func (d strictJSONDecoder) Decode(v any) error {
	dec := json.NewDecoder(d.body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if strings.HasPrefix(err.Error(), unknownFieldErrorPrefix) {
			return &strictUnknownFieldsError{err: err}
		}
		return err
	}
	return nil
}

// strictAwareDecoder wraps a Goa response decoder factory (such as one
// returned by refusalAwareDecoder) so that a successful (2xx) JSON response to
// a request whose context was marked by WithStrictDecoding is decoded with
// DisallowUnknownFields instead of the inner factory's decoder. Every other
// response — non-2xx status, non-JSON content, or a request not marked
// strict — is decoded exactly as the inner factory would.
func strictAwareDecoder(inner func(*http.Response) goahttp.Decoder) func(*http.Response) goahttp.Decoder {
	return func(resp *http.Response) goahttp.Decoder {
		base := inner(resp)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return base
		}
		if resp.Request == nil || !isStrictDecoding(resp.Request.Context()) || !isJSONResponse(resp) {
			return base
		}
		return strictJSONDecoder{body: resp.Body}
	}
}

// isJSONResponse reports whether resp's Content-Type is (or defaults to)
// application/json, the only content type DisallowUnknownFields applies to.
func isJSONResponse(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		// goahttp.ResponseDecoder defaults to JSON when unset.
		return true
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}
