// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import "testing"

func TestIsScopeBlindClient(t *testing.T) {
	tests := []struct {
		name     string
		clientID string
		want     bool
	}{
		{
			name:     "codex dev callback id",
			clientID: "https://chatgpt.com/oauth/codex/IrVFZga_egXz/client.json",
			want:     true,
		},
		{
			// The callback ID differs per MCP server URL, so only the prefix
			// may be matched.
			name:     "codex other callback id",
			clientID: "https://chatgpt.com/oauth/codex/SomeOtherId/client.json",
			want:     true,
		},
		{
			name:     "unrelated cimd client",
			clientID: "https://example.com/oauth/client.json",
			want:     false,
		},
		{
			// Must not match on the word alone, only on the full prefix.
			name:     "codex mentioned elsewhere in the value",
			clientID: "https://example.com/oauth/codex/client.json",
			want:     false,
		},
		{
			name:     "opaque client id",
			clientID: "tpc_9UuFX25HRGY2XRvio7VWtf",
			want:     false,
		},
		{
			name:     "empty",
			clientID: "",
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsScopeBlindClient(tc.clientID); got != tc.want {
				t.Errorf("IsScopeBlindClient(%q) = %v, want %v", tc.clientID, got, tc.want)
			}
		})
	}
}

func TestClientID(t *testing.T) {
	if got := ClientID(nil); got != "" {
		t.Errorf("ClientID(nil) = %q, want empty", got)
	}
}
