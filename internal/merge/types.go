// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// Package merge contains the pure deep-merge engine used by the
// reconciler: deep-merge of per-tenant override maps with provenance
// tracking, deterministic YAML rendering, and per-field conflict
// surfacing. It is decoupled from Kubernetes types (no client-go,
// no api/v1alpha1 imports) so it can be tested as plain Go.
package merge

// Override is the merge-engine-internal representation of a *TenantOverride
// CR. The reconciler builds these from LokiTenantOverride / MimirTenantOverride
// before passing them to the merge engine, so this package stays decoupled
// from the API types.
type Override struct {
	// Namespace and Name identify the source CR. Used for sort tie-break
	// and for provenance reporting on conflicts.
	Namespace string
	Name      string

	// TenantId is the effective tenant ID this override targets
	// (Spec.TenantId or the defaulted-from-namespace value).
	TenantId string

	// Weight is the precedence weight. Higher wins on conflict; same-weight
	// ties are broken lexically by (Namespace, Name).
	Weight int32

	// Overrides is the freeform map of override key/values to deep-merge
	// into the tenant's stanza of the output ConfigMap. Owned by the caller;
	// the merge engine does not retain references after returning.
	Overrides map[string]any
}

// CRRef is a compact reference to a CR for provenance reporting.
type CRRef struct {
	Namespace string
	Name      string
	Weight    int32
}

// refOf builds a CRRef from an Override.
func refOf(o Override) CRRef {
	return CRRef{Namespace: o.Namespace, Name: o.Name, Weight: o.Weight}
}
