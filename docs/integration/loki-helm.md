# Wiring runtime-overrides-operator into the grafana/loki Helm chart

This document covers the three deployment modes of the official
[grafana/loki](https://artifacthub.io/packages/helm/grafana/loki) chart:
SimpleScalable (the default), SingleBinary, and Distributed.

In all three, you need to do two things:

1. Mount the operator's output ConfigMap as a **directory** (not subPath
   — subPath ConfigMap mounts don't auto-update; see
   [kubernetes/kubernetes#50345](https://github.com/kubernetes/kubernetes/issues/50345)).
2. Add the mounted file path to `runtime_config.file` as a CSV entry
   **listed rightmost** so the operator's per-tenant values override
   anything the installer's base file says about the same tenant.

The operator's ConfigMap defaults are `monitoring/loki-runtime-tenants`
(adjust via Helm values if you put the operator in a different namespace).
The data key inside the ConfigMap is `runtime-tenants.yaml`.

## SimpleScalable / SingleBinary

The `loki` chart exposes runtime config and extra volumes via
`structuredConfig`, `extraVolumes`, and `extraVolumeMounts`:

```yaml
loki:
  structuredConfig:
    runtime_config:
      file: /etc/loki/runtime-base/base.yaml,/etc/loki/runtime-tenants/runtime-tenants.yaml

# Mount the operator's CM on every component that loads runtime config.
# For SimpleScalable, that's write, read, and backend.
# For SingleBinary, the `singleBinary` block alone is sufficient.
write:
  extraVolumes:
    - name: runtime-tenants
      configMap:
        name: loki-runtime-tenants
  extraVolumeMounts:
    - name: runtime-tenants
      mountPath: /etc/loki/runtime-tenants
read:
  extraVolumes:
    - name: runtime-tenants
      configMap:
        name: loki-runtime-tenants
  extraVolumeMounts:
    - name: runtime-tenants
      mountPath: /etc/loki/runtime-tenants
backend:
  extraVolumes:
    - name: runtime-tenants
      configMap:
        name: loki-runtime-tenants
  extraVolumeMounts:
    - name: runtime-tenants
      mountPath: /etc/loki/runtime-tenants
singleBinary:
  extraVolumes:
    - name: runtime-tenants
      configMap:
        name: loki-runtime-tenants
  extraVolumeMounts:
    - name: runtime-tenants
      mountPath: /etc/loki/runtime-tenants
```

Important: the operator's ConfigMap must exist in the same namespace as
Loki, or you must use a `projected` volume / reflection controller to
make it visible across namespaces. The simplest approach is to set the
operator's `operator.targets.loki.outputConfigMap.namespace` to the
namespace where Loki is installed.

## Distributed

For the distributed chart, every component that reads runtime config
needs the mount. That's: `distributor`, `ingester`, `querier`,
`queryFrontend`, `queryScheduler`, `compactor`, `indexGateway`, `ruler`,
`bloomCompactor`, `bloomGateway` (if enabled), plus `gateway` if it
proxies runtime config.

Each component takes the same `extraVolumes`/`extraVolumeMounts` shape
shown above. To avoid the copy-paste, use a YAML anchor:

```yaml
_volumes: &runtime-tenants-volume
  - name: runtime-tenants
    configMap:
      name: loki-runtime-tenants
_volumeMounts: &runtime-tenants-mount
  - name: runtime-tenants
    mountPath: /etc/loki/runtime-tenants

loki:
  structuredConfig:
    runtime_config:
      file: /etc/loki/runtime-base/base.yaml,/etc/loki/runtime-tenants/runtime-tenants.yaml

distributor:
  extraVolumes: *runtime-tenants-volume
  extraVolumeMounts: *runtime-tenants-mount
ingester:
  extraVolumes: *runtime-tenants-volume
  extraVolumeMounts: *runtime-tenants-mount
# ... repeat for every runtime-config-reading component
```

## Verify once

The Loki image is distroless (no `sh`/`curl`/`wget`), so port-forward
and probe `/runtime_config` from your own shell:

```sh
kubectl -n loki port-forward deploy/loki 3100:3100 &
curl -s http://localhost:3100/runtime_config | yq '.overrides'
```

You should see `{}` initially (the operator bootstraps an empty
ConfigMap). Apply a `LokiTenantOverride` and re-run the command; the
tenant block should appear within ~90 seconds.
