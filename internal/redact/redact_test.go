// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package redact

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

// Test values only: none of these is a real credential.
const (
	testBearer    = "test-bearer-value"
	testSignature = "test-signature-value"
	testCredKey   = "test-key-id%2F20260101%2Fus-east-1%2Fs3%2Faws4_request"
	testSession   = "test-session-value%2B%2F%3D"
)

// signedLink is an S3-style pre-signed link carrying three secret parameters
// and three that are safe to log.
const signedLink = "https://files.example.test/meetings/m-1/agenda.pdf" +
	"?X-Amz-Algorithm=AWS4-HMAC-SHA256" +
	"&X-Amz-Credential=" + testCredKey +
	"&X-Amz-Date=20260101T000000Z" +
	"&X-Amz-Expires=3600" +
	"&X-Amz-Security-Token=" + testSession +
	"&X-Amz-SignedHeaders=host" +
	"&X-Amz-Signature=" + testSignature

func assertNoSecrets(t *testing.T, got string) {
	t.Helper()
	for _, secret := range []string{testBearer, testSignature, testCredKey, testSession} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q must be masked, got:\n%s", secret, got)
		}
	}
}

func assertContains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("want %q kept in the logged copy, got:\n%s", w, got)
		}
	}
}

func TestWireDump_MasksCredentialHeaders(t *testing.T) {
	dump := "POST /committees/c-1/members HTTP/1.1\r\n" +
		"Host: api.example.test\r\n" +
		"Authorization: Bearer " + testBearer + "\r\n" +
		"proxy-authorization: Basic test-basic-value\r\n" +
		"Cookie: session=abc123; theme=dark\r\n" +
		"X-Api-Key: test-api-key-value\r\n" +
		"Content-Type: application/json\r\n" +
		"X-Request-Id: req-42\r\n" +
		"\r\n" +
		`{"name":"Jane","role":"chair"}`

	got := WireDump([]byte(dump))

	assertNoSecrets(t, got)
	for _, secret := range []string{"test-basic-value", "session=abc123", "test-api-key-value"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q must be masked, got:\n%s", secret, got)
		}
	}
	assertContains(t, got,
		"POST /committees/c-1/members HTTP/1.1\r\n",
		"Authorization: Bearer "+Mask+"\r\n",
		"proxy-authorization: Basic "+Mask+"\r\n",
		"Cookie: "+Mask+"\r\n",
		"X-Api-Key: "+Mask+"\r\n",
		"Host: api.example.test\r\n",
		"Content-Type: application/json\r\n",
		"X-Request-Id: req-42\r\n",
		"\r\n\r\n"+`{"name":"Jane","role":"chair"}`,
	)
}

func TestWireDump_MasksSetCookieOnResponse(t *testing.T) {
	dump := "HTTP/1.1 200 OK\r\n" +
		"Set-Cookie: sid=secret-session; Path=/; HttpOnly\r\n" +
		"Content-Type: application/json\r\n" +
		"\r\n" +
		`{"ok":true}`

	got := WireDump([]byte(dump))

	if strings.Contains(got, "secret-session") {
		t.Errorf("Set-Cookie must be masked, got:\n%s", got)
	}
	assertContains(t, got, "HTTP/1.1 200 OK\r\n", "Set-Cookie: "+Mask+"\r\n", `{"ok":true}`)
}

