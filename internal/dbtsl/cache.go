// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package dbtsl provides a client for the dbt Semantic Layer API.
package dbtsl

import (
	"sync"
	"time"
)

// ttlCache is an in-memory TTL cache with a bounded entry count.
//
// An expired entry is dropped when accessed, expired entries are swept on
// put, and the entry closest to expiry is evicted when the cache is full and
// a new key arrives. Unlike the Python original, which relied on the GIL and a
// single event loop, this is guarded by a mutex: the MCP server handles
// requests concurrently across goroutines.
type ttlCache[K comparable, V any] struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	store      map[K]cacheEntry[V]
}

type cacheEntry[V any] struct {
	expiry time.Time
	value  V
}

func newTTLCache[K comparable, V any](ttl time.Duration, maxEntries int) *ttlCache[K, V] {
	return &ttlCache[K, V]{
		ttl:        ttl,
		maxEntries: maxEntries,
		store:      make(map[K]cacheEntry[V]),
	}
}

// get returns the cached value for key, and whether it was present and unexpired.
func (c *ttlCache[K, V]) get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.store[key]
	if !ok {
		var zero V
		return zero, false
	}
	if time.Now().After(entry.expiry) {
		delete(c.store, key)
		var zero V
		return zero, false
	}
	return entry.value, true
}

// put stores value under key, sweeping expired entries first.
func (c *ttlCache[K, V]) put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for k, entry := range c.store {
		if !entry.expiry.After(now) {
			delete(c.store, k)
		}
	}

	// Evict only when inserting a new key into a full cache.
	if _, exists := c.store[key]; !exists && len(c.store) >= c.maxEntries {
		var oldestKey K
		var oldestExpiry time.Time
		first := true
		for k, entry := range c.store {
			if first || entry.expiry.Before(oldestExpiry) {
				oldestKey, oldestExpiry, first = k, entry.expiry, false
			}
		}
		if !first {
			delete(c.store, oldestKey)
		}
	}

	c.store[key] = cacheEntry[V]{expiry: now.Add(c.ttl), value: value}
}

// clear drops every entry.
func (c *ttlCache[K, V]) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[K]cacheEntry[V])
}
