// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package validate

import (
	"bytes"
	"fmt"

	mimirvalidation "github.com/grafana/mimir/pkg/util/validation"
	yamlin "go.yaml.in/yaml/v3"
	yamlv3 "gopkg.in/yaml.v3"
)

// Verified against: github.com/grafana/mimir (see go.mod for the pinned pseudo-
// version). Bump the module in go.mod to pick up new upstream fields and
// validation rules; this is a no-API-change patch release of the operator.
//
// Mimir's Limits implements the go.yaml.in/yaml/v3 UnmarshalYAML signature
// (func(value *yaml.Node) error). We use yamlin (the maintained fork of
// yaml.v3 that Mimir itself uses) with KnownFields(true) for strict mode,
// mirroring how Mimir tests parse Limits.

type mimirValidator struct{}

func (mimirValidator) Target() Target { return TargetMimir }

func (mimirValidator) Validate(raw map[string]any) error {
	// Re-marshal the freeform map to YAML so we can feed it through Mimir's
	// strict YAML decoder. Marshaling via gopkg.in/yaml.v3 is fine — the
	// on-the-wire YAML is decoder-agnostic.
	body, err := yamlv3.Marshal(raw)
	if err != nil {
		return fmt.Errorf("mimir: re-marshal raw overrides: %w", err)
	}

	dec := yamlin.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)

	limits := mimirvalidation.Limits{}
	if err := dec.Decode(&limits); err != nil {
		return fmt.Errorf("mimir: %w", err)
	}

	if err := limits.Validate(); err != nil {
		return fmt.Errorf("mimir: %w", err)
	}
	return nil
}