func TestWireDump_BodyTextIsNotTreatedAsHeaders(t *testing.T) {
	body := "note: keep\r\nauthorization: this is body text, not a header"
	got := WireDump([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n" + body))
	if !strings.HasSuffix(got, "\r\n\r\n"+body) {
		t.Errorf("the body must pass through untouched when it holds no signed link, got:\n%s", got)
	}
}

func TestWireDump_MasksSignedLinkInJSONBody(t *testing.T) {
	// Go's encoding/json escapes "&" as \u0026, so a pre-signed link in a
	// JSON response from a Go service reaches the dump in this form.
	escaped := strings.ReplaceAll(signedLink, "&", `\u0026`)
	dump := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n" +
		`{"uid":"att-7","download_url":"` + escaped + `"}` + "\n" +
		`{"uid":"att-8","file_url":"` + signedLink + `","page_token":"next-page-abc"}`

	got := WireDump([]byte(dump))

	assertNoSecrets(t, got)
	assertContains(t, got,
		`"uid":"att-7"`,
		`"uid":"att-8"`,
		"https://files.example.test/meetings/m-1/agenda.pdf?X-Amz-Algorithm=AWS4-HMAC-SHA256",
		`\u0026X-Amz-Signature=`+Mask+`"`,
		`\u0026X-Amz-Credential=`+Mask+`\u0026`,
		`\u0026X-Amz-Date=20260101T000000Z`,
		"&X-Amz-Security-Token="+Mask+"&",
		"&X-Amz-Expires=3600",
		"&X-Amz-SignedHeaders=host",
		`"page_token":"next-page-abc"`,
	)
}

func TestWireDump_MasksSignedLinkInRequestLineAndHeaders(t *testing.T) {
	dump := "GET /files/agenda.pdf?X-Amz-Signature=" + testSignature + "&v=1 HTTP/1.1\r\n" +
		"Host: files.example.test\r\n" +
		"\r\n"
	got := WireDump([]byte(dump))
	assertNoSecrets(t, got)
	assertContains(t, got, "GET /files/agenda.pdf?X-Amz-Signature="+Mask+"&v=1 HTTP/1.1\r\n")

	resp := "HTTP/1.1 302 Found\r\nLocation: " + signedLink + "\r\n\r\n"
	got = WireDump([]byte(resp))
	assertNoSecrets(t, got)
	assertContains(t, got, "Location: https://files.example.test/meetings/m-1/agenda.pdf?", "X-Amz-Date=20260101T000000Z")
}

func TestURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "pre-signed link",
			in:   "https://files.example.test/a.pdf?X-Amz-Expires=3600&X-Amz-Signature=" + testSignature,
			want: "https://files.example.test/a.pdf?X-Amz-Expires=3600&X-Amz-Signature=" + Mask,
		},
		{
			name: "parameter names match case-insensitively",
			in:   "https://files.example.test/a.pdf?x-amz-signature=abc&X-AMZ-CREDENTIAL=def",
			want: "https://files.example.test/a.pdf?x-amz-signature=" + Mask + "&X-AMZ-CREDENTIAL=" + Mask,
		},
		{
			name: "generic signed-link parameters",
			in:   "https://cdn.example.test/r.mp4?Expires=1&Signature=s1&Key-Pair-Id=k&sig=s2&token=t&access_token=a",
			want: "https://cdn.example.test/r.mp4?Expires=1&Signature=" + Mask + "&Key-Pair-Id=k&sig=" + Mask + "&token=" + Mask + "&access_token=" + Mask,
		},
		{
			name: "join link passcode",
			in:   "https://meet.example.test/j/1234567890?pwd=test-join-passcode&from=addon",
			want: "https://meet.example.test/j/1234567890?pwd=" + Mask + "&from=addon",
		},
		{
			name: "LFX join link password",
			in:   "https://meet.example.test/meeting/93699735000?password=test-join-page-pw",
			want: "https://meet.example.test/meeting/93699735000?password=" + Mask,
		},
		{
			name: "names that only end in password are kept",
			in:   "https://api.example.test/x?reset_password=r1&password_hint=h",
			want: "https://api.example.test/x?reset_password=r1&password_hint=h",
		},
		{
			name: "HTML-escaped separator",
			in:   "https://files.example.test/a.pdf?v=1&amp;X-Amz-Signature=abc",
			want: "https://files.example.test/a.pdf?v=1&amp;X-Amz-Signature=" + Mask,
		},
		{
			name: "twice HTML-escaped separator",
			in:   "https://files.example.test/a.pdf?v=1&amp;amp;token=secret-value",
			want: "https://files.example.test/a.pdf?v=1&amp;amp;token=" + Mask,
		},
		{
			name: "JSON-escaped HTML separator",
			in:   `https://files.example.test/a.pdf?v=1\u0026amp;token=secret-value`,
			want: `https://files.example.test/a.pdf?v=1\u0026amp;token=` + Mask,
		},
		{
			name: "HTML numeric-entity separators",
			in:   "https://files.example.test/a.pdf?v=1&#38;sig=s1&#x26;Signature=s2",
			want: "https://files.example.test/a.pdf?v=1&#38;sig=" + Mask + "&#x26;Signature=" + Mask,
		},
		{
			name: "JSON-escaped slash inside a value",
			in:   `https://files.example.test/x?token=header.payload\/signature&v=1`,
			want: `https://files.example.test/x?token=` + Mask + `&v=1`,
		},
		{
			name: "unicode-escaped slash inside a value",
			in:   `https://files.example.test/x?X-Amz-Credential=AKIDEXAMPLE\u002F20260924\u002fus-east-1\u002Fs3\u002Faws4_request&v=1`,
			want: `https://files.example.test/x?X-Amz-Credential=` + Mask + `&v=1`,
		},
		{
			name: "escaped quote still ends a value",
			in:   `https://files.example.test/x?sig=abc\"kept`,
			want: `https://files.example.test/x?sig=` + Mask + `\"kept`,
		},
		{
			name: "fragment after a masked value is kept",
			in:   "https://files.example.test/a.pdf?sig=abc#page=2",
			want: "https://files.example.test/a.pdf?sig=" + Mask + "#page=2",
		},
		{
			name: "names that only end in a secret name are kept",
			in:   "https://api.example.test/query/resources?page_token=p1&type=meeting&next_sig=n",
			want: "https://api.example.test/query/resources?page_token=p1&type=meeting&next_sig=n",
		},
		{
			name: "URL without a query is unchanged",
			in:   "https://api.example.test/meetings/m-1",
			want: "https://api.example.test/meetings/m-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := URL(tt.in); got != tt.want {
				t.Errorf("URL(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWireDump_DoesNotModifyInput(t *testing.T) {
	dump := []byte("GET /a?sig=abc HTTP/1.1\r\nAuthorization: Bearer " + testBearer + "\r\n\r\n")
	orig := string(dump)
	_ = WireDump(dump)
	if string(dump) != orig {
		t.Errorf("the dump passed in must not be modified: got %q want %q", dump, orig)
	}
}

func TestWireDump_MasksMeetingPasscodeFields(t *testing.T) {
	// A meeting record shaped like the query service returns it.
	body := `{"resources":[{"type":"meeting","data":{"uid":"m-1","title":"Weekly sync",` +
		`"join_url":"https://meet.example.test/j/1234567890?pwd=test-join-passcode",` +
		`"passcode":"test-passcode","host_key":"test-host-key","recording_password":"test-rec-pw",` +
		`"password":"test-join-page-pw","meeting_password": "test-past-pw","other_host_key":9,` +
		`"host_key_hint":"kept","note":"escaped \"quote\" kept"}}],"page_token":"p2"}` + "\n" +
		`{"uid":"m-2","host_key":654321}` + "\n" +
		`{"uid":"m-3","password":987.65,"passcode":7e9,"meeting_password":-3.5E+4,"after":"kept"}` + "\n" +
		`{"uid":"m-4","join_url":"https://meet.example.test/meeting/93699735000?password=test-lfx-join-pw","password":"test-lfx-join-pw"}`
	dump := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n" + body

	got := WireDump([]byte(dump))

	for _, secret := range []string{
		"test-join-passcode", "test-passcode", "test-host-key", "test-rec-pw",
		"test-join-page-pw", "test-past-pw", "654321", "987", ".65", "7e9", "3.5E", "E+4",
		"test-lfx-join-pw",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q must be masked, got:\n%s", secret, got)
		}
	}
	assertContains(t, got,
		`"uid":"m-1"`, `"title":"Weekly sync"`,
		`"join_url":"https://meet.example.test/j/1234567890?pwd=`+Mask+`"`,
		`"passcode":"`+Mask+`"`,
		`"host_key":"`+Mask+`"`,
		`"recording_password":"`+Mask+`"`,
		`"password":"`+Mask+`"`,
		`"meeting_password": "`+Mask+`"`,
		`"other_host_key":9`,
		`"host_key_hint":"kept"`,
		`"note":"escaped \"quote\" kept"`,
		`"page_token":"p2"`,
		`{"uid":"m-2","host_key":"`+Mask+`"}`,
		`{"uid":"m-3","password":"`+Mask+`","passcode":"`+Mask+`","meeting_password":"`+Mask+`","after":"kept"}`,
		`{"uid":"m-4","join_url":"https://meet.example.test/meeting/93699735000?password=`+Mask+`","password":"`+Mask+`"}`,
	)
}

func TestError(t *testing.T) {
	if got := Error(nil); got != "" {
		t.Errorf("Error(nil) = %q, want empty", got)
	}
	err := &url.Error{
		Op:  "Get",
		URL: "https://files.example.test/a.pdf?X-Amz-Signature=abc&X-Amz-Date=20260101T000000Z",
		Err: errors.New("dial tcp: connection refused"),
	}
	want := `Get "https://files.example.test/a.pdf?X-Amz-Signature=` + Mask + `&X-Amz-Date=20260101T000000Z": dial tcp: connection refused`
	if got := Error(err); got != want {
		t.Errorf("Error()\n got %q\nwant %q", got, want)
	}
}
