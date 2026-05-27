// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// rawJSON is a tiny helper to build a *runtime.RawExtension from a Go literal.
func rawJSON(t *testing.T, v any) *runtime.RawExtension {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &runtime.RawExtension{Raw: b}
}

func TestLokiTenantOverride_DeepCopyRoundTrip(t *testing.T) {
	orig := &LokiTenantOverride{
		ObjectMeta: metav1.ObjectMeta{Name: "boost", Namespace: "acme"},
		Spec: LokiTenantOverrideSpec{
			TenantId: "acme",
			Weight:   100,
			Overrides: rawJSON(t, map[string]any{
				"ingestion_rate_mb":       32,
				"ingestion_burst_size_mb": 64,
				"nested": map[string]any{
					"a": "b",
					"c": []any{1, 2, 3},
				},
			}),
		},
		Status: LokiTenantOverrideStatus{
			ObservedGeneration: 7,
			EffectiveTenantId:  "acme",
			Conditions: []metav1.Condition{
				{Type: ConditionValidated, Status: metav1.ConditionTrue, Reason: ReasonValidationSucceeded},
				{Type: ConditionApplied, Status: metav1.ConditionTrue, Reason: ReasonWrittenToConfigMap},
			},
			ContributingPeers: []ContributingPeer{
				{Namespace: "acme", Name: "base", Weight: 0},
			},
		},
	}

	copied := orig.DeepCopy()

	if !reflect.DeepEqual(orig, copied) {
		t.Fatalf("DeepCopy result is not equal to original")
	}

	// Mutate the original; the copy must remain unaffected.
	orig.Spec.Weight = 999
	orig.Spec.Overrides.Raw = []byte(`{"changed":true}`)
	orig.Status.Conditions[0].Reason = "Changed"
	orig.Status.ContributingPeers[0].Name = "changed"

	if copied.Spec.Weight == 999 {
		t.Errorf("copy shares Spec.Weight with original")
	}
	if string(copied.Spec.Overrides.Raw) == `{"changed":true}` {
		t.Errorf("copy shares Spec.Overrides.Raw with original")
	}
	if copied.Status.Conditions[0].Reason == "Changed" {
		t.Errorf("copy shares Status.Conditions slice with original")
	}
	if copied.Status.ContributingPeers[0].Name == "changed" {
		t.Errorf("copy shares Status.ContributingPeers slice with original")
	}
}

func TestMimirTenantOverride_DeepCopyRoundTrip(t *testing.T) {
	orig := &MimirTenantOverride{
		ObjectMeta: metav1.ObjectMeta{Name: "boost", Namespace: "acme"},
		Spec: MimirTenantOverrideSpec{
			TenantId: "acme",
			Weight:   100,
			Overrides: rawJSON(t, map[string]any{
				"ingestion_rate":             50000,
				"max_global_series_per_user": 1500000,
			}),
		},
		Status: MimirTenantOverrideStatus{
			ObservedGeneration: 3,
			EffectiveTenantId:  "acme",
		},
	}

	copied := orig.DeepCopy()

	if !reflect.DeepEqual(orig, copied) {
		t.Fatalf("DeepCopy result is not equal to original")
	}

	orig.Spec.Overrides.Raw = []byte(`{}`)
	if string(copied.Spec.Overrides.Raw) == `{}` {
		t.Errorf("copy shares Spec.Overrides.Raw with original")
	}
}

func TestLokiTenantOverrideList_DeepCopyRoundTrip(t *testing.T) {
	orig := &LokiTenantOverrideList{
		Items: []LokiTenantOverride{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "a"},
				Spec:       LokiTenantOverrideSpec{Overrides: rawJSON(t, map[string]any{"k": "v"})},
			},
		},
	}

	copied := orig.DeepCopy()

	if !reflect.DeepEqual(orig, copied) {
		t.Fatalf("DeepCopy result is not equal to original")
	}

	orig.Items[0].Spec.Overrides.Raw = []byte(`{}`)
	if string(copied.Items[0].Spec.Overrides.Raw) == `{}` {
		t.Errorf("copy shares item Overrides.Raw with original")
	}
}
