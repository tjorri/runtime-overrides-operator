// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const mimirTargetLabel = "mimir"

// MimirReconciler is the Mimir analog of LokiReconciler. Symmetric in shape;
// differs in CR kind, validator, output CM, and labels. See loki_controller.go
// for the detailed comments — the two reconcilers are kept parallel rather
// than abstracted to keep each one readable end-to-end.
type MimirReconciler struct {
	Client    client.Client
	OutputCM  types.NamespacedName
	Validator validate.Validator
	Recorder  record.EventRecorder
	HashCache *HashCache
}

var mimirReconcileKey = types.NamespacedName{Namespace: "_target", Name: "mimir"}

type mimirCREnvelope struct {
	CR        *v1alpha1.MimirTenantOverride
	Effective string
	Override  merge.Override
	ValidErr  error
}

// +kubebuilder:rbac:groups=runtimeoverrides.io,resources=mimirtenantoverrides,verbs=get;list;watch
// +kubebuilder:rbac:groups=runtimeoverrides.io,resources=mimirtenantoverrides/status,verbs=get;update;patch

func (r *MimirReconciler) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("target", mimirTargetLabel)

	driftDetected, conflictingMgr := r.checkDrift(ctx)
	if driftDetected {
		metrics.OutputDriftTotal.WithLabelValues(mimirTargetLabel, conflictingMgr).Inc()
		logger.Info("output ConfigMap drift detected — reverting",
			"conflicting_field_manager", conflictingMgr)
	}

	envelopes, err := r.gather(ctx)
	if err != nil {
		metrics.ReconcileTotal.WithLabelValues(mimirTargetLabel, string(metrics.ResultWriteFailed)).Inc()
		return reconcile.Result{}, err
	}

	valid := make([]merge.Override, 0, len(envelopes))
	for _, e := range envelopes {
		if e.ValidErr == nil {
			valid = append(valid, e.Override)
		}
	}
	groups := merge.GroupByTenant(valid)
	out := render.Output{}
	allConflicts := map[string][]merge.FieldConflict{}
	for tenant, group := range groups {
		merge.SortGroup(group)
		merged, conflicts := merge.TrackingMerge(tenant, group)
		if mergedErr := r.Validator.Validate(merged); mergedErr != nil {
			logger.Info("skipping tenant whose merged result fails validation",
				"tenant", tenant, "err", mergedErr)
			metrics.ValidationErrorsTotal.WithLabelValues(
				mimirTargetLabel, tenant, "merged_result_invalid").Inc()
			continue
		}
		out[tenant] = merged
		allConflicts[tenant] = conflicts
	}

	body, err := render.Render(out)
	if err != nil {
		metrics.ReconcileTotal.WithLabelValues(mimirTargetLabel, string(metrics.ResultWriteFailed)).Inc()
		return reconcile.Result{}, fmt.Errorf("render: %w", err)
	}
	metrics.OutputSizeBytes.WithLabelValues(mimirTargetLabel).Set(float64(len(body)))

	res, err := apply.ConfigMap(ctx, r.Client, r.OutputCM, body)
	if err != nil {
		metrics.ReconcileTotal.WithLabelValues(mimirTargetLabel, string(metrics.ResultWriteFailed)).Inc()
		return reconcile.Result{}, fmt.Errorf("apply: %w", err)
	}

	if res.Skipped {
		metrics.ReconcileTotal.WithLabelValues(mimirTargetLabel, string(metrics.ResultNoop)).Inc()
		logger.V(1).Info("output CM up to date",
			"hash", res.Hash, "generation", res.Generation, "tenants", len(out))
	} else {
		metrics.ReconcileTotal.WithLabelValues(mimirTargetLabel, string(metrics.ResultSuccess)).Inc()
		logger.Info("output CM updated",
			"hash", res.Hash, "generation", res.Generation, "tenants", len(out))
	}
	if r.HashCache != nil {
		r.HashCache.Set(mimirTargetLabel, res.Hash)
	}

	if driftDetected && r.Recorder != nil {
		live := &corev1.ConfigMap{}
		if errGet := r.Client.Get(ctx, r.OutputCM, live); errGet == nil {
			r.Recorder.Eventf(live, corev1.EventTypeWarning, "ConfigMapDrifted",
				"third-party write reverted (field manager: %s)", conflictingMgr)
		}
	}

	metrics.ActiveCRs.WithLabelValues(mimirTargetLabel).Set(float64(len(envelopes)))
	metrics.ActiveTenants.WithLabelValues(mimirTargetLabel).Set(float64(len(out)))
	metrics.LastReconcileTimestamp.WithLabelValues(mimirTargetLabel).Set(float64(time.Now().Unix()))

	if conflictHit := r.updateStatuses(ctx, envelopes, valid, allConflicts, res); conflictHit {
		return reconcile.Result{Requeue: true}, nil
	}

	return reconcile.Result{}, nil
}

