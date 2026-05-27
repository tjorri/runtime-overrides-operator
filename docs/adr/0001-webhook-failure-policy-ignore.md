# ADR 0001: Webhook `failurePolicy: Ignore`

**Status:** Accepted (2026-05-26)

## Decision

The `ValidatingWebhookConfiguration` for `LokiTenantOverride` and
`MimirTenantOverride` uses `failurePolicy: Ignore`, not `Fail`.

## Rationale

The validation pipeline has three layers:

1. **Layer 1**: CRD OpenAPI schema (typos in the wrapper).
2. **Layer 2**: Admission webhook (upstream `Limits.Validate()` at apply time).
3. **Layer 3**: Controller-side validation (same upstream `Validate()` at
   merge time).

Layer 3 is the load-bearing safety guarantee: the merged output ConfigMap
is always safe because the reconciler re-runs upstream `Validate()` per CR
before including it in the merge. Bad CRs that bypass Layer 2 (via
`kubectl --validate=false`, during a webhook outage, or because the
operator was upgraded with newer upstream rules than the webhook had at
admission time) are caught at Layer 3 and excluded from the output.

`failurePolicy: Ignore` makes the webhook a *UX enhancement* — fast
feedback at apply time — rather than a hard gate. The trade:

- **Cost of `Ignore`**: bad CRs can land in etcd during a webhook outage.
  Users see `Validated=False` in status afterwards.
- **Benefit of `Ignore`**: webhook outages do not block CR writes.
  Critically, this includes the escape-hatch use case where an SRE is
  applying an emergency override during an incident that may itself be
  affecting the operator.

- **Cost of `Fail`**: incident response is blocked when the operator (and
  its webhook) is part of the incident. cert-manager renewal failures,
  rolling restarts with misconfigured PDBs, and the operator's own
  upgrade window all become outage events.
- **Benefit of `Fail`**: slightly faster rejection feedback for typos at
  apply time (instant vs. `Validated=False` ~1s later); cleaner etcd
  state (no bad CRs sitting around).

The asymmetry is decisive: `Fail` makes the *common path* (creating good
CRs) brittle, while `Ignore` makes the *rare path* (creating bad CRs
during a webhook outage) slightly more annoying. That's the wrong trade
for an operator whose job is to enable dynamic tenant configuration —
especially during incidents.

## Consequences

- The framing ("webhook is UX; Layer 3 is safety") is load-bearing.
  Future work that weakens Layer 3 must revisit this ADR.
- The bundled `ValidatingAdmissionPolicy` for tenant ownership is
  configured separately and may use `Fail` semantics if appropriate —
  that's a different policy axis.
- The README documents this behavior under "When the webhook is down."
