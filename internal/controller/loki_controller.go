// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// Package controller hosts the per-target reconcilers. Each reconciler
// watches its CR kind cluster-wide, validates per CR, deep-merges by tenant,
// writes one output ConfigMap via Server-Side Apply, and updates per-CR
// status conditions.
//
// No finalizers: the output ConfigMap is internal state
// the operator owns. Deleted CRs drop their contribution on the next
// reconcile via informer-cache eventual consistency.
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
	"github.com/tjorri/runtime-overrides-operator/internal/apply"
	"github.com/tjorri/runtime-overrides-operator/internal/merge"
	"github.com/tjorri/runtime-overrides-operator/internal/metrics"
	"github.com/tjorri/runtime-overrides-operator/internal/render"
	"github.com/tjorri/runtime-overrides-operator/internal/validate"
)

const lokiTargetLabel = "loki"

// LokiReconciler reconciles all LokiTenantOverride CRs in the cluster into
// a single output ConfigMap and updates per-CR status conditions.
//
// The reconciler uses a target-wide singleton key. CR events map to the
// same key via a static EventHandler.MapFunc — every CR change triggers
// one full re-render. This trades per-CR locality for simpler invariants
// (the output CM is always the union of *all* current valid CRs).
type LokiReconciler struct {
	Client    client.Client
	OutputCM  types.NamespacedName
	Validator validate.Validator
	Recorder  record.EventRecorder
	HashCache *HashCache
}

// lokiReconcileKey is the singleton key used by the per-target queue. Any
// LokiTenantOverride event remaps to this and triggers one full re-render.
var lokiReconcileKey = types.NamespacedName{Namespace: "_target", Name: "loki"}

// crEnvelope carries everything we need about one CR through the reconcile
// pipeline, including its Layer-3 validation result.
type crEnvelope struct {
	CR        *v1alpha1.LokiTenantOverride
	Effective string // effective tenantId (defaults to namespace)
	Override  merge.Override
	ValidErr  error // non-nil → excluded from merge (Validated=False)
}

