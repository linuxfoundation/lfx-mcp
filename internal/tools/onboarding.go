// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"github.com/linuxfoundation/lfx-mcp/internal/serviceapi"
)

// OnboardingConfig holds configuration shared by member onboarding tools.
type OnboardingConfig struct {
	ServiceAuth
	ServiceClient *serviceapi.Client
}

var onboardingConfig *OnboardingConfig

// SetOnboardingConfig sets the configuration for onboarding tools.
func SetOnboardingConfig(cfg *OnboardingConfig) {
	onboardingConfig = cfg
}

// TODO: RegisterListMembershipActions — add back when guided onboarding flow is ready.
// See member-onboarding service for the /memberships endpoint.
