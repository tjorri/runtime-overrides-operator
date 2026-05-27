# Wiring runtime-overrides-operator into grafana/loki-operator LokiStack

The Grafana `loki-operator` deploys Loki via a `LokiStack` CR rather
than via raw Helm values. To inject the operator's output ConfigMap
into the LokiStack, you need to use the LokiStack's `template`
override fields (available on recent versions) or patch the rendered
manifests with a `MutatingWebhookConfiguration` or kustomize overlay.

> **Compatibility note.** This integration relies on the LokiStack
> `spec.template.<component>.podTemplate` field. That field requires:
>
> - `grafana/loki-operator` **v0.5.0+** (upstream — released 2023);
> - or OpenShift Logging Operator **5.7+** if you're on OCP.
>
> Verify your installation supports it by inspecting the CRD:
>
> ```sh
> kubectl get crd lokistacks.loki.grafana.com -o jsonpath='{.spec.versions[?(@.name=="v1")].schema.openAPIV3Schema.properties.spec.properties.template}' | head -c 80
> ```
>
> If that returns empty, the field isn't in your CRD yet — fall back to
> the Helm chart integration in [loki-helm.md](./loki-helm.md). You can
> deploy Loki via the Helm chart and the loki-operator in parallel for
> different stacks.

## Template overrides

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: my-stack
  namespace: openshift-logging
spec:
  template:
    distributor:
      podTemplate:
        spec:
          containers:
            - name: loki
              args:
                - "-runtime-config.file=/etc/loki/config/runtime-config.yaml,/etc/loki/runtime-tenants/runtime-tenants.yaml"
              volumeMounts:
                - name: runtime-tenants
                  mountPath: /etc/loki/runtime-tenants
          volumes:
            - name: runtime-tenants
              configMap:
                name: loki-runtime-tenants
    # Repeat the same template patch for ingester, querier, query-frontend,
    # index-gateway, compactor, ruler — every read-runtime-config component.
```

Verify in the same way as the Helm-chart integration — the Loki image
is distroless, so port-forward and probe from your own shell:

```sh
kubectl -n openshift-logging port-forward deploy/my-stack-distributor 3100:3100 &
curl -s http://localhost:3100/runtime_config | yq '.overrides'
```
