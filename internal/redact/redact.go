// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package redact masks credential header values, signed-link and join-link
// query parameters, and meeting passcode fields in the HTTP wire dumps and
// URLs that the outbound debug transports write to the log. Other content is
// logged as is.
//
// It only ever produces a masked copy for logging: callers keep sending and
// receiving the original bytes.
package redact

import (
	"regexp"
	"strings"
)

// Mask replaces every masked value in the logged copy.
const Mask = "[REDACTED]"

// credentialHeaders are the header names, in lower case, whose values are
// masked in a dump. Authorization is the only one the clients set today; the
// others are masked as well because they carry credentials by definition.
var credentialHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"x-api-key":           true,
}

// schemeHeaders keep their authentication scheme ("Bearer", "Basic") in the
// logged copy, which says what kind of credential was sent without its value.
var schemeHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
}

// signedLinkParam matches a query parameter that makes a URL a temporary
// signed link or a join link, wherever the URL appears: a request line, a
// header, or a body. The names cover S3 pre-signed URLs, which is what the
// meeting attachment upload and download endpoints return (X-Amz-Signature,
// X-Amz-Credential, X-Amz-Security-Token), CDN signed URLs (Signature), the
// generic sig, token and access_token parameters, and pwd, the passcode
// parameter of the join_url on meeting records. The parameter must follow
// "?" or "&", or the escaped forms of "&" that a JSON ("\u0026") or HTML
// ("&amp;", once or more) body uses, so names that merely end in one of these (page_token)
// are kept.
// Parameters that carry no secret, such as X-Amz-Date and X-Amz-Expires,
// are kept because they are what explains an expired link.
var signedLinkParam = regexp.MustCompile(
	`(?i)((?:\?|&(?:amp;)*|\\u0026)(?:x-amz-signature|x-amz-credential|x-amz-security-token|signature|sig|token|access_token|pwd)=)[^&#\s"'\\<>]*`,
)

// passcodeField matches a JSON member whose key is one of the meeting
// passcode or password fields and whose value is a string or a number. The
// keys are the ones meeting and past-meeting records carry for joining or
// hosting a call (passcode, host_key, recording_password, and the join-page
// password and meeting_password). Matching is exact and case-sensitive, like
// the field trimming in the tools package, so other keys are kept.
var passcodeField = regexp.MustCompile(
	`("(?:passcode|host_key|recording_password|password|meeting_password)"\s*:\s*)(?:"(?:[^"\\]|\\.)*"|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?)`,
)

// headerBodySeparator ends the header block of an HTTP/1.x wire dump.
const headerBodySeparator = "\r\n\r\n"

// WireDump returns a copy of an httputil request or response dump with
// credential header values, signed-link and join-link parameters, and meeting
// passcode fields masked. The start line and the headers are read up to the
// first blank line; link parameters are masked everywhere, the body included,
// and passcode fields in the body.
func WireDump(dump []byte) string {
	text := string(dump)
	head, body, hasBody := strings.Cut(text, headerBodySeparator)

	lines := strings.Split(head, "\r\n")
	// lines[0] is the request or status line, never a header.
	for i := 1; i < len(lines); i++ {
		lines[i] = maskHeaderLine(lines[i])
	}
	text = strings.Join(lines, "\r\n")
	if hasBody {
		text += headerBodySeparator + passcodeField.ReplaceAllString(body, `${1}"`+Mask+`"`)
	}
	return URL(text)
}

// URL returns s with the values of signed-link query parameters masked. It
// accepts a URL or any text that contains URLs.
func URL(s string) string {
	return signedLinkParam.ReplaceAllString(s, "${1}"+Mask)
}

// maskHeaderLine masks the value of a "Name: value" line when Name is a
// credential header, keeping the authentication scheme where there is one.
func maskHeaderLine(line string) string {
	name, value, ok := strings.Cut(line, ":")
	if !ok {
		return line
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if !credentialHeaders[key] {
		return line
	}
	if schemeHeaders[key] {
		if scheme, _, hasCred := strings.Cut(strings.TrimSpace(value), " "); hasCred {
			return name + ": " + scheme + " " + Mask
		}
	}
	return name + ": " + Mask
}
