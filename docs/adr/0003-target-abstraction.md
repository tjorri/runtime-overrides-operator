# ADR 0003: Per-target reconcilers, no generic abstraction

**Status:** Accepted (2026-05-26)

## Decision

`internal/controller/loki_controller.go` and `internal/controller/mimir_controller.go`
are concrete, parallel reconcilers — one per backend — that each call
into shared utility packages (`internal/merge`, `internal/render`,
`internal/apply`, `internal/validate`). There is no `Target` interface
that abstracts the two; the kind-specific bits (CR list type, validator
selection, target metric label, reconcile-queue singleton key) are
duplicated across the two files by design.

## Rationale

The two reconcilers are genuinely symmetric — they parse a freeform
overrides map, group by tenant, deep-merge, validate, render, and apply
to one ConfigMap. An interface-based abstraction is tempting:

```go
type Target interface {
    Name() string
    OutputCM() types.NamespacedName
    ListOverrides(ctx) ([]merge.Override, error)
    UpdateStatus(ctx, ...) error
    // ...
}
```

We explicitly chose duplication over abstraction because:

1. **Status types diverge by kind.** `LokiTenantOverrideStatus` and
   `MimirTenantOverrideStatus` are separate types in `api/v1alpha1/`
   (Kubernetes API convention). Threading them through an interface
   requires either erasing the type to `client.Object` (losing all the
   safety we get from typed CRs) or generics-with-callback gymnastics
   that obscure rather than simplify.
2. **Each file reads end-to-end.** A new contributor reads
   `loki_controller.go` and sees the whole Loki reconcile flow on one
   screen. With an abstraction, they'd jump between the interface
   definition, two implementations, and the shared core.
3. **Divergence is acceptable and bounded.** If Loki and Mimir ever
   need different reconcile shapes (e.g., a Mimir-specific status
   field or a different merge invariant), the cost of forking the
   shared code is zero — they're already forked. With an abstraction,
   that change becomes "refactor the abstraction first."
4. **The shared parts are already extracted.** `internal/merge`,
   `internal/render`, `internal/apply`, `internal/validate`, and the
   helpers in `internal/controller/status.go`, `peers.go`,
   `drift.go`, `bootstrap.go`, `disabled.go`, and `hashcache.go`
   carry the kind-agnostic logic. The two reconciler files only
   contain the kind-specific glue — a couple hundred lines each.

## Consequences

- Adding a third backend (Tempo, Pyroscope) means a third reconciler
  file, copy-paste-edited from Loki's or Mimir's. ~30 minutes of
  mechanical work.
- Bug fixes that apply to both reconcilers must be applied twice. The
  envtest suite covers Loki end-to-end; symmetry tests for Mimir would
  catch divergent regressions cheaply if any user feedback ever
  surfaces them.
- Renaming a shared field (e.g., `ContributingPeers`) requires touching
  both `lokitenantoverride_types.go` and `mimirtenantoverride_types.go`,
  but `api/v1alpha1/common_types.go` holds the constants that both
  reconcilers depend on, so a typo can't desync the two.
