// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

// AGPL §13 belt-and-suspenders — the operator interacts with users
// remotely via its admission webhook and metrics endpoint, so we make
// the source URL trivially discoverable in three places:
//
//  1. The OCI image label org.opencontainers.image.source (set by .ko.yaml).
//  2. The Source-Code HTTP response header on every webhook reply,
//     injected by internal/webhook.WithSourceCodeHeader.
//  3. A startup log line written to stdout on first start (see writeBanner
//     below — main.go calls this before the manager starts).
//
// Plus the NOTICE file at the repo root.
const (
	sourceURL     = "https://github.com/tjorri/runtime-overrides-operator"
	licenseID     = "AGPL-3.0-only"
	licenseNotice = "AGPL-3.0-only (operator binary) / Apache-2.0 (Helm chart)"
)

// versionDev is the sentinel for untagged builds (`go run`, plain
// `go build` without ldflags). defaultVersion() upgrades it to a
// short vcs.revision when build info is available.
const versionDev = "dev"

// versionUnknown is the fallback when neither ldflags nor build info
// can supply a sensible version string.
const versionUnknown = "unknown"

// version is overridden at release-build time via
//
//	-ldflags="-X main.version=v0.1.0"
//
// `versionDev` is the default for `go run` and untagged builds.
var version = versionDev

// defaultVersion returns the linker-set version, or falls back to the
// vcs.revision from build info when ldflags wasn't applied.
func defaultVersion() string {
	if version != versionDev {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return versionDev
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return versionDev
}

// writeBanner prints a single-line startup banner identifying the binary,
// its source repository, and the upstream Loki/Mimir module versions
// the build was verified against. Picked up by the structured logger
// once setupLog is initialized; written directly to stdout otherwise.
//
// loki/mimir version strings come from build-info via debug.ReadBuildInfo,
// avoiding the need to thread ldflags through the build.
func writeBanner(w io.Writer, version string) {
	loki := moduleVersion("github.com/grafana/loki/v3")
	mimir := moduleVersion("github.com/grafana/mimir")
	fmt.Fprintf(w,
		"runtime-overrides-operator version=%s source=%s license=%s loki=%s mimir=%s\n",
		version, sourceURL, licenseID, loki, mimir,
	)
}

// moduleVersion looks up the version of a module that was linked into
// the binary. Returns "unknown" when build info isn't available
// (e.g. `go run` or some unit-test scenarios).
func moduleVersion(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return versionUnknown
	}
	for _, dep := range info.Deps {
		if dep == nil {
			continue
		}
		if dep.Path == path {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return versionUnknown
}
