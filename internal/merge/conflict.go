// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package merge

import "strings"

// FieldConflict describes a single leaf in the merged output where a CR's
// contribution was overridden by a later CR in sort order.
//
// Path is the dotted path to the conflicting leaf inside the tenant's
// overrides map (e.g. ["ingestion_rate_mb"] or ["nested", "key"]).
//
// SameWeight is true when at least one Loser had the same Weight as the
// Winner — these are the "authoring smell" collisions resolved by lexical
// (Namespace, Name) tie-break, surfaced via the
// runtime_overrides_field_conflicts_total metric.
// Different-weight overrides are not metric-counted but are still recorded
// here so the controller can emit FieldOverridden events on the losers.
type FieldConflict struct {
	Tenant     string
	Path       []string
	Winner     CRRef
	Losers     []CRRef
	SameWeight bool
}

// TrackingMerge deep-merges a tenant's sorted CRs and records per-leaf
// conflicts. Input must be pre-sorted by SortGroup; behavior is undefined
// otherwise.
//
// Conflicts are pair-aggregated per leaf path: each FieldConflict represents
// one path with one Winner (the last CR to write that leaf) and the list of
// prior CRs that wrote the same leaf with a different value.
//
// A leaf written by multiple CRs with the *same* value is not a conflict —
// they agreed, no surface needed.
func TrackingMerge(tenant string, sorted []Override) (map[string]any, []FieldConflict) {
	merged := map[string]any{}
	// pathKey -> writer info (last-writer-wins as we go)
	type writerInfo struct {
		ref   CRRef
		value any
	}
	provenance := map[string]writerInfo{}
	// pathKey -> conflict struct under construction
	conflicts := map[string]*FieldConflict{}
	pathKeyOrder := []string{}

	for _, cr := range sorted {
		ref := refOf(cr)
		walkLeaves(cr.Overrides, nil, func(path []string, value any) {
			key := strings.Join(path, "\x00") // NUL separator avoids collision with field names containing dots
			if prior, ok := provenance[key]; ok {
				if !leafValuesEqual(prior.value, value) {
					// Real conflict: leaf value differs.
					fc, exists := conflicts[key]
					if !exists {
						fc = &FieldConflict{
							Tenant: tenant,
							Path:   append([]string(nil), path...),
						}
						conflicts[key] = fc
						pathKeyOrder = append(pathKeyOrder, key)
					}
					fc.Losers = append(fc.Losers, prior.ref)
					fc.Winner = ref
					if prior.ref.Weight == ref.Weight {
						fc.SameWeight = true
					}
				}
			}
			provenance[key] = writerInfo{ref: ref, value: value}
		})
		merged = DeepMerge(merged, cr.Overrides)
	}

	// Materialize conflicts in insertion order for deterministic output.
	out := make([]FieldConflict, 0, len(conflicts))
	for _, k := range pathKeyOrder {
		out = append(out, *conflicts[k])
	}
	return merged, out
}

// walkLeaves visits every leaf of a parsed-JSON-style map. A leaf is any
// non-map value (slices are treated as leaves — we don't conflict-track
// inside arrays; they overwrite atomically). The callback receives a
// freshly-allocated path slice for safety.
func walkLeaves(v map[string]any, prefix []string, fn func(path []string, value any)) {
	for k, vv := range v {
		path := append(append([]string(nil), prefix...), k)
		if m, ok := vv.(map[string]any); ok {
			walkLeaves(m, path, fn)
			continue
		}
		fn(path, vv)
	}
}

// leafValuesEqual compares two leaf values for "same value" purposes. For
// scalars (string/bool/int/float) this is simple equality; for slices we
// compare element-wise using the same predicate recursively. We never see
// maps here because walkLeaves recurses into them.
func leafValuesEqual(a, b any) bool {
	switch ax := a.(type) {
	case []any:
		bx, ok := b.([]any)
		if !ok || len(ax) != len(bx) {
			return false
		}
		for i := range ax {
			if !leafValuesEqual(ax[i], bx[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
