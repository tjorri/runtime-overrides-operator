# ADR 0002: AGPL-3.0 operator binary + Apache-2.0 Helm chart

**Status:** Accepted (2026-05-26)

## Decision

The project ships two artifacts under two licenses:

| Artifact | License |
|---|---|
| Operator Go binary, source, container image | **AGPL-3.0-only** |
| Helm chart | **Apache-2.0** |

## Rationale

The operator imports `github.com/grafana/loki/v3/pkg/validation` and
`github.com/grafana/mimir/pkg/util/validation` for the Layer-2 (webhook)
and Layer-3 (controller) validation pipeline. Both upstream packages
are licensed AGPL-3.0. Linking AGPL Go modules into the operator binary
makes the combined work AGPL.

This is exactly the trade-off we wanted: the AGPL imports give us
validation that is, by construction, identical to what Loki and Mimir
run themselves at startup — single source of truth, zero drift across
upstream releases. The alternative (Apache-2 everywhere + hand-rolled
field registry) was viable but ongoing-maintenance-heavy and would lag
upstream by definition.

The Helm chart contains only YAML templates, default values, and a
string reference to the container image. It contains no AGPL code.
The chart and the binary together form "an aggregate" under AGPL §5;
the chart does not become AGPL by referencing the image.

This split is the same model Grafana themselves use:
[github.com/grafana/helm-charts](https://github.com/grafana/helm-charts)
is Apache-2.0 and ships the official Loki, Mimir, Tempo, and Pyroscope
charts, all of which install AGPL binaries via image references.

## Consequences

- Operating an unmodified release: nothing beyond pulling the image.
  AGPL doesn't restrict use.
- Forking and modifying the operator, then running the modified version:
  must offer modified source to users under AGPL §13. For internal-only
  deployments this is mild — a Git repo URL in the operator's startup
  log is sufficient.
- Embedding the operator into a proprietary product: by AGPL design,
  not possible without either (a) keeping the derivative AGPL,
  (b) negotiating an alternative license with the operator's
  maintainer AND clean-room-replacing the Loki/Mimir AGPL imports
  with non-AGPL validators, or (c) reimplementing the operator from
  scratch. Forking this repo and swapping out only the upstream
  validators does NOT relicense our own code — it remains AGPL.

AGPL §13 compliance measures (the source URL is exposed in three
runtime-discoverable places plus the NOTICE file, belt-and-suspenders):

1. Container image OCI label `org.opencontainers.image.source`
   (set by `.ko.yaml`).
2. `Source-Code` HTTP response header on every webhook reply
   (injected by `internal/webhook.WithSourceCodeHeader`).
3. Operator startup banner (cmd/banner.go) prints the source URL on the
   first stdout line.
4. NOTICE file at the repo root.
