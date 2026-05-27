// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// Package webhook holds HTTP middleware used by the operator's
// validating webhooks. The middleware is registered around the
// webhook handlers in internal/webhook/v1alpha1 by the manual
// setup path so AGPL §13 compliance hooks are attached to every
// admission response.
package webhook

import "net/http"

// SourceURL is the upstream source repository for runtime-overrides-operator.
// Exposed publicly so cmd/banner.go and the middleware can share it.
const SourceURL = "https://github.com/tjorri/runtime-overrides-operator"

// SourceCodeHeader is the HTTP response header name carrying the
// upstream source URL on every webhook response. AGPL §13 belt-and-
// suspenders alongside the OCI image label and the startup banner
// in cmd/banner.go.
const SourceCodeHeader = "Source-Code"

// WithSourceCodeHeader wraps any http.Handler to inject the
// Source-Code response header. The header is set BEFORE next.ServeHTTP
// so it survives even when the inner handler returns an error or panics
// without writing a body — the API server's audit log captures it
// regardless.
func WithSourceCodeHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(SourceCodeHeader, SourceURL)
		next.ServeHTTP(w, r)
	})
}
