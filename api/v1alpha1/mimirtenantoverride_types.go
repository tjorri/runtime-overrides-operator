// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// MimirTenantOverrideSpec defines a per-tenant override contribution for Mimir.
// The operator deep-merges Spec.Overrides into the tenant's stanza of the
// output ConfigMap.
type MimirTenantOverrideSpec struct {
	// TenantId this override applies to. Defaults to metadata.namespace
	// when empty. The bundled ValidatingAdmissionPolicy (shipped in the
	// Helm chart) restricts cross-namespace tenantId by default.
	// +optional
	// +kubebuilder:validation:MaxLength=250
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	TenantId string `json:"tenantId,omitempty"`

	// Weight is the precedence weight on conflict. Higher wins. Same-weight
	// collisions are broken by lexical (namespace, name). Negative values
	// are allowed for "below baseline" platform-default profiles.
	// +optional
	// +kubebuilder:default=0
	Weight int32 `json:"weight,omitempty"`

	// Overrides is the freeform map deep-merged into the tenant's stanza
	// of the merged output ConfigMap. Schema is preserve-unknown-fields;
	// validation against Mimir's upstream Limits happens in the admission
	// webhook and in the controller.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	// +required
	Overrides *runtime.RawExtension `json:"overrides"`
}

// MimirTenantOverrideStatus reflects the controller's view of this CR.
type MimirTenantOverrideStatus struct {
	// ObservedGeneration is the most recent metadata.generation observed
	// by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// EffectiveTenantId is the tenantId actually used (Spec.TenantId or
	// the defaulted-from-namespace value).
	// +optional
	EffectiveTenantId string `json:"effectiveTenantId,omitempty"`

	// Conditions: see common_types.go for the type/reason vocabulary.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ContributingPeers lists other MimirTenantOverride CRs that target
	// the same tenantId.
	// +optional
	ContributingPeers []ContributingPeer `json:"contributingPeers,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mto
// +kubebuilder:printcolumn:name="TenantID",type=string,JSONPath=`.status.effectiveTenantId`
// +kubebuilder:printcolumn:name="Weight",type=integer,JSONPath=`.spec.weight`
// +kubebuilder:printcolumn:name="Validated",type=string,JSONPath=`.status.conditions[?(@.type=="Validated")].status`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MimirTenantOverride is a per-tenant runtime override contribution for Mimir.
type MimirTenantOverride struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of MimirTenantOverride
	// +required
	Spec MimirTenantOverrideSpec `json:"spec"`

	// status defines the observed state of MimirTenantOverride
	// +optional
	Status MimirTenantOverrideStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MimirTenantOverrideList contains a list of MimirTenantOverride
type MimirTenantOverrideList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MimirTenantOverride `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MimirTenantOverride{}, &MimirTenantOverrideList{})
}
