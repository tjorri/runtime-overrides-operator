// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// Package apply provides the thin Server-Side Apply wrapper the controller
// uses to write its output ConfigMap. Centralizes the fieldManager string,
// the hash-compare-before-apply skip check, and the labels we attach.
package apply

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/tjorri/runtime-overrides-operator/internal/render"
)

// FieldManager is the Server-Side Apply field manager string the operator
// uses on every write to its output ConfigMaps. Surfaced via
// metadata.managedFields so the drift watch can identify
// "our own" writes vs third-party stomping.
const FieldManager = "runtime-overrides-operator"

// ManagedByLabel and ManagedByValue tag every output ConfigMap so operators
// can locate them by label selector, and so kustomize / Helm don't try to
// take ownership.
const (
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "runtime-overrides-operator"
)

// Result reports what an Apply call did.
type Result struct {
	// Hash is the SHA-256 of the body we ended up with (either freshly
	// applied or what was already there).
	Hash string
	// Generation is the live ConfigMap's metadata.generation after the
	// call. Zero if the CM doesn't exist (shouldn't happen post-Apply).
	Generation int64
	// Skipped is true when the live CM's content already matched the
	// desired body (hash equality) so no write was issued.
	Skipped bool
}

// ConfigMap performs an SSA upsert of a single-data-key ConfigMap with our
// canonical shape: data[render.DataKey] = body, managed-by label set.
// If the live ConfigMap's content hash already matches Hash(body), no write
// is issued (avoids triggering cluster-wide kubelet ConfigMap re-syncs on
// every reconcile when nothing changed.
// SSA uses Force=true so the operator's writes are authoritative — drift
// from other writers is detected and re-asserted by the drift watch
// within ~1s. The fieldManager string is FieldManager.
func ConfigMap(ctx context.Context, c client.Client, name types.NamespacedName, body []byte) (Result, error) {
	bodyHash := render.Hash(body)

	// Hash-compare: if live matches, no-op.
	live := &corev1.ConfigMap{}
	getErr := c.Get(ctx, name, live)
	switch {
	case getErr == nil:
		if liveBody, ok := live.Data[render.DataKey]; ok && render.Hash([]byte(liveBody)) == bodyHash {
			return Result{Hash: bodyHash, Generation: live.Generation, Skipped: true}, nil
		}
	case apierrors.IsNotFound(getErr):
		// fine — we'll create below
	default:
		return Result{}, fmt.Errorf("get configmap %s: %w", name, getErr)
	}

	desired := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
			},
		},
		Data: map[string]string{
			render.DataKey: string(body),
		},
	}

	// SA1019: client.Apply is soft-deprecated in favor of c.Apply(...) on
	// the typed-Client surface, but that path requires applyconfigurations
	// codegen for the typed object. For a single ConfigMap this is overkill;
	// revisit if we ever apply more types.
	if err := c.Patch(ctx, desired, client.Apply, //nolint:staticcheck
		client.FieldOwner(FieldManager),
		client.ForceOwnership,
	); err != nil {
		return Result{}, fmt.Errorf("ssa apply configmap %s: %w", name, err)
	}

	// Re-read for the post-apply generation.
	after := &corev1.ConfigMap{}
	if err := c.Get(ctx, name, after); err != nil {
		return Result{}, fmt.Errorf("re-get configmap %s after apply: %w", name, err)
	}
	return Result{Hash: bodyHash, Generation: after.Generation, Skipped: false}, nil
}
