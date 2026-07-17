// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"
)

func TestSplitTrimmed(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "empty string is unset",
			input: "",
			want:  []string{},
		},
		{
			name:  "single value",
			input: "hello_world",
			want:  []string{"hello_world"},
		},
		{
			name:  "multiple values",
			input: "a,b,c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "values with surrounding whitespace",
			input: "a, b ,  c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:    "lone comma is malformed",
			input:   ",",
			wantErr: true,
		},
		{
			name:    "leading comma is malformed",
			input:   ",a,b",
			wantErr: true,
		},
		{
			name:    "trailing comma is malformed",
			input:   "a,b,",
			wantErr: true,
		},
		{
			name:    "embedded empty entry is malformed",
			input:   "a,,b",
			wantErr: true,
		},
		{
			name:    "whitespace-only entry is malformed",
			input:   "a, ,b",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitTrimmed(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitTrimmed(%q) = %#v, nil, want an error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitTrimmed(%q) returned unexpected error: %v", tt.input, err)
			}
			if got == nil {
				t.Fatal("splitTrimmed returned nil, want non-nil slice")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitTrimmed(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}
