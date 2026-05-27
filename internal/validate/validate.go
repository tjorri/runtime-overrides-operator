// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// Package validate runs upstream Loki and Mimir Limits.UnmarshalYAML +
// Validate() against per-tenant override maps. By importing the upstream
// validation packages directly (which is what makes the operator binary
// AGPL-3.0, the validation behavior is identical to what
// Loki and Mimir run themselves at startup — single source of truth, zero
// drift across upstream releases.
// Hazard: both upstream packages use a package-level global set by
// SetDefaultLimitsForYAMLUnmarshalling (consumed by Limits.UnmarshalYAML).
// We call InitDefaults() exactly once at controller startup; concurrent
// validation calls thereafter are read-only on that global and safe.
package validate

// Target identifies which upstream validator to use.
type Target string

const (
	TargetLoki  Target = "loki"
	TargetMimir Target = "mimir"
)

// Validator validates a raw per-tenant overrides map against the upstream
// Limits schema for a specific backend. Implementations are stateless after
// InitDefaults() has been called once.
type Validator interface {
	// Validate parses the raw map strictly as the upstream Limits type and
	// runs its Validate() method. Returns nil if the map represents a valid
	// override for this backend; returns an error wrapping the upstream
	// error verbatim otherwise.
	Validate(raw map[string]any) error

	// Target returns the backend this validator targets.
	Target() Target
}

// New returns a Validator for the given target, or nil for an unknown target.
func New(t Target) Validator {
	switch t {
	case TargetLoki:
		return lokiValidator{}
	case TargetMimir:
		return mimirValidator{}
	default:
		return nil
	}
}
