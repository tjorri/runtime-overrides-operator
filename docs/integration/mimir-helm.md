# Wiring runtime-overrides-operator into the grafana/mimir-distributed Helm chart

The official [grafana/mimir-distributed](https://artifacthub.io/packages/helm/grafana/mimir-distributed)
chart exposes runtime config via `mimir.structuredConfig` and per-component
`extraVolumes` / `extraVolumeMounts`.

Same two requirements as for Loki:

1. Mount the operator's output ConfigMap as a **directory** (not subPath).
2. Add the mounted file path to `runtime_config.file` as a CSV entry
   **rightmost** of any installer-supplied base file.

The operator's ConfigMap defaults to `monitoring/mimir-runtime-tenants`
with the data key `runtime-tenants.yaml`.

## Configuration

```yaml
mimir:
  structuredConfig:
    runtime_config:
      file: /etc/mimir/runtime-base/base.yaml,/etc/mimir/runtime-tenants/runtime-tenants.yaml

# YAML anchor to share the mount across all runtime-config-reading components.
_runtime_tenants_volume: &runtime_tenants_volume
  - name: runtime-tenants
    configMap:
      name: mimir-runtime-tenants
_runtime_tenants_mount: &runtime_tenants_mount
  - name: runtime-tenants
    mountPath: /etc/mimir/runtime-tenants

distributor:
  extraVolumes: *runtime_tenants_volume
  extraVolumeMounts: *runtime_tenants_mount
ingester:
  extraVolumes: *runtime_tenants_volume
  extraVolumeMounts: *runtime_tenants_mount
querier:
  extraVolumes: *runtime_tenants_volume
  extraVolumeMounts: *runtime_tenants_mount
query_frontend:
  extraVolumes: *runtime_tenants_volume
  extraVolumeMounts: *runtime_tenants_mount
query_scheduler:
  extraVolumes: *runtime_tenants_volume
  extraVolumeMounts: *runtime_tenants_mount
store_gateway:
  extraVolumes: *runtime_tenants_volume
  extraVolumeMounts: *runtime_tenants_mount
compactor:
  extraVolumes: *runtime_tenants_volume
  extraVolumeMounts: *runtime_tenants_mount
ruler:
  extraVolumes: *runtime_tenants_volume
  extraVolumeMounts: *runtime_tenants_mount
alertmanager:
  extraVolumes: *runtime_tenants_volume
  extraVolumeMounts: *runtime_tenants_mount
```

## Verify once

The Mimir image is distroless (no `sh`/`curl`/`wget`), so port-forward
and probe `/runtime_config` from your own shell:

```sh
kubectl -n mimir port-forward deploy/mimir-distributor 8080:8080 &
curl -s http://localhost:8080/runtime_config | yq '.overrides'
```

Apply a `MimirTenantOverride` and re-run; the tenant block should appear
within ~90 seconds.
