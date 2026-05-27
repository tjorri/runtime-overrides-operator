// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// outputCMPredicate returns a predicate that fires only on changes to
// the specified output ConfigMap (name + namespace match). Used to scope
// the drift watch narrowly — not all ConfigMaps cluster-wide.
func outputCMPredicate(cmName types.NamespacedName) predicate.Predicate {
	matches := func(obj client.Object) bool {
		return obj != nil &&
			obj.GetNamespace() == cmName.Namespace &&
			obj.GetName() == cmName.Name
	}
	return predicate.NewPredicateFuncs(matches)
}

// configMapHandler maps any output-CM event to the per-target singleton
// reconcile key.
func configMapHandler(reconcileKey types.NamespacedName) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(_ context.Context, _ client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: reconcileKey}}
	})
}

// unknownFieldManager is the bucket label used when no non-operator
// writer can be identified on the drifted ConfigMap — keeps the
// `conflicting_field_manager` metric label cardinality bounded.
const unknownFieldManager = "unknown"

// extractConflictingFieldManager pulls a short, label-safe identifier for
// the most recent non-operator writer to a ConfigMap, used as the
// `conflicting_field_manager` metric label and in the ConfigMapDrifted
// event. Long manager names are truncated; unknown is bucketed.
//
// SSA records writers in metadata.managedFields as {Manager, Operation,
// Time, ...}. The slice ordering is API-server-defined (not time-sorted),
// so we walk all entries and pick the one with the largest Time among the
// non-operator writers. Entries with a zero Time act as "unknown freshness"
// and are only used as a fallback when nothing has a Time. The cap is 64
// chars to keep label cardinality bounded under a misbehaving writer.
func extractConflictingFieldManager(cm *corev1.ConfigMap, ourManager string) string {
	const maxLen = 64
	if cm == nil {
		return unknownFieldManager
	}
	var (
		bestName     string
		bestHasTime  bool
		bestTime     int64
		fallbackName string
	)
	for _, mf := range cm.ManagedFields {
		if mf.Manager == "" || mf.Manager == ourManager {
			continue
		}
		m := strings.TrimSpace(mf.Manager)
		if m == "" {
			continue
		}
		if len(m) > maxLen {
			m = m[:maxLen]
		}
		if mf.Time == nil || mf.Time.IsZero() {
			if fallbackName == "" {
				fallbackName = m
			}
			continue
		}
		t := mf.Time.UnixNano()
		if !bestHasTime || t > bestTime {
			bestHasTime = true
			bestTime = t
			bestName = m
		}
	}
	if bestHasTime {
		return bestName
	}
	if fallbackName != "" {
		return fallbackName
	}
	return unknownFieldManager
}
