# runtime-overrides-operator

Kubernetes operator that assembles per-tenant runtime config overrides
for Grafana Loki and Mimir from declarative LokiTenantOverride /
MimirTenantOverride CRDs. The chart is Apache-2.0; the operator binary
it deploys is AGPL-3.0. See NOTICE at the repo root for details.

![Version: 0.2.4](https://img.shields.io/badge/Version-0.2.4-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.2.4](https://img.shields.io/badge/AppVersion-0.2.4-informational?style=flat-square)

This Apache-2.0 chart installs the AGPL-3.0-licensed runtime-overrides-operator
binary. See the [NOTICE at the repo root](https://github.com/tjorri/runtime-overrides-operator/blob/main/NOTICE)
for the license-split rationale.

## Prerequisites

- **Kubernetes 1.30+** (the bundled `ValidatingAdmissionPolicy` is built-in
  at this version)
- **cert-manager** installed in the cluster (required for the webhook's
  TLS cert; set `operator.webhook.enabled=false` to skip)
- Helm 3.8+ (for OCI registry support)

## Install

```sh
helm install ro-op oci://ghcr.io/tjorri/charts/runtime-overrides-operator \
  --version 0.2.4 \
  --namespace runtime-overrides-system --create-namespace
```

By default this enables the Loki reconciler with output ConfigMap
`monitoring/loki-runtime-tenants`. To enable Mimir as well, pass:

```sh
  --set operator.targets.mimir.enabled=true
```

## Wire the operator's ConfigMap into Loki/Mimir

The Loki/Mimir installer must mount the operator's output ConfigMap as
a **directory** (not subPath — those don't auto-update) and list its
file path **rightmost** in `runtime_config.file`. Snippets:

- [grafana/loki Helm chart](https://github.com/tjorri/runtime-overrides-operator/blob/main/docs/integration/loki-helm.md) — SimpleScalable, SingleBinary, Distributed
- [grafana/mimir-distributed Helm chart](https://github.com/tjorri/runtime-overrides-operator/blob/main/docs/integration/mimir-helm.md)
- [grafana loki-operator LokiStack](https://github.com/tjorri/runtime-overrides-operator/blob/main/docs/integration/loki-operator-lokistack.md)
- [kustomize](https://github.com/tjorri/runtime-overrides-operator/blob/main/docs/integration/kustomize.md)

## Tenant ownership policy

The chart ships a `ValidatingAdmissionPolicy` that gates which namespaces
can set which `tenantId`. Default is `namespace-equals-tenant`. If your
tenant IDs don't map onto namespaces, configure it:

```yaml
tenantOwnership:
  enabled: true
  mode: template
  namespacePattern: "team-{namespace}-prod"
```

Or pass an arbitrary CEL expression with `mode: custom` +
`customExpression`, or disable entirely with `mode: off`.

## When the webhook is down

The admission webhook is a UX enhancement (fast feedback at
`kubectl apply` time). Its `failurePolicy` is `Ignore`: webhook outages
don't block CR writes. Validation is enforced regardless by the
operator's controller — see
[ADR 0001](https://github.com/tjorri/runtime-overrides-operator/blob/main/docs/adr/0001-webhook-failure-policy-ignore.md)
for the rationale.

## Uninstall

```sh
helm uninstall ro-op -n runtime-overrides-system
```

This leaves the output ConfigMaps (and the CRDs and any existing
`*TenantOverride` CRs) in place — Loki/Mimir keep working with the last
applied overrides until you delete those manually.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for the operator pod. |
| certManager | object | `{"enabled":true,"issuerRef":{}}` | cert-manager wiring for the webhook's TLS cert. Required when `operator.webhook.enabled: true`. |
| certManager.enabled | bool | `true` | Render the Certificate (and a self-signed Issuer if no `issuerRef` is set). |
| certManager.issuerRef | object | `{}` — uses the chart's self-signed Issuer | Use an existing cluster issuer instead of the chart's self-signed Issuer. Set to `{name: my-issuer, kind: ClusterIssuer}` (or `Issuer`). |
| commonLabels | object | `{}` | Labels applied to every resource the chart creates. |
| crds | object | `{"install":true}` | CRD installation behavior. By default the main chart installs the `LokiTenantOverride` and `MimirTenantOverride` CRDs as Helm release resources. Set `crds.install: false` if you install the CRDs separately via the dedicated `runtime-overrides-operator-crds` chart (recommended for production: gives you explicit upgrade lifecycle via `helm upgrade`) or via raw `kubectl apply -f` from the per-release `crds.yaml` asset. |
| crds.install | bool | `true` | Render and install the CRDs as part of this chart's release. Disable when installing the CRDs out-of-band. |
| image | object | `{"pullPolicy":"IfNotPresent","pullSecrets":[],"repository":"ghcr.io/tjorri/runtime-overrides-operator","tag":""}` | The operator's container image. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy. |
| image.pullSecrets | list | `[]` | Image pull secrets if the image is in a private registry. |
| image.repository | string | `"ghcr.io/tjorri/runtime-overrides-operator"` | Container image repository for the operator binary. |
| image.tag | string | `.Chart.AppVersion` | Image tag. Default is the chart's appVersion; override to use a specific operator release independent of the chart version. |
| metrics.serviceMonitor.enabled | bool | `false` | Render a Prometheus ServiceMonitor for scraping the metrics endpoint. Requires prometheus-operator in the cluster. |
| nodeSelector | object | `{}` | nodeSelector for the operator pod. |
| operator | object | `{"targets":{"loki":{"enabled":true,"outputConfigMap":{"name":"loki-runtime-tenants","namespace":"monitoring"}},"mimir":{"enabled":false,"outputConfigMap":{"name":"mimir-runtime-tenants","namespace":"monitoring"}}},"webhook":{"enabled":true}}` | Per-target configuration. One Loki + one Mimir per operator deployment. Multi-target setups: install the chart twice with different release names and outputConfigMap names. |
| operator.targets.loki.enabled | bool | `true` | Enable the Loki reconciler. When false, the chart still installs the LokiTenantOverride CRD; the disabled-target reconciler then surfaces `Applied=False, reason=TargetDisabled` on any CR. |
| operator.targets.loki.outputConfigMap | object | `{"name":"loki-runtime-tenants","namespace":"monitoring"}` | The ConfigMap the operator owns and writes per-tenant overrides to. The Loki installer must mount this CM as a directory and add the `runtime-tenants.yaml` file as the RIGHTMOST entry in its runtime_config.file CSV. Operator's value wins on conflict against the installer's base file. |
| operator.targets.loki.outputConfigMap.name | string | `"loki-runtime-tenants"` | Name of the Loki output ConfigMap. |
| operator.targets.loki.outputConfigMap.namespace | string | `"monitoring"` | Namespace of the Loki output ConfigMap. |
| operator.targets.mimir.enabled | bool | `false` | Enable the Mimir reconciler. Same disabled-target semantics as Loki. |
| operator.targets.mimir.outputConfigMap | object | `{"name":"mimir-runtime-tenants","namespace":"monitoring"}` | The ConfigMap the operator owns for Mimir overrides; see the Loki outputConfigMap docs for wire-up requirements. |
| operator.targets.mimir.outputConfigMap.name | string | `"mimir-runtime-tenants"` | Name of the Mimir output ConfigMap. |
| operator.targets.mimir.outputConfigMap.namespace | string | `"monitoring"` | Namespace of the Mimir output ConfigMap. |
| operator.webhook.enabled | bool | `true` | Enable the validating admission webhook. failurePolicy is Ignore (ADR 0001) — Layer-3 controller-side validation is the load-bearing safety net. Disabling here means users don't get apply-time error messages on bad CRs, but validation still gates the merged output. |
| podAnnotations | object | `{}` | Annotations applied to the operator pod. |
| podLabels | object | `{}` | Labels applied to the operator pod (in addition to the chart's selector labels). |
| prometheusRules.enabled | bool | `false` | Render bundled Prometheus alert rules. Off by default — the canonical rule set ships in Phase 2. |
| replicas | int | `2` | Replica count. Two replicas recommended for webhook HA — the webhook server runs on both replicas independently of leader election; only one replica reconciles at a time. |
| resources | object | `{"limits":{"cpu":"500m","memory":"256Mi"},"requests":{"cpu":"50m","memory":"64Mi"}}` | Container resource requests and limits. |
| tenantOwnership | object | `{"crossNamespaceNamespaces":[],"customExpression":"","enabled":true,"mode":"namespace-equals-tenant","namespacePattern":"{namespace}"}` | Bundled ValidatingAdmissionPolicy that gates which namespaces can set which `spec.tenantId`. K8s 1.30+ baseline. |
| tenantOwnership.crossNamespaceNamespaces | list | `[]` | Namespaces allowed to set arbitrary `spec.tenantId`. The default `namespace-equals-tenant` mode honors this list as an escape hatch for central-overrides patterns (e.g. a `platform-overrides` namespace contributing to many tenants). |
| tenantOwnership.customExpression | string | `""` | Used only when `mode: custom`. Raw CEL expression evaluated against the AdmissionRequest. The chart does NOT escape this — you are responsible for valid CEL. |
| tenantOwnership.enabled | bool | `true` | Render the bundled ValidatingAdmissionPolicy. Set false to bring your own admission policy (OPA/Gatekeeper, Kyverno, another VAP, …). |
| tenantOwnership.mode | string | `"namespace-equals-tenant"` | Policy mode. Allowed values: namespace-equals-tenant | template | custom | off. See README for the CEL each generates. |
| tenantOwnership.namespacePattern | string | `"{namespace}"` | Used only when `mode: template`. The `{namespace}` substring is replaced with the request's namespace at admission time. Single quotes are rejected by values.schema.json — they break the generated CEL. |
| tolerations | list | `[]` | Tolerations for the operator pod. |

## Source

- Operator binary (AGPL-3.0) and chart (Apache-2.0):
  https://github.com/tjorri/runtime-overrides-operator

----

_Documentation generated by [helm-docs](https://github.com/norwoodj/helm-docs). Do not edit `README.md` by hand — edit `values.yaml` or `README.md.gotmpl` and run `make chart-docs`._
