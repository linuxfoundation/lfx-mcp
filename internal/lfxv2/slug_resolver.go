// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client utilities for interacting with LFX v2 APIs, including OAuth2 token exchange.
package lfxv2

import (
	"context"
	"fmt"
	"strings"
	"sync"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
)

// SlugResolver resolves project slugs to V2 UUIDs with in-memory caching.
//
// Slug-to-UUID mappings are stable, so the cache has no TTL. The resolver
// accepts a [Clients] instance per call because each call may use a different
// user's auth context for the V2 query service, but the cached mappings are
// user-independent.
type SlugResolver struct {
	mu    sync.RWMutex
	cache map[string]string // slug -> UUID
}

// projectResourceType is the resource type for project queries.
const slugResolverResourceType = "project"

// NewSlugResolver creates a new resolver with an empty cache.
func NewSlugResolver() *SlugResolver {
	return &SlugResolver{
		cache: make(map[string]string),
	}
}

// Resolve looks up the V2 UUID for a project slug. It checks the in-memory
// cache first and falls back to the V2 query service on a cache miss.
func (r *SlugResolver) Resolve(ctx context.Context, clients *Clients, slug string) (string, error) {
	// Check cache (read lock).
	r.mu.RLock()
	uuid, ok := r.cache[slug]
	r.mu.RUnlock()

	if ok {
		return uuid, nil
	}

	// Cache miss — query the V2 service.
	uuid, err := r.queryProjectUUID(ctx, clients, slug)
	if err != nil {
		return "", err
	}

	// Populate cache (write lock).
	r.mu.Lock()
	r.cache[slug] = uuid
	r.mu.Unlock()

	return uuid, nil
}

// queryProjectUUID queries the V2 query service for a project by slug.
// It uses the Filters field with "slug:<value>" which performs a term match
// on the data.slug field of indexed project resources.
func (r *SlugResolver) queryProjectUUID(ctx context.Context, clients *Clients, slug string) (string, error) {
	resourceType := slugResolverResourceType

	result, err := clients.QuerySvc.QueryResources(ctx, &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		Filters:  []string{"slug:" + slug},
		PageSize: 1,
	})
	if err != nil {
		return "", fmt.Errorf("failed to query project by slug %q: %w", slug, err)
	}

	if result == nil || len(result.Resources) == 0 {
		return "", fmt.Errorf("project not found for slug %q", slug)
	}

	id := result.Resources[0].ID
	if id == nil || *id == "" {
		return "", fmt.Errorf("project found for slug %q but has no ID", slug)
	}

	// The query service returns IDs with a type prefix (e.g., "project:uuid").
	// Strip the prefix to get the bare UUID needed for access-check.
	uuid := strings.TrimPrefix(*id, "project:")

	return uuid, nil
}