func (r *MimirReconciler) checkDrift(ctx context.Context) (bool, string) {
	if r.HashCache == nil {
		return false, ""
	}
	ourHash, have := r.HashCache.Get(mimirTargetLabel)
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

func (r *MimirReconciler) gather(ctx context.Context) ([]mimirCREnvelope, error) {
	var crList v1alpha1.MimirTenantOverrideList
	if err := r.Client.List(ctx, &crList); err != nil {
		return nil, fmt.Errorf("list MimirTenantOverrides: %w", err)
	}
	envelopes := make([]mimirCREnvelope, 0, len(crList.Items))
	for i := range crList.Items {
		cr := &crList.Items[i]
		raw, decErr := unmarshalRawExtension(cr.Spec.Overrides)
		effective := cr.Spec.TenantId
		if effective == "" {
			effective = cr.Namespace
		}
		e := mimirCREnvelope{
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
				mimirTargetLabel, effective, "decode_failed").Inc()
		} else if vErr := r.Validator.Validate(raw); vErr != nil {
			e.ValidErr = vErr
			metrics.ValidationErrorsTotal.WithLabelValues(
				mimirTargetLabel, effective, "upstream_validate_failed").Inc()
		}
		envelopes = append(envelopes, e)
	}
	return envelopes, nil
}

// updateStatuses mirrors the Loki reconciler's contract — see its doc
// comment. Returns true if any status write hit an etcd conflict.
func (r *MimirReconciler) updateStatuses(
	ctx context.Context,
	envelopes []mimirCREnvelope,
	validOverrides []merge.Override,
	allConflicts map[string][]merge.FieldConflict,
	applyRes apply.Result,
) (conflictHit bool) {
	logger := log.FromContext(ctx).WithValues("target", mimirTargetLabel)

	for tenant, conflicts := range allConflicts {
		for _, fc := range conflicts {
			field := strings.Join(fc.Path, ".")
			if fc.SameWeight {
				metrics.FieldConflictsTotal.WithLabelValues(
					mimirTargetLabel, tenant, field).Inc()
			}
			for _, loser := range fc.Losers {
				cr := lookupMimirCR(envelopes, loser.Namespace, loser.Name)
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

func (r *MimirReconciler) computeDesiredStatus(
	e *mimirCREnvelope,
	validOverrides []merge.Override,
	applyRes apply.Result,
) v1alpha1.MimirTenantOverrideStatus {
	st := v1alpha1.MimirTenantOverrideStatus{
		ObservedGeneration: e.CR.Generation,
		EffectiveTenantId:  e.Effective,
	}
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
	if e.ValidErr == nil {
		st.ContributingPeers = computePeers(e.Override, validOverrides)
	}
	return st
}

func lookupMimirCR(envelopes []mimirCREnvelope, namespace, name string) *v1alpha1.MimirTenantOverride {
	for i := range envelopes {
		if envelopes[i].CR.Namespace == namespace && envelopes[i].CR.Name == name {
			return envelopes[i].CR
		}
	}
	return nil
}

func (r *MimirReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("runtime-overrides-operator-mimir") //nolint:staticcheck // SA1019: new events.EventRecorder API has a structurally different interface (regarding+related+action+note); revisit when migrating
	}
	mapper := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, _ client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: mimirReconcileKey}}
	})
	return ctrl.NewControllerManagedBy(mgr).
		Named("mimirtenantoverride").
		Watches(
			&v1alpha1.MimirTenantOverride{},
			mapper,
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&corev1.ConfigMap{},
			configMapHandler(mimirReconcileKey),
			builder.WithPredicates(outputCMPredicate(r.OutputCM)),
		).
		Complete(r)
}
