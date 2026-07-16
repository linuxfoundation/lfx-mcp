// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"
)

func TestSplitTrimmed(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string",
			input: "",
			want:  []string{},
		},
		{
			name:  "whitespace only",
			input: "   ",
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
			name:  "leading and trailing commas",
			input: ",a,b,",
			want:  []string{"a", "b"},
		},
		{
			name:  "embedded empty entries",
			input: "a,,b",
			want:  []string{"a", "b"},
		},
		{
			name:  "all empty entries",
			input: ", , ,",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTrimmed(tt.input)
			if got == nil {
				t.Fatal("splitTrimmed returned nil, want non-nil slice")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitTrimmed(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}
