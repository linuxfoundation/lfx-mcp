// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client configuration for LFX v2 API services; this
// file holds the committee client's request encoder.
package lfxv2

import (
	"encoding/json"
	"net/http"

	committeehttpclient "github.com/linuxfoundation/lfx-v2-committee-service/gen/http/committee_service/client"
	goahttp "goa.design/goa/v3/http"
)

// committeeRequestEncoder is goahttp.RequestEncoder with one adjustment: an
// update-committee-member request body whose e-mail is empty is sent without
// the "email" key. Every other request body is encoded exactly as goa does.
//
// The pinned committee client generates that body's e-mail as a plain string
// without omitempty, because the service required the field when the client
// was generated. The committee service is changing the contract
// (linuxfoundation/lfx-v2-committee-service#226): the member GET no longer
// returns the e-mail, an e-mail present on the update body must be well
// formed, and an omitted e-mail is restored server-side from the stored
// record. update_committee_member is a fetch-merge-put that copies the GET's
// e-mail into the PUT, so against that service the pinned client would send
// `"email":""` and the update would be rejected before the restore runs.
//
// Dropping the key only when the e-mail is empty keeps both contracts
// working: today's service still receives the e-mail its GET returned, and
// the new service receives no key and keeps the stored address. The hook
// sees only the merged string, so an empty e-mail a caller supplies on
// purpose is dropped too; neither service ever accepted that body, and the
// new one then keeps the stored address instead of answering 400.
//
// Delete this hook when the pinned client is bumped to a release that
// contains #226: that client generates the field as *string with omitempty
// and the handler can pass the GET's pointer straight through.
func committeeRequestEncoder(r *http.Request) goahttp.Encoder {
	return &updateMemberBodyEncoder{Encoder: goahttp.RequestEncoder(r)}
}

// updateMemberBodyEncoder wraps a goa encoder and rewrites one body shape.
type updateMemberBodyEncoder struct {
	goahttp.Encoder
}

// Encode drops the "email" key from an update-committee-member body whose
// e-mail is empty and delegates everything else unchanged. The generated
// encoder passes the body as a pointer to its pointer.
func (e *updateMemberBodyEncoder) Encode(v any) error {
	body, ok := v.(**committeehttpclient.UpdateCommitteeMemberRequestBody)
	if !ok || body == nil || *body == nil || (*body).Email != "" {
		return e.Encoder.Encode(v)
	}
	raw, err := json.Marshal(*body)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	delete(fields, "email")
	return e.Encoder.Encode(fields)
}
