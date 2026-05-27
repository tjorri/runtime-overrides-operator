# Contributing to runtime-overrides-operator

Thanks for considering a contribution. This document covers the
practical bits — license, CLA, commit conventions, what tests need to
pass — so your first PR lands cleanly.

## License + CLA

The Project is licensed under **AGPL-3.0-only** (operator binary) and
**Apache-2.0** (Helm chart). See [`LICENSE`](LICENSE),
[`NOTICE`](NOTICE), and [ADR 0002](docs/adr/0002-agpl-binary-apache-chart.md)
for the rationale.

Contributions are accepted under a Contributor License Agreement
([`CLA.md`](CLA.md)). The CLA preserves copyright on your contribution
in your name but grants the project maintainer the right to sublicense
the combined work. AGPL-3.0 is and will remain the project's outbound
license; the sublicensing right exists only to keep open the option of
accommodating users for whom AGPL is incompatible with their specific
use, if such a request ever arises. See
[ADR 0005](docs/adr/0005-cla-for-relicensing-rights.md) for the full
reasoning, including what the maintainer is committing *not* to do.

### How to sign

A GitHub bot posts a one-time consent prompt the first time you open a
pull request. Clicking through records your signature against your
GitHub account for all future contributions. If your employer holds
rights to your code, please raise that in the PR — a separate Corporate
CLA arrangement may be needed.

If you cannot or do not wish to sign the CLA, you can still file
issues, propose changes via comments, and report security
vulnerabilities. These do not require a signed CLA.

## Commit conventions

Commits and PR titles use **Conventional Commits**
(<https://www.conventionalcommits.org/>). release-please reads the
commit log to compute the next semver and to assemble the changelog.

Examples:

```
feat(controller): expose per-tenant reconcile latency histogram
fix(webhook): tolerate nil RawExtension in ValidateUpdate
docs(adr): record decision to require a CLA
test(merge): cover same-weight lexical tie-break
chore(deps): bump grafana/loki/v3 to v3.7.3
ci(release): pin azure/setup-helm to v3.16.4
```

Breaking changes use the `!` marker or a `BREAKING CHANGE:` footer —
expect this for any API-shape change while we're pre-1.0.

The footer

```
Co-authored-by: Name <email>
```

is honored by GitHub for joint authorship credit.

## Development loop

```sh
# Pull in tools (controller-gen, envtest, etc).
make build

# Unit + envtest suites (used by CI).
make test

# Lint — golangci-lint + chart lint + chart README freshness check.
make lint chart-lint

# End-to-end against KinD (Loki + Mimir single-binaries; takes ~5 min).
make test-e2e
```

A PR must pass `make test` and `make lint`. The e2e suite is run by CI
on every PR that touches code, manifests, or workflows; if your change
is docs-only it's automatically skipped.

## Generated files

Several files are generated and must stay in sync with their sources:

| Generated | Source | Regenerate with |
|---|---|---|
| `config/crd/bases/*.yaml` | `api/v1alpha1/*_types.go` kubebuilder markers | `make manifests` |
| `deploy/charts/runtime-overrides-operator/templates/crds/*.yaml` | `config/crd/bases/` | `make chart-sync` |
| `deploy/charts/runtime-overrides-operator/README.md` | `deploy/charts/runtime-overrides-operator/values.yaml` annotations + `README.md.gotmpl` | `make chart-docs` |
| `api/v1alpha1/zz_generated.deepcopy.go` | `api/v1alpha1/*_types.go` | `make generate` |

CI fails if any of these are stale.

## PR checklist

Before you submit:

- [ ] CLA signed (the bot will prompt).
- [ ] Commit messages follow Conventional Commits.
- [ ] `make test` passes locally.
- [ ] `make lint chart-lint` is clean.
- [ ] If you touched API types: `make manifests chart-sync` was run.
- [ ] If you touched `values.yaml`: `make chart-docs` was run.
- [ ] New behavior has a test that would fail without the change.
- [ ] User-visible changes are reflected in the README or the relevant
      doc under `docs/`.

## What's in scope

- Bug fixes against the supported Loki/Mimir versions (see the
  compatibility matrix in [README.md](README.md)).
- Performance improvements that don't change the merge semantics.
- New target backends — must implement the same Validator-based contract
  the existing two do (see [ADR 0003](docs/adr/0003-target-abstraction.md)
  for why we don't have a generic interface).
- Documentation, integration guides, examples.

## What's out of scope

- Changes that introduce finalizers on `*TenantOverride` CRs (see
  [ADR 0004](docs/adr/0004-no-finalizers.md)).
- A "policy" mode that allows the operator to talk to Loki/Mimir at
  runtime — this is a non-goal (see the README's "How it works").
- Embedding the upstream validation logic by copy-paste rather than via
  module import — defeats the whole single-source-of-truth design (see
  [ADR 0002](docs/adr/0002-agpl-binary-apache-chart.md)).

If you're unsure, open a discussion or issue before sinking time into a
PR. The maintainer would rather chat for ten minutes than have you
discover a non-goal after building something.

## Reporting security issues

Please use GitHub's **private vulnerability reporting** rather than a
public issue:

  <https://github.com/tjorri/runtime-overrides-operator/security/advisories/new>

See [`SECURITY.md`](SECURITY.md) for the full policy.
