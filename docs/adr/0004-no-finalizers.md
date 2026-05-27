# ADR 0004: No finalizers on `*TenantOverride` CRs

**Status:** Accepted (2026-05-26)

## Decision

`LokiTenantOverride` and `MimirTenantOverride` CRs do **not** have
finalizers. Deletion is immediate; the controller picks up the deletion
on the next reconcile via informer-cache eventual consistency and the
CR's contribution drops out of the merged output ConfigMap.

## Rationale

Finalizers exist to gate object deletion on external cleanup — e.g.,
"don't delete this CR until we've removed the cloud resource it
created." The operator's output ConfigMap is **internal state we own
end-to-end**, not external state we need to clean up. There's nothing
to gate on:

- The merge re-renders from scratch every reconcile, using whatever
  CRs the informer cache currently lists.
- If a CR is deleted, the next list returns the post-deletion set, and
  the next render produces a ConfigMap without that CR's contribution.
- controller-runtime's informer cache is eventually-consistent but
  fast (sub-second under normal load); a small race window where a
  deleted CR is still cached is fine — the next reconcile fixes it.

What we lose by **not** having finalizers:

- Cannot guarantee "deleted CRs disappear from the live ConfigMap
  before the CR's metadata is GC'd." This is unobservable to users —
  there's no externally-visible inconsistency.

What we gain:

- **No stuck-on-finalizer foot-guns.** If the operator pod is down and
  a user tries to delete a CR (or its namespace), the deletion goes
  through immediately. With finalizers, the deletion would hang until
  the operator returned, which is a routine surprise for operators.
- **No `Update().Finalizers = append(...)` write on every reconcile**
  (subtle write storm), no finalizer-removal logic to maintain.
- **Cluster cleanup is straightforward.** `kubectl delete ns
  test-tenant-a` works even when the operator is uninstalled.

## Consequences

- The output ConfigMap may briefly contain a tenant's stanza after the
  user's `kubectl delete` returns. The next reconcile (usually <1s)
  clears it.
- The chart's uninstall path doesn't need to special-case finalizer
  cleanup.
- Future features that genuinely require external cleanup (none
  planned) would need to add finalizers in a focused way rather than
  inheriting them by default.
