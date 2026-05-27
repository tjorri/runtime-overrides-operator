// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/tjorri/runtime-overrides-operator/internal/apply"
	"github.com/tjorri/runtime-overrides-operator/internal/render"
)

// BootstrapRunnable writes an empty `overrides: {}` output ConfigMap at
// controller startup for each configured target. This guarantees the file
// the Loki/Mimir installer's `runtime_config.file` reference points at
// always exists, even before any CR is authored. Idempotent
// via the apply helper's hash-compare-skip.
//
// Implements manager.Runnable so it runs once at manager start, before
// any reconciler fires. It does NOT need leader election — both replicas
// can safely call SSA with the same content; the operation is idempotent.
type BootstrapRunnable struct {
	Client    client.Client
	Targets   []BootstrapTarget
	HashCache *HashCache
}

// BootstrapTarget identifies one ConfigMap to bootstrap.
type BootstrapTarget struct {
	Name     string // metric/log label (e.g. "loki")
	OutputCM types.NamespacedName
}

var _ manager.Runnable = (*BootstrapRunnable)(nil)
var _ manager.LeaderElectionRunnable = (*BootstrapRunnable)(nil)

// NeedLeaderElection returns false — the bootstrap is idempotent and
// running on both replicas is fine.
func (b *BootstrapRunnable) NeedLeaderElection() bool { return false }

// Start ensures each configured output ConfigMap exists and has the
// operator's content fingerprint cached. Behavior per target:
//
//   - CM missing: write `overrides: {}` so the file the Loki/Mimir
//     installer's runtime_config.file CSV references is never absent.
//     Populates HashCache with the empty-body hash.
//   - CM exists, body non-empty: leave it alone. This is the no-race
//     case for restarts — a previous operator instance (or this one,
//     pre-restart) wrote real content; overwriting with empty would
//     briefly serve `overrides: {}` to dskit before the reconciler's
//     first pass catches up. Populate HashCache with the LIVE hash so
//     the drift watch doesn't false-positive on the first reconcile.
//   - CM exists, body empty: identical to the missing case; the
//     hash-compare-skip inside apply.ConfigMap makes this a no-op.
//
// Implements manager.Runnable + LeaderElectionRunnable. NeedLeaderElection
// is false: SSA bootstrap is idempotent across replicas.
func (b *BootstrapRunnable) Start(ctx context.Context) error {
	for _, t := range b.Targets {
		live := &corev1.ConfigMap{}
		err := b.Client.Get(ctx, t.OutputCM, live)
		switch {
		case err == nil:
			// CM exists — record its hash so the drift watch has a
			// baseline. Don't overwrite, even if the body is empty
			// (the next reconcile will refresh it idempotently anyway).
			if b.HashCache != nil {
				body := live.Data[render.DataKey]
				b.HashCache.Set(t.Name, render.Hash([]byte(body)))
			}
			continue
		case apierrors.IsNotFound(err):
			// fall through to create below
		default:
			return fmt.Errorf("bootstrap get %s for %s: %w", t.OutputCM, t.Name, err)
		}

		body, err := render.Render(render.Output{})
		if err != nil {
			return fmt.Errorf("bootstrap render for %s: %w", t.Name, err)
		}
		res, err := apply.ConfigMap(ctx, b.Client, t.OutputCM, body)
		if err != nil {
			return fmt.Errorf("bootstrap apply for %s: %w", t.Name, err)
		}
		if b.HashCache != nil {
			b.HashCache.Set(t.Name, res.Hash)
		}
	}
	return nil
}
