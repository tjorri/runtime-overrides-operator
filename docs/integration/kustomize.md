# Wiring runtime-overrides-operator into a kustomize-based install

If you don't use Helm — or you bake Loki/Mimir manifests into a
kustomize base — the same two requirements apply:

1. Mount the operator's output ConfigMap as a directory.
2. List its file path **rightmost** in `runtime_config.file`.

The cleanest way to express this in kustomize is a strategic-merge
patch:

```yaml
# overlays/loki-runtime-tenants.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: loki
spec:
  template:
    spec:
      containers:
        - name: loki
          args:
            - "-config.file=/etc/loki/config/loki.yaml"
            - "-runtime-config.file=/etc/loki/runtime-base/base.yaml,/etc/loki/runtime-tenants/runtime-tenants.yaml"
          volumeMounts:
            - name: runtime-tenants
              mountPath: /etc/loki/runtime-tenants
      volumes:
        - name: runtime-tenants
          configMap:
            name: loki-runtime-tenants
```

```yaml
# kustomization.yaml
resources:
  - ../base
patches:
  - path: overlays/loki-runtime-tenants.yaml
```

The same shape works for Mimir; substitute the path and namespace.

## Cross-namespace ConfigMap references

`configMap.name` must reference a ConfigMap in the same namespace as
the Deployment. If your operator and your Loki/Mimir live in different
namespaces, configure the operator's
`operator.targets.{loki,mimir}.outputConfigMap.namespace` to match
the Loki/Mimir namespace. The operator's scoped RBAC (per-target Role
with `resourceNames`) is generated for whichever namespace you choose.
