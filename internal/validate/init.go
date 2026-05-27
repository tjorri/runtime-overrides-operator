// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package validate

import (
	"flag"
	"sync"

	lokivalidation "github.com/grafana/loki/v3/pkg/validation"
	mimirvalidation "github.com/grafana/mimir/pkg/util/validation"
)

// InitDefaults primes the package-level Limits-defaults globals that Loki's
// and Mimir's Limits.UnmarshalYAML consume via SetDefaultLimitsForYAMLUnmarshalling.
//
// IMPORTANT: both upstream functions write to a package-level
// global. Calling them per-request from concurrent webhook handlers is a Go
// memory-model race. We must call InitDefaults exactly once at controller
// startup before mgr.Start() and before any Validate() call. The sync.Once
// is belt-and-suspenders against accidental re-init from tests or other
// callers; the real contract is "call once at startup, never again."
//
// The defaults must come from upstream's RegisterFlags machinery, not from
// a zero-value Limits struct. Several fields (deletion mode, ingest storage
// read consistency, ...) have enum-style validation rules that reject the
// zero value; only the flag-registered defaults pass Validate(). This mirrors
// what Loki/Mimir do at startup themselves.
func InitDefaults() {
	initOnce.Do(func() {
		var lokiDefaults lokivalidation.Limits
		lokiDefaults.RegisterFlags(flag.NewFlagSet("loki-defaults", flag.PanicOnError))
		lokivalidation.SetDefaultLimitsForYAMLUnmarshalling(lokiDefaults)

		var mimirDefaults mimirvalidation.Limits
		mimirDefaults.RegisterFlags(flag.NewFlagSet("mimir-defaults", flag.PanicOnError))
		mimirvalidation.SetDefaultLimitsForYAMLUnmarshalling(mimirDefaults)
	})
}

var initOnce sync.Once