// +kubebuilder:rbac:groups=runtimeoverrides.io,resources=lokitenantoverrides,verbs=get;list;watch
// +kubebuilder:rbac:groups=runtimeoverrides.io,resources=lokitenantoverrides/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is the per-target reconcile loop. Drift watch + bootstrap
// land in M6; the admission webhook is M7.
func (r *LokiReconciler) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("target", lokiTargetLabel)

	// Drift check: if the live CM's content hash doesn't
	// match the last hash we applied, a third party clobbered the file.
	// We record the drift here; the SSA apply later in this reconcile
	// reverts it.
	driftDetected, conflictingMgr := r.checkDrift(ctx)
	if driftDetected {
		metrics.OutputDriftTotal.WithLabelValues(lokiTargetLabel, conflictingMgr).Inc()
		logger.Info("output ConfigMap drift detected — reverting",
			"conflicting_field_manager", conflictingMgr)
	}

	envelopes, err := r.gather(ctx)
	if err != nil {
		metrics.ReconcileTotal.WithLabelValues(lokiTargetLabel, string(metrics.ResultWriteFailed)).Inc()
		return reconcile.Result{}, err
	}

	// Build the merged output across valid CRs and track per-leaf conflicts.
	valid := make([]merge.Override, 0, len(envelopes))
	for _, e := range envelopes {
		if e.ValidErr == nil {
			valid = append(valid, e.Override)
		}
	}
	groups := merge.GroupByTenant(valid)
	out := render.Output{}
	allConflicts := map[string][]merge.FieldConflict{} // tenant -> conflicts
	for tenant, group := range groups {
		merge.SortGroup(group)
		merged, conflicts := merge.TrackingMerge(tenant, group)
		// Belt-and-suspenders: re-validate the merged result per tenant.
		if mergedErr := r.Validator.Validate(merged); mergedErr != nil {
			logger.Info("skipping tenant whose merged result fails validation",
				"tenant", tenant, "err", mergedErr)
			metrics.ValidationErrorsTotal.WithLabelValues(
				lokiTargetLabel, tenant, "merged_result_invalid").Inc()
			continue
		}
		out[tenant] = merged
		allConflicts[tenant] = conflicts
	}

	// Render + SSA apply.
	body, err := render.Render(out)
	if err != nil {
		metrics.ReconcileTotal.WithLabelValues(lokiTargetLabel, string(metrics.ResultWriteFailed)).Inc()
		return reconcile.Result{}, fmt.Errorf("render: %w", err)
	}
	metrics.OutputSizeBytes.WithLabelValues(lokiTargetLabel).Set(float64(len(body)))

	res, err := apply.ConfigMap(ctx, r.Client, r.OutputCM, body)
	if err != nil {
		metrics.ReconcileTotal.WithLabelValues(lokiTargetLabel, string(metrics.ResultWriteFailed)).Inc()
		return reconcile.Result{}, fmt.Errorf("apply: %w", err)
	}

	if res.Skipped {
		metrics.ReconcileTotal.WithLabelValues(lokiTargetLabel, string(metrics.ResultNoop)).Inc()
		logger.V(1).Info("output CM up to date",
			"hash", res.Hash, "generation", res.Generation, "tenants", len(out))
	} else {
		metrics.ReconcileTotal.WithLabelValues(lokiTargetLabel, string(metrics.ResultSuccess)).Inc()
		logger.Info("output CM updated",
			"hash", res.Hash, "generation", res.Generation, "tenants", len(out))
	}
	if r.HashCache != nil {
		r.HashCache.Set(lokiTargetLabel, res.Hash)
	}

	// Emit ConfigMapDrifted event after the revert, so users see "drift
	// happened and we fixed it" rather than just an unexplained reconcile.
	if driftDetected && r.Recorder != nil {
		live := &corev1.ConfigMap{}
		if errGet := r.Client.Get(ctx, r.OutputCM, live); errGet == nil {
			r.Recorder.Eventf(live, corev1.EventTypeWarning, "ConfigMapDrifted",
				"third-party write reverted (field manager: %s)", conflictingMgr)
		}
	}

	metrics.ActiveCRs.WithLabelValues(lokiTargetLabel).Set(float64(len(envelopes)))
	metrics.ActiveTenants.WithLabelValues(lokiTargetLabel).Set(float64(len(out)))
	metrics.LastReconcileTimestamp.WithLabelValues(lokiTargetLabel).Set(float64(time.Now().Unix()))

	// Update per-CR status + emit events for conflicts. Errors here are
	// logged but don't fail the reconcile — the ConfigMap is already correct.
	// A 409 from a stale cache requeues so the next pass uses fresh CRs.
	if conflictHit := r.updateStatuses(ctx, envelopes, valid, allConflicts, res); conflictHit {
		return reconcile.Result{Requeue: true}, nil
	}

	return reconcile.Result{}, nil
}

// checkDrift inspects the live output ConfigMap and compares its content
// hash against the last hash we applied (HashCache). Returns true and the
// conflicting field-manager name when a third-party write is detected.
// Safe before any apply has happened — returns (false, "") when the cache
// has no entry yet.
func (r *LokiReconciler) checkDrift(ctx context.Context) (bool, string) {
	if r.HashCache == nil {
		return false, ""
	}
	ourHash, have := r.HashCache.Get(lokiTargetLabel)
	if !have {
		return false, ""
	}
	live := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, r.OutputCM, live); err != nil {
		return false, ""
	}
	liveBody := live.Data[render.DataKey]
	if render.Hash([]byte(liveBody)) == ourHash {
		return false, ""
	}
	return true, extractConflictingFieldManager(live, apply.FieldManager)
}

