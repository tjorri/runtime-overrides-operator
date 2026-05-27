// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package v1alpha1

// ContributingPeer references another *TenantOverride CR that targets the
// same tenantId. Surfaced in status so users can see at a glance which peers
// contribute to the merged output for this tenant.
type ContributingPeer struct {
	// Namespace of the peer CR.
	// +required
	Namespace string `json:"namespace"`

	// Name of the peer CR.
	// +required
	Name string `json:"name"`

	// Weight of the peer CR. Higher weight wins on conflict.
	// +required
	Weight int32 `json:"weight"`
}

// Condition types used on *TenantOverride.status.conditions.
const (
	// ConditionValidated is True when the CR's spec.overrides parsed strictly
	// into upstream Limits and passed Limits.Validate() — i.e. the CR is safe
	// to include in the merged output ConfigMap.
	ConditionValidated = "Validated"

	// ConditionApplied is True when the CR's contribution has been written
	// into the operator's output ConfigMap. False with reason=TargetDisabled
	// when the CR targets a backend that's disabled in operator config.
	ConditionApplied = "Applied"
)

// Reasons used on *TenantOverride.status.conditions. Kept to a small fixed
// vocabulary so they're alertable.
const (
	ReasonValidationSucceeded = "ValidationSucceeded"
	ReasonValidationFailed    = "ValidationFailed"
	ReasonWrittenToConfigMap  = "WrittenToConfigMap"
	ReasonTargetDisabled      = "TargetDisabled"
)
