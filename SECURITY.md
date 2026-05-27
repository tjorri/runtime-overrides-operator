# Security policy

## Reporting a vulnerability

**Please report security vulnerabilities through GitHub's private
vulnerability reporting, not by opening a public issue.**

The reporting form is at:

  https://github.com/tjorri/runtime-overrides-operator/security/advisories/new

This routes the report to the project maintainer privately, with a
collaboration thread inside GitHub for triage and fixes. Public
disclosure happens via the same workflow once a fix ships, with a CVE
requested if applicable.

If for some reason you cannot use GitHub's reporting form (rare;
unauthenticated users can submit), please open a regular GitHub issue
that only says "security: please contact me" and includes a way to
reach you — DO NOT include the vulnerability details in the public
issue. The maintainer will respond with a private channel.

## What's in scope

- The operator binary (Go code in this repo).
- The Helm chart (`deploy/charts/runtime-overrides-operator/`),
  including the bundled `ValidatingAdmissionPolicy` and the generated
  RBAC.
- The release artifacts published at:
  - `ghcr.io/tjorri/runtime-overrides-operator:vX.Y.Z` (signed via
    cosign keyless OIDC; verify with the command in each release's notes)
  - `ghcr.io/tjorri/charts/runtime-overrides-operator:X.Y.Z`

## What's out of scope

- Vulnerabilities in upstream `github.com/grafana/loki/v3/pkg/validation`
  or `github.com/grafana/mimir/pkg/util/validation` (these get pulled
  in via Go modules and live in their own repos — please report there).
- Vulnerabilities in cert-manager, the controller-runtime framework,
  or other transitive dependencies (please report upstream).
- Misuse of the bundled `ValidatingAdmissionPolicy` with a custom CEL
  expression that allows unintended cross-tenant overrides — that's a
  policy-authoring concern.

## Response timeline

This is a hobby project maintained by a single person. Best-effort
acknowledgement within 7 days. Fix timelines depend on severity and
maintainer availability; I'll communicate progress on the private
advisory thread.

For critical issues that need immediate disclosure to users (e.g.
RCE in the operator binary), I'll cut a patch release as soon as a
fix passes the test suite, and tag the GitHub Security Advisory.

## Supported versions

| Version | Supported |
|---------|-----------|
| v0.1.x (latest) | yes |
| Older | no — please upgrade |

While the project is pre-1.0, only the latest minor version receives
security fixes. Once 1.0 ships, the last two minor versions will be
supported.
