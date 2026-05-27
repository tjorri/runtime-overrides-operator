// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	rov1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
	"github.com/tjorri/runtime-overrides-operator/internal/validate"
	whmiddleware "github.com/tjorri/runtime-overrides-operator/internal/webhook"
)

var mimirtenantoverridelog = logf.Log.WithName("mimirtenantoverride-resource")

// MimirTenantOverrideValidatePath mirrors LokiTenantOverrideValidatePath
// in the Loki webhook file. See the comment there.
const MimirTenantOverrideValidatePath = "/validate-runtimeoverrides-io-v1alpha1-mimirtenantoverride"

// SetupMimirTenantOverrideWebhookWithManager registers the validating webhook
// for MimirTenantOverride. See lokitenantoverride_webhook.go for the
// failurePolicy=Ignore rationale and the manual-registration-via-middleware
// pattern used here.
func SetupMimirTenantOverrideWebhookWithManager(mgr ctrl.Manager) error {
	// See lokitenantoverride_webhook.go for the WithCustomValidator
	// deprecation note — migrate when controller-runtime v0.24.2+ ships.
	wh := admission.WithCustomValidator( //nolint:staticcheck // SA1019: see comment above
		mgr.GetScheme(),
		&rov1alpha1.MimirTenantOverride{},
		&MimirTenantOverrideCustomValidator{
			Validator: validate.New(validate.TargetMimir),
		},
	)
	mgr.GetWebhookServer().Register(
		MimirTenantOverrideValidatePath,
		whmiddleware.WithSourceCodeHeader(wh),
	)
	return nil
}

// +kubebuilder:webhook:path=/validate-runtimeoverrides-io-v1alpha1-mimirtenantoverride,mutating=false,failurePolicy=ignore,sideEffects=None,groups=runtimeoverrides.io,resources=mimirtenantoverrides,verbs=create;update,versions=v1alpha1,name=vmimirtenantoverride-v1alpha1.kb.io,admissionReviewVersions=v1,timeoutSeconds=5

// MimirTenantOverrideCustomValidator runs upstream Mimir Limits.Validate() on
// create/update. failurePolicy=Ignore at the registration site; Layer 3 is
// the load-bearing safety net.
type MimirTenantOverrideCustomValidator struct {
	Validator validate.Validator
}

var _ webhook.CustomValidator = &MimirTenantOverrideCustomValidator{} //nolint:staticcheck

func (v *MimirTenantOverrideCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	cr, ok := obj.(*rov1alpha1.MimirTenantOverride)
	if !ok {
		return nil, fmt.Errorf("expected a MimirTenantOverride object but got %T", obj)
	}
	mimirtenantoverridelog.V(1).Info("validate create",
		"name", cr.GetName(), "namespace", cr.GetNamespace())
	return nil, v.validateSpec(cr)
}

func (v *MimirTenantOverrideCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	cr, ok := newObj.(*rov1alpha1.MimirTenantOverride)
	if !ok {
		return nil, fmt.Errorf("expected a MimirTenantOverride object for the newObj but got %T", newObj)
	}
	mimirtenantoverridelog.V(1).Info("validate update",
		"name", cr.GetName(), "namespace", cr.GetNamespace())
	return nil, v.validateSpec(cr)
}

func (v *MimirTenantOverrideCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *MimirTenantOverrideCustomValidator) validateSpec(cr *rov1alpha1.MimirTenantOverride) error {
	if v.Validator == nil {
		return fmt.Errorf("internal: validator not initialized")
	}
	raw, err := decodeOverrides(cr.Spec.Overrides)
	if err != nil {
		return fmt.Errorf("decode spec.overrides: %w", err)
	}
	return v.Validator.Validate(raw)
}