// gather lists all LokiTenantOverride CRs cluster-wide, decodes their
// overrides, defaults tenantId from namespace, and runs Layer-3 validation
// on each. The result records validation errors per CR for status reporting.
func (r *LokiReconciler) gather(ctx context.Context) ([]crEnvelope, error) {
	var crList v1alpha1.LokiTenantOverrideList
	if err := r.Client.List(ctx, &crList); err != nil {
		return nil, fmt.Errorf("list LokiTenantOverrides: %w", err)
	}

	envelopes := make([]crEnvelope, 0, len(crList.Items))
	for i := range crList.Items {
		cr := &crList.Items[i]
		raw, decErr := unmarshalRawExtension(cr.Spec.Overrides)
		effective := cr.Spec.TenantId
		if effective == "" {
			effective = cr.Namespace
		}
		e := crEnvelope{
			CR:        cr,
			Effective: effective,
			Override: merge.Override{
				Namespace: cr.Namespace,
				Name:      cr.Name,
				TenantId:  effective,
				Weight:    cr.Spec.Weight,
				Overrides: raw,
			},
		}
		if decErr != nil {
			e.ValidErr = decErr
			metrics.ValidationErrorsTotal.WithLabelValues(
				lokiTargetLabel, effective, "decode_failed").Inc()
		} else if vErr := r.Validator.Validate(raw); vErr != nil {
			e.ValidErr = vErr
			metrics.ValidationErrorsTotal.WithLabelValues(
				lokiTargetLabel, effective, "upstream_validate_failed").Inc()
		}
		envelopes = append(envelopes, e)
	}
	return envelopes, nil
}

// updateStatuses writes Validated/Applied conditions on each CR, populates
// ContributingPeers, and emits FieldOverridden events for conflict losers.
// Status writes are transition-gated so the API
// server isn't write-spammed on every reconcile.
//
// Returns true if any status write hit an etcd conflict (stale cached CR).
// The caller should requeue so convergence happens this reconcile generation
// rather than waiting for the next event.
func (r *LokiReconciler) updateStatuses(
	ctx context.Context,
	envelopes []crEnvelope,
	validOverrides []merge.Override,
	allConflicts map[string][]merge.FieldConflict,
	applyRes apply.Result,
) (conflictHit bool) {
	logger := log.FromContext(ctx).WithValues("target", lokiTargetLabel)

	// Build a lookup: (ns/name) -> position of any CR that LOST a conflict.
	// Used to emit FieldOverridden events on losers and increment metrics.
	for tenant, conflicts := range allConflicts {
		for _, fc := range conflicts {
			field := strings.Join(fc.Path, ".")
			if fc.SameWeight {
				metrics.FieldConflictsTotal.WithLabelValues(
					lokiTargetLabel, tenant, field).Inc()
			}
			for _, loser := range fc.Losers {
				cr := lookupCR(envelopes, loser.Namespace, loser.Name)
				if cr == nil {
					continue
				}
				msg := fmt.Sprintf("field %q overridden by %s/%s (weight=%d) for tenant %q",
					field, fc.Winner.Namespace, fc.Winner.Name, fc.Winner.Weight, tenant)
				if r.Recorder != nil {
					r.Recorder.Event(cr, corev1.EventTypeNormal, "FieldOverridden", msg)
				}
			}
		}
	}

	for i := range envelopes {
		e := &envelopes[i]
		desired := r.computeDesiredStatus(e, validOverrides, applyRes)

		needsUpdate := desired.ObservedGeneration != e.CR.Status.ObservedGeneration ||
			desired.EffectiveTenantId != e.CR.Status.EffectiveTenantId ||
			conditionsChanged(e.CR.Status.Conditions, desired.Conditions) ||
			peersChanged(e.CR.Status.ContributingPeers, desired.ContributingPeers)
		if !needsUpdate {
			continue
		}

		// Merge desired into a copy, preserving timestamps on unchanged conditions.
		updated := e.CR.DeepCopy()
		updated.Status.ObservedGeneration = desired.ObservedGeneration
		updated.Status.EffectiveTenantId = desired.EffectiveTenantId
		updated.Status.ContributingPeers = desired.ContributingPeers
		for _, c := range desired.Conditions {
			updated.Status.Conditions = upsertCondition(updated.Status.Conditions, c)
		}
		if err := r.Client.Status().Update(ctx, updated); err != nil {
			switch {
			case apierrors.IsConflict(err):
				conflictHit = true
				logger.Info("status update conflict; will requeue",
					"cr", e.CR.Name, "ns", e.CR.Namespace)
			case apierrors.IsNotFound(err):
				// CR was deleted under us; nothing to converge.
			default:
				logger.Error(err, "status update failed",
					"cr", e.CR.Name, "ns", e.CR.Namespace)
			}
		}
	}
	return conflictHit
}

