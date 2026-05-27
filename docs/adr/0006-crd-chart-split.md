# ADR 0006: Publish a separate CRD-only Helm chart alongside the operator chart

**Status:** Accepted (2026-05-27)

## Decision

Ship two Helm charts on every release, versioned in lockstep:

- `runtime-overrides-operator` (Apache-2.0) — the operator chart, as before.
  Its CRD installation is gated by a new value `crds.install` which
  defaults to `true` for backward compatibility.
- `runtime-overrides-operator-crds` (Apache-2.0, new in v0.2.0) — installs
  only the two `LokiTenantOverride` and `MimirTenantOverride` CRDs as
  Helm release resources.

Plus a third install path: every release attaches a single
`crds.yaml` asset to the GitHub Release for users who want to manage
CRDs with raw `kubectl apply` instead of Helm.

## Rationale

Helm 3 has a well-known limitation around CRD lifecycle. CRDs installed
via:

- the special `crds/` chart directory (Helm's official "bundle CRDs"
  convention), or
- `templates/crds/` *without* explicit `helm.sh/hook` annotations (which
  is what we did in v0.1.0)

…are applied on `helm install` but **not updated on `helm upgrade`**.
Helm's design rationale is that CRD schema changes are risky and should
be deliberate. The practical consequence is that any CRD-only schema
change (new optional fields, validation tightening, new printer columns)
never reaches a cluster that was installed via `helm install` and only
ever upgraded via `helm upgrade`.

Two patterns are well-established for resolving this:

1. **A dedicated CRD chart.** Install the CRDs as Helm release
   resources of *their own* chart. `helm upgrade <crd-release>`
   reconciles changes normally. This is what cert-manager, Linkerd,
   and recent kube-prometheus-stack versions do.
2. **Raw CRD YAML.** Skip Helm entirely for CRDs. Apply with `kubectl
   apply -f` (often automated via Argo CD, Crossplane, or a Makefile).

We ship both. The operator chart retains its `crds.install: true`
default so a `helm install` of the operator chart alone Just Works for
proofs of concept; production users opt in to pattern (1) or (2) by
setting `crds.install: false` and installing the CRDs out-of-band.

## Why lockstep versions

The CRD schemas and the operator binary that consumes them ship from
the same source tree (`api/v1alpha1/*_types.go` defines both the Go
structs and the controller-gen-generated CRD YAML). Letting the chart
versions drift would let a user pair v0.2.0 operator with v0.1.0 CRDs,
which we have no way to validate. Lockstep removes the variable.

release-please tracks both `Chart.yaml` versions plus both `appVersion`
fields as `extra-files` of the same `.` package (see
`release-please-config.json`), so a single release PR bumps everything
in one commit.

## Why not strip CRDs from the operator chart entirely

We considered three options for the operator chart's relationship to
CRDs:

- **A.** Strip CRDs from the operator chart. Users *must* install the
  CRD chart (or apply the YAML) separately.
- **B.** Keep CRDs in the operator chart behind a values toggle, default
  `true` for backward compat. (Adopted.)
- **C.** Leave the operator chart unchanged, add the CRD chart as a pure
  addition. Users installing both get conflicts on the same CRDs.

Option B preserves the single-command install — important for first-
contact UX and for the e2e suite, which would otherwise need two helm
installs. Users who care about upgrade lifecycle pay one extra flag
(`--set crds.install=false`); users who don't, don't.

## Consequences

- The release pipeline now packages, pushes, signs (cosign keyless),
  and attests (SLSA build-provenance) **two** Helm charts per release.
  Same supply-chain story as the operator chart and image, applied to
  the CRD chart symmetrically.
- The release also generates and attaches `crds.yaml` to the GitHub
  Release as a per-release asset. The aggregated file is two CRDs
  separated by a `---` document break — drop-in to `kubectl apply -f`.
- The Makefile's `chart-sync` target now (a) wraps the main chart's
  CRDs with `{{- if .Values.crds.install }}` ... `{{- end }}` during
  the sync, (b) copies the bare CRD YAML into the CRD chart's
  `templates/`, and (c) writes the aggregated `dist/crds.yaml`. All
  three artifacts stay in sync from a single `controller-gen` run.
- The Helm-managed CRDs in both charts carry
  `helm.sh/resource-policy: keep` (injected by `chart-sync` into the
  controller-gen-emitted `metadata.annotations` block). This is the
  load-bearing safety net for the lifecycle story above: without it,
  `helm upgrade --set crds.install=false` (the migration step from the
  bundled-CRDs install to the dedicated CRD chart) would remove the
  CRDs from the chart's manifest tracking and Helm would then DELETE
  them from the cluster, which cascade-deletes every `LokiTenantOverride`
  / `MimirTenantOverride` CR. The annotation tells Helm to leave the
  CRDs in place on resource-removal and on `helm uninstall`; it does
  NOT affect `helm upgrade`'s normal update behavior, so CRD schema
  changes still propagate. Users who explicitly want the CRDs gone
  use `kubectl delete crd` directly. The aggregated `dist/crds.yaml`
  release asset does not get the annotation because it isn't Helm-
  managed. This matches the convention used by cert-manager-crds,
  kube-prometheus-stack, Linkerd, and others.
- `make chart-lint` runs `helm lint` + `helm template` against both
  charts and runs `helm unittest` against both. The chart-unittest
  suite for the new CRD chart covers the two CRDs' identity (kind,
  group, scope, name); the operator chart gains a test pinning the
  `crds.install=false` path emits zero CRDs.
- The chart README in the operator chart explains the three install
  patterns. The CRD chart's README points back to this ADR for the
  full reasoning.

## References

- Helm 3 CRD lifecycle limitation:
  <https://helm.sh/docs/chart_best_practices/custom_resource_definitions/>
- cert-manager's CRD chart split:
  <https://github.com/cert-manager/cert-manager/pull/4646>
- The "managing CRDs with Helm" discussion thread that motivated most
  projects to adopt this pattern:
  <https://github.com/helm/helm/issues/9351>
