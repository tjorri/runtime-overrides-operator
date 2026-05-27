// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// Package v1alpha1 contains API Schema definitions for the runtimeoverrides v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=runtimeoverrides.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "runtimeoverrides.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	//
	// SA1019: controller-runtime's scheme.Builder is soft-deprecated in
	// favor of apimachinery's runtime.SchemeBuilder so api packages have
	// fewer dependencies. The migration changes the init() pattern in
	// *_types.go (Register vs. AddKnownTypes) — defer until the next
	// breaking-API cleanup pass.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion} //nolint:staticcheck

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
