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
	"strconv"
	"strings"

	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"
)

// ErrAccessRefused is returned (wrapped) when an upstream LFX v2 service call
// is answered with HTTP 401 or 403 on an endpoint that declares that status,
// and the response carries no service-authored message. A status the endpoint
// does not declare surfaces as Goa's invalid_response error instead; see
// IsRefusalWithoutServiceMessage. Match it with errors.Is; UpstreamStatus
// tells a 401 from a 403.
var ErrAccessRefused = errors.New("request refused without a service message")

// refusalError is the error refusalAwareDecoder returns for a 401 or 403
// without a service-authored message. It matches ErrAccessRefused and keeps
// the status, so UpstreamStatus can tell the two apart.
type refusalError struct {
	status int
}

// Error returns the ErrAccessRefused text followed by the HTTP status.
func (e *refusalError) Error() string {
	return fmt.Sprintf("%s (HTTP %d)", ErrAccessRefused, e.status)
}

// Is reports whether target is ErrAccessRefused.
func (e *refusalError) Is(target error) bool {
	return target == ErrAccessRefused
}

// IsRefusalWithoutServiceMessage reports whether err is an upstream 401 or 403
// that carries no service-authored message. That is either ErrAccessRefused
// (a status the endpoint declares) or Goa's invalid_response error for a 401
// or 403 the endpoint does not declare, whose body, if any, is not a JSON
// object with a non-empty "message".
func IsRefusalWithoutServiceMessage(err error) bool {
	if errors.Is(err, ErrAccessRefused) {
		return true
	}
	status, body, ok := invalidResponse(err)
	if !ok || (status != http.StatusUnauthorized && status != http.StatusForbidden) {
		return false
	}
	return !hasServiceMessage([]byte(body))
}

// UpstreamStatus returns the HTTP status an LFX v2 service answered with when
// err shows that status was 401, 403 or 404, and 0 otherwise. It reads the
// three forms such an answer takes once a Goa-generated client has handled it:
//   - a 401 or 403 without a service-authored message (ErrAccessRefused),
//     which keeps its status;
//   - Goa's invalid_response error, for a status the endpoint does not declare;
//   - the service's typed error, for a status the endpoint declares, by its
//     Goa error name: "Unauthorized", "Forbidden", or "NotFound". Every
//     client this module uses names its typed 404 "NotFound"; only the
//     meeting service declares "Unauthorized".
//
// A declared status whose body the client cannot decode or validate surfaces
// as Goa's decoding or validation error, which does not carry the status.
func UpstreamStatus(err error) int {
	var refusal *refusalError
	if errors.As(err, &refusal) {
		return refusal.status
	}
	if status, _, ok := invalidResponse(err); ok {
		switch status {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return status
		}
		return 0
	}
	var named goa.GoaErrorNamer
	if errors.As(err, &named) {
		switch named.GoaErrorName() {
		case "Unauthorized":
			return http.StatusUnauthorized
		case "Forbidden":
			return http.StatusForbidden
		case "NotFound":
			return http.StatusNotFound
		}
	}
	return 0
}

// invalidResponse returns the status and body of Goa's invalid_response error,
// which goahttp.ErrInvalidResponse formats as "invalid response code <status>"
// followed by ", body: <body>" only when the body is not empty. ok is false
// when err is not such an error.
func invalidResponse(err error) (status int, body string, ok bool) {
	var clientErr *goahttp.ClientError
	if !errors.As(err, &clientErr) || clientErr.Name != "invalid_response" {
		return 0, "", false
	}
	rest, found := strings.CutPrefix(clientErr.Message, "invalid response code ")
	if !found {
		return 0, "", false
	}
	code, body, _ := strings.Cut(rest, ", body: ")
	status, convErr := strconv.Atoi(code)
	if convErr != nil {
		return 0, "", false
	}
	return status, body, true
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
			return errDecoder{err: &refusalError{status: resp.StatusCode}}
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
