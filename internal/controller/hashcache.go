// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import "sync"

// HashCache is a concurrent-safe per-target last-applied hash, used by the
// drift watch to distinguish "we wrote this" from "someone
// else clobbered it." Each target reconciler has its own HashCache; the
// cache survives an operator restart by reading the live CM's content hash
// on first reconcile.
type HashCache struct {
	mu     sync.RWMutex
	hashes map[string]string // targetLabel -> hex sha256 of last applied body
}

// NewHashCache returns an empty cache.
func NewHashCache() *HashCache {
	return &HashCache{hashes: map[string]string{}}
}

// Set records the hash applied for a target.
func (h *HashCache) Set(target, hash string) {
	h.mu.Lock()
	h.hashes[target] = hash
	h.mu.Unlock()
}

// Get returns the last-known hash for a target. Returns ("", false) if the
// cache has nothing recorded yet (e.g. first reconcile after restart).
func (h *HashCache) Get(target string) (string, bool) {
	h.mu.RLock()
	v, ok := h.hashes[target]
	h.mu.RUnlock()
	return v, ok
}
