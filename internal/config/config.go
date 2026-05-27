// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// Package config carries the operator's runtime configuration — per-target
// output ConfigMap coordinates and enable flags. Loaded from CLI flags in
// cmd/main.go.
package config

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

// Targets holds per-backend configuration.
type Targets struct {
	Loki  TargetConfig
	Mimir TargetConfig
}

// TargetConfig describes one backend (Loki or Mimir). Enabled gates the
// reconciler registration; OutputCM is the ConfigMap the reconciler owns.
//
// v1 supports exactly one Loki + one Mimir target per operator deployment.
// Multi-instance setups run multiple Helm releases.
type TargetConfig struct {
	Enabled  bool
	OutputCM types.NamespacedName
}

// Validate sanity-checks the resolved configuration.
func (t Targets) Validate() error {
	if t.Loki.Enabled {
		if err := t.Loki.validate("loki"); err != nil {
			return err
		}
	}
	if t.Mimir.Enabled {
		if err := t.Mimir.validate("mimir"); err != nil {
			return err
		}
	}
	if !t.Loki.Enabled && !t.Mimir.Enabled {
		return fmt.Errorf("at least one of --loki-enabled or --mimir-enabled must be true")
	}
	return nil
}

func (tc TargetConfig) validate(name string) error {
	if tc.OutputCM.Name == "" {
		return fmt.Errorf("%s: output ConfigMap name is required when enabled", name)
	}
	if tc.OutputCM.Namespace == "" {
		return fmt.Errorf("%s: output ConfigMap namespace is required when enabled", name)
	}
	return nil
}
