// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mf builds a ManagedFieldsEntry with the given manager and optional time.
// A nil tPtr leaves Time as nil (i.e. "unknown freshness"), exercising
// the fallback path. The Operation/Subresource fields are irrelevant here.
func mf(name string, tPtr *time.Time) metav1.ManagedFieldsEntry {
	e := metav1.ManagedFieldsEntry{Manager: name}
	if tPtr != nil {
		e.Time = &metav1.Time{Time: *tPtr}
	}
	return e
}

func TestExtractConflictingFieldManager(t *testing.T) {
	const ourMgr = "runtime-overrides-operator"

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(1 * time.Hour)
	t2 := t0.Add(2 * time.Hour)

	cases := []struct {
		name   string
		cm     *corev1.ConfigMap
		expect string
	}{
		{
			name:   "nil ConfigMap returns unknown",
			cm:     nil,
			expect: "unknown",
		},
		{
			name:   "empty managedFields returns unknown",
			cm:     &corev1.ConfigMap{},
			expect: "unknown",
		},
		{
			name: "only our writes returns unknown",
			cm: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{
				mf(ourMgr, &t0),
				mf(ourMgr, &t1),
			}}},
			expect: "unknown",
		},
		{
			name: "single non-operator writer returns it",
			cm: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{
				mf(ourMgr, &t0),
				mf("kubectl-edit", &t1),
			}}},
			expect: "kubectl-edit",
		},
		{
			name: "multiple non-operator writers with Times: latest wins",
			cm: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{
				// Out-of-order on purpose — slice order is not load-bearing.
				mf("argocd-controller", &t1),
				mf(ourMgr, &t0),
				mf("kubectl-edit", &t2),
				mf("flux-source-controller", &t0),
			}}},
			expect: "kubectl-edit",
		},
		{
			name: "non-operator writers without Times fall back to first non-empty",
			cm: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{
				mf(ourMgr, &t0),
				mf("argocd-controller", nil),
				mf("flux-source-controller", nil),
			}}},
			expect: "argocd-controller",
		},
		{
			name: "writers with Time always beat writers without Time",
			cm: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{
				mf("argocd-controller", nil),
				mf("kubectl-edit", &t1),
				mf("flux-source-controller", nil),
			}}},
			expect: "kubectl-edit",
		},
		{
			name: "whitespace-only manager names are skipped",
			cm: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{
				mf("   ", &t2),
				mf("kubectl-edit", &t1),
			}}},
			expect: "kubectl-edit",
		},
		{
			name: "manager names longer than maxLen are truncated",
			cm: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{
				mf(strings.Repeat("x", 200), &t1),
			}}},
			expect: strings.Repeat("x", 64),
		},
		{
			name: "zero Time is treated as unknown-freshness fallback",
			cm: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{
				// Zero metav1.Time. Should NOT beat the timed writer.
				{Manager: "argocd-controller", Time: &metav1.Time{}},
				mf("kubectl-edit", &t1),
			}}},
			expect: "kubectl-edit",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractConflictingFieldManager(tc.cm, ourMgr)
			if got != tc.expect {
				t.Fatalf("got %q, want %q", got, tc.expect)
			}
		})
	}
}
