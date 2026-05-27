// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// Package render turns a per-tenant merged-overrides map into the canonical
// YAML body the operator writes into its output ConfigMap.
package render

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/tjorri/runtime-overrides-operator/internal/merge"
)

// DataKey is the key under ConfigMap.data that holds the rendered runtime
// overrides YAML. Centralized so the controller, the apply helper, and the
// Loki/Mimir installer's volume-mount path all agree on the same name.
const DataKey = "runtime-tenants.yaml"

// Output is the rendered shape: a tenant ID → merged overrides map.
type Output map[string]map[string]any

// Render produces a deterministic YAML body wrapping tenants in the
// top-level `overrides:` map expected by dskit's runtime-config loader.
// Tenant IDs are sorted lexically for idempotency; leaf keys are sorted
// by gopkg.in/yaml.v3's default behavior (see internal/merge.SortedMarshal).
//
// An empty Output produces "overrides: {}\n" — used by the startup bootstrap
// path so the file the Loki/Mimir installer mounts always
// exists.
func Render(tenants Output) ([]byte, error) {
	// Build the wrapper with tenant IDs in sorted order. yaml.v3 sorts map
	// keys alphabetically on marshal, but constructing in sorted order
	// makes the intent explicit and lets us assert on it.
	keys := make([]string, 0, len(tenants))
	for k := range tenants {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	inner := make(map[string]any, len(tenants))
	for _, k := range keys {
		inner[k] = tenants[k]
	}

	doc := map[string]any{"overrides": inner}
	return merge.SortedMarshal(doc)
}

// Hash returns the hex-encoded SHA-256 of body. Used as the
// hash-compare-before-apply skip check and as the fingerprint
// surfaced in the Applied condition's message.
func Hash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
