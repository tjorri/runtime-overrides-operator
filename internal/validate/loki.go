// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package validate

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	lokivalidation "github.com/grafana/loki/v3/pkg/validation"
	lokiyaml "go.yaml.in/yaml/v4"
	yamlv3 "gopkg.in/yaml.v3"
)

// Verified against: github.com/grafana/loki/v3 (see go.mod for the pinned
// pseudo-version). Bump the module in go.mod to pick up new upstream fields and
// validation rules.
//
// Loki's Limits implements the go.yaml.in/yaml/v4 UnmarshalYAML signature
// (func(value *yaml.Node) error). We decode with lokiyaml (the yaml.v4 fork Loki
// itself uses), because Loki's custom UnmarshalYAML is what seeds the
// flag-registered defaults (deletion_mode, ingest-storage read consistency, ...)
// from SetDefaultLimitsForYAMLUnmarshalling. Decoding with a different YAML
// library silently skips that method, leaving enum fields at their invalid zero
// value and failing Validate().
//
// Strict unknown-field rejection is enforced by us, not the decoder. Loki
// migrated Limits from yaml.v2 to yaml.v4 after v3.7.2; its UnmarshalYAML uses
// value.Decode (not node.Load(..., WithKnownFields())), so a decoder-level
// KnownFields(true) no longer rejects unknown override keys the way yaml.v2's
// UnmarshalStrict did. We restore that typo-protection with lokiKnownFields
// below. (Mimir still gets strict checking from upstream — it is on the grafana
// go-yaml/v3 fork, which threads KnownFields through nested Decode.) This covers
// top-level keys, which is what tenant runtime overrides are in practice; an
// unknown key nested inside a struct-valued override is not caught here.

type lokiValidator struct{}

func (lokiValidator) Target() Target { return TargetLoki }

func (lokiValidator) Validate(raw map[string]any) error {
	// Reject unknown top-level override keys before parsing (see package note
	// above on why the decoder can't do this for Loki anymore).
	for k := range raw {
		if _, ok := lokiKnownFields[k]; !ok {
			return fmt.Errorf("loki: field %q not found in type validation.Limits", k)
		}
	}

	// Re-marshal the freeform map to YAML so we can feed it through Loki's
	// YAML decoder. Marshaling via gopkg.in/yaml.v3 is fine — the on-the-wire
	// YAML is decoder-agnostic.
	body, err := yamlv3.Marshal(raw)
	if err != nil {
		return fmt.Errorf("loki: re-marshal raw overrides: %w", err)
	}

	dec := lokiyaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)

	limits := lokivalidation.Limits{}
	if err := dec.Decode(&limits); err != nil {
		return fmt.Errorf("loki: %w", err)
	}

	if err := limits.Validate(); err != nil {
		return fmt.Errorf("loki: %w", err)
	}
	return nil
}

// lokiKnownFields is the set of top-level YAML keys accepted by
// lokivalidation.Limits, derived from its struct tags. Built once at package
// load and read-only thereafter, so it tracks upstream automatically when the
// module is bumped.
var lokiKnownFields = knownYAMLFields(reflect.TypeOf(lokivalidation.Limits{}))

// knownYAMLFields returns the set of YAML field names a struct accepts,
// recursing through ",inline" embedded structs. It follows the same tag
// semantics as the YAML decoder: a "-" name is skipped and an untagged exported
// field falls back to its lowercased Go name.
func knownYAMLFields(t reflect.Type) map[string]struct{} {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	out := make(map[string]struct{})
	if t.Kind() != reflect.Struct {
		return out
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if strings.Contains(opts, "inline") {
			for k := range knownYAMLFields(f.Type) {
				out[k] = struct{}{}
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		out[name] = struct{}{}
	}
	return out
}
