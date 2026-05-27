// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package apply

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/tjorri/runtime-overrides-operator/internal/render"
)

// Note: controller-runtime's fake client doesn't fully simulate SSA's
// managedFields behavior, but it does honor the Patch call with the
// apply media type — sufficient to exercise the "create / no-op / update"
// branches of our wrapper.
func newFakeClient(initialObjs ...runtime.Object) *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithRuntimeObjects(initialObjs...)
}

func TestConfigMap_CreatesWhenMissing(t *testing.T) {
	c := newFakeClient().Build()
	ctx := context.Background()
	body := []byte("overrides:\n  acme:\n    x: 1\n")
	name := types.NamespacedName{Namespace: "monitoring", Name: "loki-runtime-tenants"}

	res, err := ConfigMap(ctx, c, name, body)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Skipped {
		t.Errorf("expected create-path, got Skipped=true")
	}
	if res.Hash != render.Hash(body) {
		t.Errorf("hash mismatch: got %q, want %q", res.Hash, render.Hash(body))
	}

	got := &corev1.ConfigMap{}
	if err := c.Get(ctx, name, got); err != nil {
		t.Fatalf("get after apply: %v", err)
	}
	if got.Data[render.DataKey] != string(body) {
		t.Errorf("data mismatch: got %q", got.Data[render.DataKey])
	}
	if got.Labels[ManagedByLabel] != ManagedByValue {
		t.Errorf("managed-by label not set: %v", got.Labels)
	}
}

func TestConfigMap_NoOpWhenHashMatches(t *testing.T) {
	body := []byte("overrides:\n  acme:\n    x: 1\n")
	name := types.NamespacedName{Namespace: "monitoring", Name: "loki-runtime-tenants"}
	existing := &corev1.ConfigMap{}
	existing.Name = name.Name
	existing.Namespace = name.Namespace
	existing.Data = map[string]string{render.DataKey: string(body)}

	c := newFakeClient(existing).Build()
	ctx := context.Background()

	res, err := ConfigMap(ctx, c, name, body)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Skipped {
		t.Errorf("expected hash-match no-op, got Skipped=false")
	}
	if res.Hash != render.Hash(body) {
		t.Errorf("hash mismatch: %q vs %q", res.Hash, render.Hash(body))
	}
}

func TestConfigMap_UpdatesWhenContentDiffers(t *testing.T) {
	name := types.NamespacedName{Namespace: "monitoring", Name: "loki-runtime-tenants"}
	old := &corev1.ConfigMap{}
	old.Name = name.Name
	old.Namespace = name.Namespace
	old.Data = map[string]string{render.DataKey: "overrides: {}\n"}

	c := newFakeClient(old).Build()
	ctx := context.Background()

	newBody := []byte("overrides:\n  acme:\n    x: 99\n")
	res, err := ConfigMap(ctx, c, name, newBody)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Skipped {
		t.Errorf("expected update, got Skipped=true")
	}

	got := &corev1.ConfigMap{}
	if err := c.Get(ctx, name, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Data[render.DataKey] != string(newBody) {
		t.Errorf("update didn't take: %q", got.Data[render.DataKey])
	}
}
