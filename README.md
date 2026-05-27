# runtime-overrides-operator

A Kubernetes operator that assembles per-tenant runtime config overrides
for Grafana **Loki** and **Mimir** from declarative custom resources.

It lets you author tenant limits anywhere in the cluster — typically in
the tenant's own namespace, by Crossplane Compositions, by GitOps
tooling, or by humans with `kubectl` — without coupling to whoever owns
the Loki/Mimir install. The operator writes one output ConfigMap per
backend; you mount that ConfigMap into Loki/Mimir as one of multiple
`runtime_config.file` entries, and dskit's built-in 10-second runtime
reload picks up the changes without restarting any pods.

> **Status: pre-alpha (v0.1.0).** API is `runtimeoverrides.io/v1alpha1`
> and may change. See [`docs/adr/`](docs/adr/) for the load-bearing
> design decisions.

## Why

Loki and Mimir support per-tenant runtime overrides via a single YAML
file polled by dskit every ~10s. In practice that file is templated
and owned by whatever installed Loki/Mimir — a Helm release, a
Terraform module, an Argo CD application. That's fine when tenants are
static; it breaks down when tenants come and go dynamically (Crossplane
Compositions, per-PR ephemeral environments, ad-hoc SRE escape hatches
during incidents).

This operator takes per-tenant overrides as CRs and assembles them into
an output ConfigMap that the Loki/Mimir installer mounts alongside its
own base file. Two distinct writers, two distinct ConfigMaps, zero
contention. Multiple CRs can contribute to the same tenant with
explicit weight-based precedence.

## Quick start

### Prerequisites

- Kubernetes 1.30+ (for the bundled `ValidatingAdmissionPolicy`)
- [cert-manager](https://cert-manager.io) installed (for the webhook's
  TLS cert; set `operator.webhook.enabled=false` to skip)
- Helm 3.8+ (for OCI registry support)

### Install

Three patterns, pick the one that matches how you manage CRD lifecycle.
See [ADR 0006](docs/adr/0006-crd-chart-split.md) for the rationale.

**A. One-command (CRDs bundled with the operator chart).**
Easiest for first install. `helm upgrade` will NOT update the CRDs
later — fine for proofs of concept, consider B or C for production.

```sh
helm install ro-op oci://ghcr.io/tjorri/charts/runtime-overrides-operator \
  --version 0.1.0 \
  --namespace runtime-overrides-system --create-namespace
```

**B. Separate CRD chart (recommended for production).**
`helm upgrade` on each chart applies its respective resources, including
CRD schema changes. Charts are versioned in lockstep.

```sh
helm install ro-op-crds oci://ghcr.io/tjorri/charts/runtime-overrides-operator-crds \
  --version 0.1.0
helm install ro-op oci://ghcr.io/tjorri/charts/runtime-overrides-operator \
  --version 0.1.0 \
  --namespace runtime-overrides-system --create-namespace \
  --set crds.install=false
```

**C. Raw CRD manifest (CRDs managed outside Helm).**
Apply the per-release aggregated CRD YAML directly. Use this if your
platform pins CRD ownership outside Helm (Argo CD, Crossplane, etc).

```sh
kubectl apply -f https://github.com/tjorri/runtime-overrides-operator/releases/download/v0.1.0/crds.yaml
helm install ro-op oci://ghcr.io/tjorri/charts/runtime-overrides-operator \
  --version 0.1.0 \
  --namespace runtime-overrides-system --create-namespace \
  --set crds.install=false
```

All three patterns default to the Loki reconciler with output ConfigMap
`monitoring/loki-runtime-tenants`. To enable Mimir as well, pass:

```sh
  --set operator.targets.mimir.enabled=true
```

### Wire it into Loki/Mimir

