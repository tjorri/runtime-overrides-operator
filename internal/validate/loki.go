// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package validate

import (
	"fmt"

	lokivalidation "github.com/grafana/loki/v3/pkg/validation"
	yamlv2 "gopkg.in/yaml.v2"
	yamlv3 "gopkg.in/yaml.v3"
)

// Verified against: github.com/grafana/loki/v3 v3.7.2 (see go.mod).
// Bump the module in go.mod to pick up new upstream fields and validation
// rules; this is a no-API-change patch release of the operator.
//
// Loki's Limits implements the gopkg.in/yaml.v2 UnmarshalYAML signature
// (func(unmarshal func(interface{}) error) error), so we drive parsing with
// yaml.v2's UnmarshalStrict — that mirrors what Loki itself does in its tests
// at pkg/validation/limits_test.go and what its config loader does at startup.

type lokiValidator struct{}

func (lokiValidator) Target() Target { return TargetLoki }

func (lokiValidator) Validate(raw map[string]any) error {
	// Re-marshal the freeform map to YAML so we can feed it through Loki's
	// strict YAML decoder. yaml.v3 marshal -> yaml.v2 unmarshal is fine
	// (both consume the same on-the-wire YAML).
	body, err := yamlv3.Marshal(raw)
	if err != nil {
		return fmt.Errorf("loki: re-marshal raw overrides: %w", err)
	}

	limits := lokivalidation.Limits{}
	if err := yamlv2.UnmarshalStrict(body, &limits); err != nil {
		return fmt.Errorf("loki: %w", err)
	}

	if err := limits.Validate(); err != nil {
		return fmt.Errorf("loki: %w", err)
	}
	return nil
}
