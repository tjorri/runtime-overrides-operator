// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// upsertCondition returns the conditions slice with the given condition
// merged in. If a condition with the same Type exists and the new condition
// would only change LastTransitionTime / ObservedGeneration but neither
// Status, Reason, nor Message changed, the existing condition is preserved
// verbatim so callers can compare for a meaningful change.
//
// This is the load-bearing transition-gate helper:
// only when a condition actually transitions do we update status, so
// CRD watchers downstream don't flap and the API server isn't write-spammed.
func upsertCondition(existing []metav1.Condition, desired metav1.Condition) []metav1.Condition {
	// Cap at len+1 so the append-fresh path at the bottom doesn't reallocate.
	out := make([]metav1.Condition, len(existing), len(existing)+1)
	copy(out, existing)

	for i := range out {
		if out[i].Type != desired.Type {
			continue
		}
		if conditionsEquivalent(out[i], desired) {
			// No meaningful change — keep the existing timestamp.
			out[i].ObservedGeneration = desired.ObservedGeneration
			return out
		}
		// Meaningful transition — replace.
		desired.LastTransitionTime = metav1.Now()
		out[i] = desired
		return out
	}
	// Not present — append fresh.
	desired.LastTransitionTime = metav1.Now()
	return append(out, desired)
}

// conditionsEquivalent returns true when two conditions of the same Type
// carry the same Status/Reason/Message. This is the "no meaningful change"
// predicate.
func conditionsEquivalent(a, b metav1.Condition) bool {
	return a.Type == b.Type &&
		a.Status == b.Status &&
		a.Reason == b.Reason &&
		a.Message == b.Message
}

// conditionsChanged returns true when applying `desired` over `existing` would
// produce a different slice (any condition added, removed, or transitioned).
func conditionsChanged(existing, desired []metav1.Condition) bool {
	if len(existing) != len(desired) {
		return true
	}
	// Compare by Type, since order isn't guaranteed.
	byType := make(map[string]metav1.Condition, len(existing))
	for _, c := range existing {
		byType[c.Type] = c
	}
	for _, d := range desired {
		e, ok := byType[d.Type]
		if !ok {
			return true
		}
		if !conditionsEquivalent(e, d) {
			return true
		}
	}
	return false
}