The Loki/Mimir installer must mount the operator's output ConfigMap
as a directory (not subPath — those don't auto-update) and list its
file path **rightmost** in `runtime_config.file`. Snippets:

- [grafana/loki Helm chart](docs/integration/loki-helm.md) — SimpleScalable, SingleBinary, Distributed
- [grafana/mimir-distributed Helm chart](docs/integration/mimir-helm.md)
- [grafana loki-operator LokiStack](docs/integration/loki-operator-lokistack.md)
- [kustomize](docs/integration/kustomize.md)

### Author overrides

```yaml
apiVersion: runtimeoverrides.io/v1alpha1
kind: LokiTenantOverride
metadata:
  name: boost
  namespace: acme
spec:
  weight: 100
  overrides:
    ingestion_rate_mb: 32
    ingestion_burst_size_mb: 64
    per_stream_rate_limit: "10MB"
```

`spec.tenantId` defaults to the namespace. End-to-end p99 propagation is
~90 seconds (kubelet ConfigMap sync ≤60s + dskit's 10s reload poll).

### Verify integration once

Loki and Mimir official images are distroless — no `sh`, no `curl`, no
`wget` — so `kubectl exec` isn't an option. Port-forward and probe from
your own shell instead:

```sh
kubectl -n loki port-forward deploy/loki 3100:3100 &
curl -s http://localhost:3100/runtime_config | yq '.overrides'
```

Apply a CR, re-run the command, see the tenant block appear. The operator
itself does **not** poll Loki/Mimir; verification is a one-time human
step at install time.

## How it works

```
LokiTenantOverride / MimirTenantOverride CRs (any namespace)
                              │
                              ▼
       Validating Admission Webhook (failurePolicy: Ignore — ADR 0001)
       Bundled ValidatingAdmissionPolicy (namespace ↔ tenant ownership)
                              │
                              ▼
            runtime-overrides-operator reconciler
              • watches CRs cluster-wide
              • runs upstream Loki/Mimir Limits.Validate()
                (Layer 3 — load-bearing safety net)
              • groups by tenantId, sorts by (weight, ns, name)
              • deep-merges overrides; later wins on conflict
              • writes one output ConfigMap per target via SSA
              • watches own output CM; reverts third-party writes
              │
              ▼
        loki-runtime-tenants            mimir-runtime-tenants
        (your installer mounts          (your installer mounts
         this rightmost in              this rightmost in
         runtime_config.file)           runtime_config.file)
              │                                  │
              └─── Loki / Mimir reload via dskit's 10s poll ──┘
                       (no pod restarts)
```

Validation runs upstream `Limits.Validate()` directly — same code path
Loki and Mimir run themselves at startup. That's what makes the
operator binary AGPL-3.0 ([ADR 0002](docs/adr/0002-agpl-binary-apache-chart.md)).

## Licensing

| Artifact | License |
|----------|---------|
| Operator binary + container image (`ghcr.io/tjorri/runtime-overrides-operator`) | **AGPL-3.0-only** ([`LICENSE`](LICENSE)) |
| Helm chart (`deploy/charts/runtime-overrides-operator/`) | **Apache-2.0** (chart-local LICENSE) |

See [`NOTICE`](NOTICE) for the rationale, upstream attribution, and the
three-place AGPL §13 source-availability statement. Operating an
unmodified release: nothing beyond pulling the image.

## Supported upstream versions

| Component | v0.1.0 verified against |
|---|---|
| Kubernetes  | 1.30 – 1.33 |
| Grafana Loki | v3.7.x (module `github.com/grafana/loki/v3 v3.7.2`) |
| Grafana Mimir | v1.3.x (module `github.com/grafana/mimir v1.3.1-pre`) |
| cert-manager | v1.14+ |
| Helm | 3.8+ (OCI registry support) |

Each release pins specific Loki and Mimir Go module versions in
[`go.mod`](go.mod) — that's how the operator picks up new upstream
fields and new validation rules. The compatibility window is "the
same upstream release line we vendor against." Bumping is automated
via Renovate ([`renovate.json`](renovate.json)) on a weekly schedule;
each bump PR runs the full KinD e2e against the new versions before
merge.

To see the exact upstream versions a running operator was built
against, look at its startup banner — the first stdout line after pod
start records `loki=...` and `mimir=...`:

```sh
kubectl logs -n runtime-overrides-system \
  deploy/ro-op-runtime-overrides-operator | head -1
```

## Operate

- **Metrics**: `runtime_overrides_*` family on `:8443/metrics`. Includes
  reconcile counts, validation errors, active tenants/CRs, drift events
  (per conflicting field manager), output size in bytes. Drives a
  Phase-2 alert rule set that ships with the chart (gated off by default).
- **Status**: every CR carries `Validated` and `Applied` conditions
  with a fixed reason vocabulary. The `Applied` message includes the
  output ConfigMap's name, generation, and content hash so users can
  verify their contribution k8s-natively, without curling Loki/Mimir.
- **Events**: `ValidationFailed`, `FieldOverridden`, `TargetDisabled`,
  `ConfigMapDrifted` — all alertable.
- **Drift**: the operator watches its own output ConfigMap. Third-party
  writes are reverted within seconds; a `ConfigMapDrifted` event names
  the conflicting field manager (capped at 64 chars, `unknown` fallback).
- **Webhook outage**: `failurePolicy: Ignore` means a webhook outage
  doesn't block CR writes — Layer 3 (controller-side validation) is the
  load-bearing safety net. See [ADR 0001](docs/adr/0001-webhook-failure-policy-ignore.md).

## Development

```sh
# unit + envtest
make test

# end-to-end on KinD
make test-e2e

# lint the Helm chart
make chart-lint

# render the chart with non-default values
helm template ro-op deploy/charts/runtime-overrides-operator/ \
  --set operator.targets.mimir.enabled=true

# regenerate the chart README after editing values.yaml or README.md.gotmpl
make chart-docs
```

## Releasing

Releases are driven by [release-please](https://github.com/googleapis/release-please)
parsing the [Conventional Commits](https://www.conventionalcommits.org/) on
`main`. You don't tag or bump versions by hand.

1. Land PRs into `main` with Conventional Commit subjects (`feat: …`,
   `fix: …`, `chore: …`, etc.). `feat!:` or a `BREAKING CHANGE:` trailer
   bumps major; `feat:` bumps minor; `fix:` / `perf:` bumps patch.
2. release-please opens (and keeps updating) a single PR titled
   `chore: release X.Y.Z` containing the bumped `Chart.yaml` and an
   updated `CHANGELOG.md` covering every commit since the last release.
3. Merge that PR when you want to cut the release. release-please tags
   `vX.Y.Z`, publishes a GitHub Release with the changelog as the body,
   and the same workflow's `publish` job then builds the multi-arch
   image with ko, signs it with cosign, packages and pushes the Helm
   chart to GHCR OCI, and attaches the chart `.tgz` to the release.

To trigger a release on demand without merging the release PR (e.g.
re-running a failed publish job), `workflow_dispatch` the Release
workflow from the Actions tab. release-please will be a no-op on a
manually-dispatched run unless its conditions are met.

See [`docs/adr/`](docs/adr/) for the rationale behind specific design
choices that aren't visible in the code.

## Source

Issues, PRs, discussions: https://github.com/tjorri/runtime-overrides-operator

Contributions welcome — see [`CONTRIBUTING.md`](CONTRIBUTING.md) for the
development loop and PR conventions, and [`CLA.md`](CLA.md) for the
Contributor License Agreement that gates pull requests (rationale in
[ADR 0005](docs/adr/0005-cla-for-relicensing-rights.md)).
