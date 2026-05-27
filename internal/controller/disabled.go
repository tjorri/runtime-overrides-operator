// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
)

// LokiDisabledReconciler reconciles LokiTenantOverride CRs when the Loki
// target is disabled, surfacing an explicit Applied=False status with
// reason=TargetDisabled — never silently ignores them.
// Only one of LokiReconciler / LokiDisabledReconciler is registered per
// operator instance, based on config.
type LokiDisabledReconciler struct {
	Client   client.Client
	Recorder record.EventRecorder
}

// Reconcile sets Applied=False, reason=TargetDisabled on the CR.
func (r *LokiDisabledReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	cr := &v1alpha1.LokiTenantOverride{}
	if err := r.Client.Get(ctx, req.NamespacedName, cr); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	changed := applyDisabledStatus(r.Recorder, cr, &cr.Status.Conditions,
		&cr.Status.ObservedGeneration, "loki")
	if !changed {
		return reconcile.Result{}, nil
	}
	if err := r.Client.Status().Update(ctx, cr); err != nil {
		if apierrors.IsConflict(err) {
			return reconcile.Result{Requeue: true}, nil
		}
		return reconcile.Result{}, client.IgnoreNotFound(fmt.Errorf("update status: %w", err))
	}
	return reconcile.Result{}, nil
}

// SetupWithManager wires the controller to the LokiTenantOverride informer.
// GenerationChangedPredicate filters out our own status-only writes so we
// don't requeue ourselves on every Status().Update.
func (r *LokiDisabledReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("runtime-overrides-operator-loki-disabled") //nolint:staticcheck // SA1019: new events.EventRecorder API has a structurally different interface (regarding+related+action+note); revisit when migrating
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("lokitenantoverride-disabled").
		For(&v1alpha1.LokiTenantOverride{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// MimirDisabledReconciler is the Mimir-side analog of LokiDisabledReconciler.
type MimirDisabledReconciler struct {
	Client   client.Client
	Recorder record.EventRecorder
}

// Reconcile sets Applied=False, reason=TargetDisabled on the CR.
func (r *MimirDisabledReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	cr := &v1alpha1.MimirTenantOverride{}
	if err := r.Client.Get(ctx, req.NamespacedName, cr); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	changed := applyDisabledStatus(r.Recorder, cr, &cr.Status.Conditions,
		&cr.Status.ObservedGeneration, "mimir")
	if !changed {
		return reconcile.Result{}, nil
	}
	if err := r.Client.Status().Update(ctx, cr); err != nil {
		if apierrors.IsConflict(err) {
			return reconcile.Result{Requeue: true}, nil
		}
		return reconcile.Result{}, client.IgnoreNotFound(fmt.Errorf("update status: %w", err))
	}
	return reconcile.Result{}, nil
}

// SetupWithManager wires the controller to the MimirTenantOverride informer.
// See LokiDisabledReconciler.SetupWithManager for the predicate rationale.
func (r *MimirDisabledReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("runtime-overrides-operator-mimir-disabled") //nolint:staticcheck // SA1019: new events.EventRecorder API has a structurally different interface (regarding+related+action+note); revisit when migrating
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("mimirtenantoverride-disabled").
		For(&v1alpha1.MimirTenantOverride{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// applyDisabledStatus is the shared status-update for the disabled-target
// watchers. Sets Applied=False with reason=TargetDisabled and a clear
// message, transition-gated so the API server isn't write-spammed. Emits
// a TargetDisabled event on first transition.
//
// Returns true when the CR's status would actually change (condition
// transition or ObservedGeneration drift) — callers gate Status().Update
// on this so identical re-writes are skipped.
func applyDisabledStatus(
	recorder record.EventRecorder,
	obj client.Object,
	conditions *[]metav1.Condition,
	observedGen *int64,
	target string,
) bool {
	desired := metav1.Condition{
		Type:    v1alpha1.ConditionApplied,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonTargetDisabled,
		Message: fmt.Sprintf("backend %q is disabled in operator configuration", target),
	}
	existing := *conditions
	updated := upsertCondition(existing, desired)
	transitioned := conditionTransitioned(existing, updated, v1alpha1.ConditionApplied)
	genDrifted := *observedGen != obj.GetGeneration()

	*conditions = updated
	*observedGen = obj.GetGeneration()

	if transitioned && recorder != nil {
		recorder.Event(obj, corev1.EventTypeWarning, "TargetDisabled", desired.Message)
	}
	return transitioned || genDrifted
}

// conditionTransitioned returns true if the condition of the given Type
// in `before` and `after` have different LastTransitionTime values (or
// the condition was newly added).
func conditionTransitioned(before, after []metav1.Condition, t string) bool {
	var bTime, aTime metav1.Time
	for _, c := range before {
		if c.Type == t {
			bTime = c.LastTransitionTime
			break
		}
	}
	for _, c := range after {
		if c.Type == t {
			aTime = c.LastTransitionTime
			break
		}
	}
	return !aTime.Equal(&bTime)
}
