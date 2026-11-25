// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client utilities for interacting with LFX v2 APIs, including OAuth2 token exchange.
package lfxv2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	meetingservice "github.com/linuxfoundation/lfx-v2-meeting-service/gen/meeting_service"
)

// TestMeetingClient_StrictAwareOverCleaning pins the meeting client's decoder
// chain as NewClients builds it: a call marked with WithStrictDecoding
// refuses a field the client does not know, on a body the cleaning has
// already been applied to, and an unmarked call decodes the same body.
func TestMeetingClient_StrictAwareOverCleaning(t *testing.T) {
	// public_link "" fails the client's URI check unless the record is
	// cleaned first, so a strict success below proves cleaning ran.
	const known = `{"id":"91461158520","project_uid":"proj-1","title":"T","visibility":"private","public_link":""}`
	const unknown = `{"id":"91461158520","project_uid":"proj-1","title":"T","visibility":"private","public_link":"","brand_new":1}`

	body := known
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	clients, err := NewClients(context.Background(), ClientConfig{
		APIDomain:      server.URL,
		StaticLFXToken: "static-token-value",
	})
	if err != nil {
		t.Fatalf("NewClients: %v", err)
	}
	get := func(ctx context.Context) error {
		_, err := clients.Meeting.GetItxMeeting(ctx, &meetingservice.GetItxMeetingPayload{MeetingID: "91461158520"})
		return err
	}

	if err := get(WithStrictDecoding(context.Background())); err != nil {
		t.Fatalf("a strict read of a known, cleaned record must decode: %v", err)
	}

	body = unknown
	err = get(WithStrictDecoding(context.Background()))
	if err == nil || !strings.Contains(err.Error(), "does not know about") {
		t.Fatalf("a strict read must refuse the unknown field, got: %v", err)
	}
	if err := get(context.Background()); err != nil {
		t.Fatalf("a read not marked strict must tolerate the unknown field: %v", err)
	}

	// The cleaning removes an empty string and a recurrence that is not one;
	// a strict read must see an unknown name inside either before that.
	for name, removed := range map[string]string{
		"empty string":       `{"id":"91461158520","project_uid":"proj-1","title":"T","visibility":"private","brand_new":""}`,
		"in an occurrence":   `{"id":"91461158520","project_uid":"proj-1","title":"T","visibility":"private","occurrences":[{"occurrence_id":"1","brand_new":""}]}`,
		"in a recurrence":    `{"id":"91461158520","project_uid":"proj-1","title":"T","visibility":"private","recurrence":{"type":0,"brand_new":1}}`,
		"after a bad value":  `{"id":"91461158520","project_uid":"proj-1","title":"T","visibility":"private","duration":"sixty","brand_new":1}`,
		"null unknown field": `{"id":"91461158520","project_uid":"proj-1","title":"T","visibility":"private","brand_new":null}`,
	} {
		body = removed
		err := get(WithStrictDecoding(context.Background()))
		if err == nil || !strings.Contains(err.Error(), "does not know about") {
			t.Errorf("%s: a strict read must refuse the unknown field, got: %v", name, err)
		}
	}
}
