// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client utilities for interacting with LFX v2 APIs, including OAuth2 token exchange.
package lfxv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	goahttp "goa.design/goa/v3/http"
)

// ErrAccessRefused is returned (wrapped) when an upstream LFX v2 service call
// is answered with HTTP 401 or 403 on an endpoint that declares that status,
// and the response carries no service-authored message. A status the endpoint
// does not declare surfaces as Goa's invalid_response error instead; see
// IsRefusalWithoutServiceMessage. Match it with errors.Is.
var ErrAccessRefused = errors.New("request refused without a service message")

// IsRefusalWithoutServiceMessage reports whether err is an upstream 401 or 403
// that carries no service-authored message. That is either ErrAccessRefused
// (a status the endpoint declares) or Goa's invalid_response error for a 401
// or 403 the endpoint does not declare, whose body, if any, is not a JSON
// object with a non-empty "message".
func IsRefusalWithoutServiceMessage(err error) bool {
	if errors.Is(err, ErrAccessRefused) {
		return true
	}
	var clientErr *goahttp.ClientError
	if !errors.As(err, &clientErr) || clientErr.Name != "invalid_response" {
		return false
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		// goahttp.ErrInvalidResponse formats the message as
		// "invalid response code <status>" and appends ", body: <body>"
		// only when the body is not empty.
		prefix := fmt.Sprintf("invalid response code %d", status)
		if clientErr.Message == prefix {
			return true
		}
		if body, ok := strings.CutPrefix(clientErr.Message, prefix+", body: "); ok {
			return !hasServiceMessage([]byte(body))
		}
	}
	return false
}

// refusalAwareDecoder returns the response decoder factory for a Goa-generated
// LFX v2 service client. The factory behaves like goahttp.ResponseDecoder
// except for HTTP 401 and 403 responses. Goa only decodes those on an endpoint
// that declares the status, and then validates the decoded body: a non-empty
// string "message" is always required, and extraFields names the other string
// fields the service's generated 401/403 body validators require (the meeting
// service requires "code"; the committee service requires nothing else). A
// body that meets those rules is decoded by goahttp.ResponseDecoder as before,
// so the service's own error type and message are kept. Any other body fails
// with ErrAccessRefused instead of a JSON syntax, EOF or validation error.
func refusalAwareDecoder(extraFields ...string) func(*http.Response) goahttp.Decoder {
	return func(resp *http.Response) goahttp.Decoder {
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return goahttp.ResponseDecoder(resp)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errDecoder{err: err}
		}
		// Put the body back so the standard decoder can read it.
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if !hasServiceMessage(body, extraFields...) {
			return errDecoder{err: fmt.Errorf("%w (HTTP %d)", ErrAccessRefused, resp.StatusCode)}
		}
		return goahttp.ResponseDecoder(resp)
	}
}

// hasServiceMessage reports whether body is a JSON object with a non-empty
// string "message" field and, for each name in extraFields, a string field of
// that name (which may be empty).
func hasServiceMessage(body []byte, extraFields ...string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return false
	}
	message, ok := stringField(fields, "message")
	if !ok || strings.TrimSpace(message) == "" {
		return false
	}
	for _, name := range extraFields {
		if _, ok := stringField(fields, name); !ok {
			return false
		}
	}
	return true
}

// stringField returns the string value of fields[name], and false when the
// field is absent, null or not a string.
func stringField(fields map[string]json.RawMessage, name string) (string, bool) {
	raw, ok := fields[name]
	if !ok {
		return "", false
	}
	var v *string
	if err := json.Unmarshal(raw, &v); err != nil || v == nil {
		return "", false
	}
	return *v, true
}

// errDecoder is a goahttp.Decoder whose Decode always returns err.
type errDecoder struct {
	err error
}

// Decode returns the decoder's error without reading anything.
func (d errDecoder) Decode(any) error {
	return d.err
}
