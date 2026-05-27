// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	v1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
)

// fakeCR is a minimal client.Object for applyDisabledStatus. We need
// GetGeneration; everything else can be a zero LokiTenantOverride.
func fakeCR(gen int64) *v1alpha1.LokiTenantOverride {
	cr := &v1alpha1.LokiTenantOverride{}
	cr.SetGeneration(gen)
	return cr
}

// drainEvents collects all events from a buffered FakeRecorder.
func drainEvents(rec *record.FakeRecorder) []string {
	var out []string
	for {
		select {
		case ev := <-rec.Events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestApplyDisabledStatus(t *testing.T) {
	t.Run("first call on a fresh CR reports changed and emits an event", func(t *testing.T) {
		rec := record.NewFakeRecorder(8)
		cr := fakeCR(1)
		var conds []metav1.Condition
		var observedGen int64

		changed := applyDisabledStatus(rec, cr, &conds, &observedGen, "loki")

		if !changed {
			t.Fatal("expected changed=true on first transition")
		}
		if observedGen != 1 {
			t.Fatalf("observedGen=%d, want 1", observedGen)
		}
		if len(conds) != 1 || conds[0].Type != v1alpha1.ConditionApplied {
			t.Fatalf("expected one Applied condition, got %#v", conds)
		}
		if conds[0].Status != metav1.ConditionFalse {
			t.Fatalf("Applied.Status=%q, want False", conds[0].Status)
		}
		if conds[0].Reason != v1alpha1.ReasonTargetDisabled {
			t.Fatalf("Applied.Reason=%q, want %q", conds[0].Reason, v1alpha1.ReasonTargetDisabled)
		}
		if !strings.Contains(conds[0].Message, `"loki"`) {
			t.Fatalf("Applied.Message=%q does not mention target", conds[0].Message)
		}

		events := drainEvents(rec)
		if len(events) != 1 {
			t.Fatalf("expected exactly one event, got %d: %v", len(events), events)
		}
		if !strings.Contains(events[0], "TargetDisabled") {
			t.Fatalf("event %q does not mention TargetDisabled", events[0])
		}
	})

	t.Run("re-applying with identical state reports unchanged and emits no event", func(t *testing.T) {
		rec := record.NewFakeRecorder(8)
		cr := fakeCR(1)

		// Pre-existing equivalent condition with a known timestamp.
		existingTime := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		conds := []metav1.Condition{{
			Type:               v1alpha1.ConditionApplied,
			Status:             metav1.ConditionFalse,
			Reason:             v1alpha1.ReasonTargetDisabled,
			Message:            `backend "loki" is disabled in operator configuration`,
			LastTransitionTime: existingTime,
		}}
		observedGen := int64(1)

		changed := applyDisabledStatus(rec, cr, &conds, &observedGen, "loki")

		if changed {
			t.Fatal("expected changed=false when condition + generation are identical")
		}
		if !conds[0].LastTransitionTime.Equal(&existingTime) {
			t.Fatalf("LastTransitionTime mutated to %v from %v",
				conds[0].LastTransitionTime, existingTime)
		}
		if events := drainEvents(rec); len(events) != 0 {
			t.Fatalf("expected no event on no-op, got %v", events)
		}
	})

	t.Run("generation drift triggers changed=true even when condition is identical", func(t *testing.T) {
		rec := record.NewFakeRecorder(8)
		cr := fakeCR(2)

		conds := []metav1.Condition{{
			Type:    v1alpha1.ConditionApplied,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.ReasonTargetDisabled,
			Message: `backend "loki" is disabled in operator configuration`,
		}}
		observedGen := int64(1) // CR.Generation=2, observed=1 → drifted

		changed := applyDisabledStatus(rec, cr, &conds, &observedGen, "loki")

		if !changed {
			t.Fatal("expected changed=true when only the generation drifted")
		}
		if observedGen != 2 {
			t.Fatalf("observedGen=%d, want 2 (CR's current generation)", observedGen)
		}
		// Condition itself did NOT transition, so no event should fire.
		if events := drainEvents(rec); len(events) != 0 {
			t.Fatalf("expected no event on generation-only drift, got %v", events)
		}
	})

	t.Run("transitioning from Applied=True to Applied=False emits an event", func(t *testing.T) {
		rec := record.NewFakeRecorder(8)
		cr := fakeCR(1)

		conds := []metav1.Condition{{
			Type:    v1alpha1.ConditionApplied,
			Status:  metav1.ConditionTrue,
			Reason:  v1alpha1.ReasonWrittenToConfigMap,
			Message: "applied to loki-runtime-tenants",
		}}
		observedGen := int64(1)

		changed := applyDisabledStatus(rec, cr, &conds, &observedGen, "mimir")

		if !changed {
			t.Fatal("expected changed=true on True→False transition")
		}
		if events := drainEvents(rec); len(events) != 1 {
			t.Fatalf("expected one TargetDisabled event on real transition, got %v", events)
		}
	})

	t.Run("nil recorder is tolerated without panic", func(t *testing.T) {
		cr := fakeCR(1)
		var conds []metav1.Condition
		var observedGen int64

		// Should not panic even though recorder is nil — applyDisabledStatus
		// is called this way in early-bootstrap edge cases.
		changed := applyDisabledStatus(nil, cr, &conds, &observedGen, "loki")
		if !changed {
			t.Fatal("expected changed=true on first transition even with nil recorder")
		}
	})
}
