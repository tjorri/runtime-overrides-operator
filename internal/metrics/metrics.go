// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// Package metrics declares the operator's Prometheus metrics and registers
// them with controller-runtime's metrics registry. Imported for side effect
// from cmd/main.go via the init() in cmd, or referenced directly.
//
// Metric set. Naming convention: runtime_overrides_<name>.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// labelTarget is the metric label whose value identifies which backend
// (Loki or Mimir) the metric series describes.
const labelTarget = "target"

// ReconcileResult tags the result label on the reconcile counter.
type ReconcileResult string

const (
	ResultSuccess     ReconcileResult = "success"
	ResultNoop        ReconcileResult = "noop"
	ResultWriteFailed ReconcileResult = "write_failed"
)

var (
	// ReconcileTotal counts reconcile passes by target and outcome.
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "runtime_overrides_reconcile_total",
			Help: "Number of reconciles by target and result.",
		},
		[]string{labelTarget, "result"},
	)

	// ActiveTenants is the number of tenants with at least one valid
	// contributing CR in the latest output ConfigMap.
	ActiveTenants = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "runtime_overrides_active_tenants",
			Help: "Number of tenants represented in the operator's output ConfigMap.",
		},
		[]string{labelTarget},
	)

	// ActiveCRs is the number of CRs the operator has seen for each
	// target (any phase — included in merge or not).
	ActiveCRs = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "runtime_overrides_active_crs",
			Help: "Number of *TenantOverride CRs the operator is aware of.",
		},
		[]string{labelTarget},
	)

	// ValidationErrorsTotal counts CRs dropped at Layer 3 by reason.
	ValidationErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "runtime_overrides_validation_errors_total",
			Help: "Number of CRs dropped at Layer-3 validation by reason.",
		},
		[]string{labelTarget, "tenant", "reason"},
	)

	// FieldConflictsTotal counts same-weight field collisions where one
	// CR's contribution is silently overridden by another's via lex
	// tie-break — surfaces an authoring smell.
	FieldConflictsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "runtime_overrides_field_conflicts_total",
			Help: "Same-weight field-value collisions resolved by lex tie-break.",
		},
		[]string{labelTarget, "tenant", "field"},
	)

	// OutputSizeBytes is the size in bytes of the current output ConfigMap
	// body. Used to drive a Phase-2 alert at 80% of the ~1 MiB CM limit.
	OutputSizeBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "runtime_overrides_output_size_bytes",
			Help: "Size in bytes of the operator's rendered output ConfigMap body.",
		},
		[]string{labelTarget},
	)

	// LastReconcileTimestamp records when the last reconcile completed
	// per target. Drives the RuntimeOverridesReconcileStalled alert.
	LastReconcileTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "runtime_overrides_last_reconcile_timestamp_seconds",
			Help: "Unix timestamp of the last successful reconcile per target.",
		},
		[]string{labelTarget},
	)

	// OutputDriftTotal counts third-party writes to the output ConfigMap
	// that the drift watch reverted. Wired in M6.
	OutputDriftTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "runtime_overrides_output_drift_total",
			Help: "Third-party writes to the output ConfigMap that were reverted.",
		},
		[]string{labelTarget, "conflicting_field_manager"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		ReconcileTotal,
		ActiveTenants,
		ActiveCRs,
		ValidationErrorsTotal,
		FieldConflictsTotal,
		OutputSizeBytes,
		LastReconcileTimestamp,
		OutputDriftTotal,
	)
}
