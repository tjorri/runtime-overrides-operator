// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package v1alpha1

import (
	"context"
	"encoding/json"
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

// LokiTenantOverrideValidatePath is the URL path of the Loki validating
// webhook. Centralized here so both the kubebuilder marker (just below)
// and the manual registration in SetupLokiTenantOverrideWebhookWithManager
// agree, and so the e2e/integration tests can reach it predictably.
const LokiTenantOverrideValidatePath = "/validate-runtimeoverrides-io-v1alpha1-lokitenantoverride"

// lokitenantoverridelog is for logging in this package.
var lokitenantoverridelog = logf.Log.WithName("lokitenantoverride-resource")

// SetupLokiTenantOverrideWebhookWithManager registers the validating webhook
// for LokiTenantOverride with the manager. The validator runs upstream
// loki/pkg/validation.Limits.Validate() against each CR's spec.overrides.
// failurePolicy=Ignore (set in the kubebuilder marker below) means a webhook
// outage does NOT block CR writes — controller-side validation (Layer 3)
// is the load-bearing safety net. See docs/adr/0001-webhook-failure-policy-ignore.md.
// We build the admission.Webhook manually (instead of using
// ctrl.NewWebhookManagedBy(...).Complete()) so the http.Handler chain
// passes through whmiddleware.WithSourceCodeHeader before reaching the
// admission logic. That middleware injects the AGPL §13 Source-Code
// response header on every webhook reply.
func SetupLokiTenantOverrideWebhookWithManager(mgr ctrl.Manager) error {
	// Note: admission.WithCustomValidator is marked deprecated in favor of
	// the generic admission.WithValidator[T], but the generic form is only
	// in controller-runtime main — not yet in a tagged release. Revisit
	// once v0.24.2 (or whatever cuts next) ships.
	wh := admission.WithCustomValidator( //nolint:staticcheck // SA1019: see comment above
		mgr.GetScheme(),
		&rov1alpha1.LokiTenantOverride{},
		&LokiTenantOverrideCustomValidator{
			Validator: validate.New(validate.TargetLoki),
		},
	)
	mgr.GetWebhookServer().Register(
		LokiTenantOverrideValidatePath,
		whmiddleware.WithSourceCodeHeader(wh),
	)
	return nil
}

// +kubebuilder:webhook:path=/validate-runtimeoverrides-io-v1alpha1-lokitenantoverride,mutating=false,failurePolicy=ignore,sideEffects=None,groups=runtimeoverrides.io,resources=lokitenantoverrides,verbs=create;update,versions=v1alpha1,name=vlokitenantoverride-v1alpha1.kb.io,admissionReviewVersions=v1,timeoutSeconds=5

// LokiTenantOverrideCustomValidator runs upstream Loki Limits.Validate() on
// create/update. failurePolicy=Ignore at the registration site, so the
// load-bearing safety guarantee remains Layer 3 (controller-side).
type LokiTenantOverrideCustomValidator struct {
	Validator validate.Validator
}

// CustomValidator is deprecated alongside WithCustomValidator (see above);
// migrate to admission.Validator[T] when controller-runtime ships it tagged.
var _ webhook.CustomValidator = &LokiTenantOverrideCustomValidator{} //nolint:staticcheck

// ValidateCreate runs upstream Validate() on the CR's spec.overrides.
func (v *LokiTenantOverrideCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	cr, ok := obj.(*rov1alpha1.LokiTenantOverride)
	if !ok {
		return nil, fmt.Errorf("expected a LokiTenantOverride object but got %T", obj)
	}
	lokitenantoverridelog.V(1).Info("validate create",
		"name", cr.GetName(), "namespace", cr.GetNamespace())
	return nil, v.validateSpec(cr)
}

// ValidateUpdate runs upstream Validate() on the new spec.overrides.
// We don't compare against oldObj — same-CR transitions are still gated by
// "must parse strictly and pass Validate()".
func (v *LokiTenantOverrideCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	cr, ok := newObj.(*rov1alpha1.LokiTenantOverride)
	if !ok {
		return nil, fmt.Errorf("expected a LokiTenantOverride object for the newObj but got %T", newObj)
	}
	lokitenantoverridelog.V(1).Info("validate update",
		"name", cr.GetName(), "namespace", cr.GetNamespace())
	return nil, v.validateSpec(cr)
}

// ValidateDelete is a no-op — we never reject deletions.
func (v *LokiTenantOverrideCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// validateSpec decodes spec.overrides into a freeform map and runs the
// upstream Loki Limits.Validate(). Errors are returned verbatim from
// the upstream package (wrapped via fmt.Errorf in the validator).
func (v *LokiTenantOverrideCustomValidator) validateSpec(cr *rov1alpha1.LokiTenantOverride) error {
	if v.Validator == nil {
		return fmt.Errorf("internal: validator not initialized")
	}
	raw, err := decodeOverrides(cr.Spec.Overrides)
	if err != nil {
		return fmt.Errorf("decode spec.overrides: %w", err)
	}
	return v.Validator.Validate(raw)
}

// decodeOverrides is the webhook-side counterpart of the reconciler's
// unmarshalRawExtension; kept here so the webhook package has no import
// of the controller package.
func decodeOverrides(raw *runtime.RawExtension) (map[string]any, error) {
	if raw == nil || len(raw.Raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw.Raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}
