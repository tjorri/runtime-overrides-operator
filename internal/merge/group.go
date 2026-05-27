// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package merge

import (
	"slices"
	"strings"
)

// GroupByTenant buckets a flat slice of Override values by their TenantId.
// The returned map's iteration order is not defined; callers that need
// deterministic ordering of tenants should sort the keys themselves.
func GroupByTenant(crs []Override) map[string][]Override {
	out := make(map[string][]Override)
	for _, cr := range crs {
		out[cr.TenantId] = append(out[cr.TenantId], cr)
	}
	return out
}

// SortGroup sorts a slice of Override values in place by
// (Weight ASC, Namespace ASC, Name ASC). The sort is stable so identical
// keys preserve input order.
//
// Sort order matches the deep-merge semantics (later wins on leaf collisions):
// the highest-weight CR ends up last in the slice and therefore overrides
// lower-weight contributions. Same-weight collisions tie-break lexically
// by (Namespace, Name) — deterministic but arbitrary; the conflict tracking
// in TrackingMerge surfaces these so they're visible to operators.
func SortGroup(group []Override) {
	slices.SortStableFunc(group, func(a, b Override) int {
		if a.Weight != b.Weight {
			if a.Weight < b.Weight {
				return -1
			}
			return 1
		}
		if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
}
