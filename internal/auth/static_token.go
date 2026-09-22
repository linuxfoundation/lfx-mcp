// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package auth

import (
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwt"
)

// PeekAudience extracts the aud claim from tokenString without verifying its
// signature or any other claim, for the same reasons as PeekExpiry. It is
// used only to warn about an obviously mismatched static LFX token (e.g. one
// minted for a different API audience), never to make any authorization
// decision.
func PeekAudience(tokenString string) ([]string, error) {
	token, err := jwt.ParseInsecure([]byte(tokenString))
	if err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}

	return token.Audience(), nil
}

// PeekExpiry extracts the exp claim from tokenString without verifying its
// signature or any other claim. This is safe because the result is never
// used to make an authorization decision — the LFX API independently
// verifies and authorizes every call made with the token — it is only used
// as a convenience to detect an already-expired token up front and to bound
// how long the token may be used for, instead of surfacing a confusing 401
// on the first tool call.
func PeekExpiry(tokenString string) (time.Time, error) {
	token, err := jwt.ParseInsecure([]byte(tokenString))
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing token: %w", err)
	}

	exp := token.Expiration()
	if exp.IsZero() {
		return time.Time{}, fmt.Errorf("token has no exp claim")
	}

	return exp, nil
}
