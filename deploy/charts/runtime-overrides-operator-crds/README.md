# runtime-overrides-operator-crds

CustomResourceDefinitions for runtime-overrides-operator
(LokiTenantOverride, MimirTenantOverride). Installed as Helm release
resources so `helm upgrade` actually applies schema changes — see
ADR 0006 for why this is a separate chart from the operator.

Apache-2.0 licensed.

![Version: 0.2.0](https://img.shields.io/badge/Version-0.2.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.2.0](https://img.shields.io/badge/AppVersion-0.2.0-informational?style=flat-square)

This Apache-2.0 chart installs only the two CustomResourceDefinitions
that runtime-overrides-operator depends on:

- `lokitenantoverrides.runtimeoverrides.io`
- `mimirtenantoverrides.runtimeoverrides.io`

It exists because Helm's bundled-CRDs path (the special `crds/`
directory) and `templates/crds/`-with-default-settings both have a
well-known gap: `helm upgrade` does not update CRDs installed those
ways. CRD-only schema changes (new optional fields, validation
tightening, printer columns) then never reach the cluster.

By packaging the CRDs as their own Helm release, this chart sidesteps
that limitation. `helm upgrade runtime-overrides-operator-crds ...`
applies the new CRD spec because the CRDs are tracked as ordinary
Helm release resources.

See [ADR 0006](https://github.com/tjorri/runtime-overrides-operator/blob/main/docs/adr/0006-crd-chart-split.md)
for the full rationale.

## Prerequisites

- **Kubernetes 1.30+** (matches the operator chart's prerequisite for
  `ValidatingAdmissionPolicy`)
- Helm 3.8+ (for OCI registry support)

## Install

```sh
helm install ro-op-crds oci://ghcr.io/tjorri/charts/runtime-overrides-operator-crds \
  --version 0.2.0
```

Then install the operator chart with CRD installation disabled (otherwise
the two charts conflict on the same resources):

```sh
helm install ro-op oci://ghcr.io/tjorri/charts/runtime-overrides-operator \
  --version 0.2.0 \
  --namespace runtime-overrides-system --create-namespace \
  --set crds.install=false
```

## Upgrade

Bump this chart first, then the operator chart. The two charts are
versioned in lockstep — chart versions match each other and match the
operator's `appVersion`.

```sh
helm upgrade ro-op-crds oci://ghcr.io/tjorri/charts/runtime-overrides-operator-crds \
  --version <new-version>
helm upgrade ro-op oci://ghcr.io/tjorri/charts/runtime-overrides-operator \
  --version <new-version> \
  --set crds.install=false
```

## Alternative: raw CRD manifest

If you don't want to manage CRDs via Helm at all, every release ships a
single aggregated YAML asset on the GitHub release. Apply it with
`kubectl`:

```sh
kubectl apply -f https://github.com/tjorri/runtime-overrides-operator/releases/download/v0.2.0/crds.yaml
```

## Verifying signature and provenance

This chart is cosign-signed (keyless via GitHub OIDC) and carries a
SLSA build-provenance attestation, the same way the operator chart and
image do. See the operator chart's README for the verification
commands.

## Source

Source code, issues, discussions:
<https://github.com/tjorri/runtime-overrides-operator>

## License

Apache-2.0. See [LICENSE](LICENSE) for the full text.

The operator binary this chart's CRDs target is AGPL-3.0-only. See the
[NOTICE](https://github.com/tjorri/runtime-overrides-operator/blob/main/NOTICE)
at the repo root for the licensing split rationale.