// computeDesiredStatus builds the target Status block for one CR based on
// the reconcile outcome.
func (r *LokiReconciler) computeDesiredStatus(
	e *crEnvelope,
	validOverrides []merge.Override,
	applyRes apply.Result,
) v1alpha1.LokiTenantOverrideStatus {
	st := v1alpha1.LokiTenantOverrideStatus{
		ObservedGeneration: e.CR.Generation,
		EffectiveTenantId:  e.Effective,
	}

	// Validated condition.
	if e.ValidErr == nil {
		st.Conditions = append(st.Conditions, metav1.Condition{
			Type:    v1alpha1.ConditionValidated,
			Status:  metav1.ConditionTrue,
			Reason:  v1alpha1.ReasonValidationSucceeded,
			Message: "parsed strictly and passed upstream Validate()",
		})
	} else {
		st.Conditions = append(st.Conditions, metav1.Condition{
			Type:    v1alpha1.ConditionValidated,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.ReasonValidationFailed,
			Message: e.ValidErr.Error(),
		})
	}

	// Applied condition.
	if e.ValidErr == nil {
		msg := fmt.Sprintf(
			"Contributed to ConfigMap %s/%s generation %d at weight %d (output hash sha256:%s)",
			r.OutputCM.Namespace, r.OutputCM.Name,
			applyRes.Generation, e.Override.Weight, applyRes.Hash,
		)
		st.Conditions = append(st.Conditions, metav1.Condition{
			Type:    v1alpha1.ConditionApplied,
			Status:  metav1.ConditionTrue,
			Reason:  v1alpha1.ReasonWrittenToConfigMap,
			Message: msg,
		})
	} else {
		st.Conditions = append(st.Conditions, metav1.Condition{
			Type:    v1alpha1.ConditionApplied,
			Status:  metav1.ConditionFalse,
			Reason:  v1alpha1.ReasonValidationFailed,
			Message: "skipped from merge due to Validated=False",
		})
	}

	// Peers (only for valid CRs; invalid CRs aren't part of the union).
	if e.ValidErr == nil {
		st.ContributingPeers = computePeers(e.Override, validOverrides)
	}

	return st
}

// lookupCR finds a CR by namespace/name in the envelope slice.
func lookupCR(envelopes []crEnvelope, namespace, name string) *v1alpha1.LokiTenantOverride {
	for i := range envelopes {
		if envelopes[i].CR.Namespace == namespace && envelopes[i].CR.Name == name {
			return envelopes[i].CR
		}
	}
	return nil
}

// SetupWithManager wires the controller to its CR informer plus the
// output ConfigMap watch (drift detection). Every LokiTenantOverride
// event and every change to the output CM remaps to the singleton
// lokiReconcileKey so a single queue serializes reconciles.
func (r *LokiReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("runtime-overrides-operator-loki") //nolint:staticcheck // SA1019: new events.EventRecorder API has a structurally different interface (regarding+related+action+note); revisit when migrating
	}
	mapper := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, _ client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: lokiReconcileKey}}
	})
	return ctrl.NewControllerManagedBy(mgr).
		Named("lokitenantoverride").
		Watches(
			&v1alpha1.LokiTenantOverride{},
			mapper,
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&corev1.ConfigMap{},
			configMapHandler(lokiReconcileKey),
			builder.WithPredicates(outputCMPredicate(r.OutputCM)),
		).
		Complete(r)
}

// unmarshalRawExtension decodes the runtime.RawExtension JSON body into a
// freeform map. The webhook already strict-parses against the upstream
// Limits schema (M7); here we just need the raw map for merge purposes.
func unmarshalRawExtension(raw *runtime.RawExtension) (map[string]any, error) {
	if raw == nil || len(raw.Raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw.Raw, &out); err != nil {
		return nil, fmt.Errorf("unmarshal overrides: %w", err)
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}
