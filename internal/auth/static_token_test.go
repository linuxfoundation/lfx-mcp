// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package auth

import (
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwt"
)

// buildInsecureToken builds an unsigned JWT (no key required) for testing
// PeekExpiry/PeekAudience, which parse tokens without verifying a signature.
func buildInsecureToken(t *testing.T, build func(*jwt.Builder) *jwt.Builder) string {
	t.Helper()
	builder := build(jwt.NewBuilder())
	token, err := builder.Build()
	if err != nil {
		t.Fatalf("building token: %v", err)
	}
	signed, err := jwt.Sign(token, jwt.WithInsecureNoSignature())
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return string(signed)
}

func TestPeekExpiry(t *testing.T) {
	future := time.Now().Add(time.Hour).Truncate(time.Second)
	past := time.Now().Add(-time.Hour).Truncate(time.Second)

	tests := []struct {
		name    string
		token   string
		want    time.Time
		wantErr bool
	}{
		{
			name:  "valid future expiry",
			token: buildInsecureToken(t, func(b *jwt.Builder) *jwt.Builder { return b.Expiration(future) }),
			want:  future,
		},
		{
			name:  "already-expired token",
			token: buildInsecureToken(t, func(b *jwt.Builder) *jwt.Builder { return b.Expiration(past) }),
			want:  past,
		},
		{
			name:    "missing exp claim",
			token:   buildInsecureToken(t, func(b *jwt.Builder) *jwt.Builder { return b.Issuer("test") }),
			wantErr: true,
		},
		{
			name:    "malformed token",
			token:   "not-a-jwt",
			wantErr: true,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PeekExpiry(tt.token)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got exp=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got exp %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPeekAudience(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		want    []string
		wantErr bool
	}{
		{
			name:  "single audience",
			token: buildInsecureToken(t, func(b *jwt.Builder) *jwt.Builder { return b.Audience([]string{"https://lfx-api.v2.cluster.lfx.dev"}) }),
			want:  []string{"https://lfx-api.v2.cluster.lfx.dev"},
		},
		{
			name:  "multiple audiences",
			token: buildInsecureToken(t, func(b *jwt.Builder) *jwt.Builder { return b.Audience([]string{"aud-one", "aud-two"}) }),
			want:  []string{"aud-one", "aud-two"},
		},
		{
			name:  "missing aud claim returns empty, not an error",
			token: buildInsecureToken(t, func(b *jwt.Builder) *jwt.Builder { return b.Issuer("test") }),
			want:  nil,
		},
		{
			name:    "malformed token",
			token:   "not-a-jwt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PeekAudience(tt.token)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got aud=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got aud %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got aud %v, want %v", got, tt.want)
				}
			}
		})
	}
}
