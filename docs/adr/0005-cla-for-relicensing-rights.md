# ADR 0005: Require a Contributor License Agreement for outside contributions

**Status:** Accepted (2026-05-27)

## Decision

Outside contributions to runtime-overrides-operator require a signed
Individual Contributor License Agreement ([`CLA.md`](../../CLA.md)). The
CLA preserves the contributor's copyright on their contribution but
grants the project maintainer a perpetual, irrevocable right to
sublicense the contribution under license terms of the maintainer's
choosing — including, if the need arises, alternative license terms
for users who cannot accept AGPL.

A GitHub bot (`cla-assistant.io` or equivalent) gates pull requests on
this Agreement.

## Rationale

[ADR 0002](0002-agpl-binary-apache-chart.md) records that the operator
binary is AGPL-3.0-only by virtue of linking against
`github.com/grafana/loki/v3/pkg/validation` and
`github.com/grafana/mimir/pkg/util/validation`, both of which are
themselves AGPL-3.0. The operator's own code under `cmd/`, `internal/`,
and `api/` is also AGPL-3.0-only by our deliberate choice. AGPL-3.0 is
the intended license for this project; it is not a placeholder for
something else later.

The narrower reason for this ADR is the contributor-side legal
mechanics. The maintainer wants to keep open the *possibility* — not
the promise, and not the plan — that some future user who has a
legitimate need for the software but cannot accept AGPL (a regulatory
context, a downstream license incompatibility, an embedding into a
codebase under a non-compatible OSS license) could be accommodated
with an alternative license to the maintainer's own code, paired with
a non-AGPL replacement for the upstream validators. Whether that
accommodation ever happens is an open question the maintainer is in no
hurry to answer. Reserving the *option* is the point; pursuing it is
not.

Without a CLA, the moment a third party contributes AGPL code under
the project's inbound-equals-outbound default, that contributor holds
copyright on their contribution. The maintainer can no longer
sublicense the *combined* work without the contributor's permission,
because the maintainer is no longer the sole copyright holder. Across
many contributors this becomes intractable: locating each one, getting
each to sign a one-off relicensing grant, and accepting that any one
contributor's refusal blocks the entire arrangement.

A CLA closes that gap by getting the sublicensing grant at the time of
contribution rather than retroactively. This is a one-time bit of
contributor-side friction to preserve a long-term option that may or
may not ever be exercised.

## Why this CLA in particular

We considered three patterns:

1. **No CLA / inbound=outbound.** GitHub's default. Cheap and
   contributor-friendly but eliminates the commercial dual-licensing
   option permanently after the first outside merge.
2. **Copyright Assignment Agreement (CAA).** Used by the FSF. Strongest
   for the project (the maintainer becomes the sole copyright holder)
   but socially loaded — assignment scares away many contributors and
   is sometimes legally unenforceable in jurisdictions that don't
   recognize copyright transfer the way the US does.
3. **CLA with sublicensing grant.** Used by Sentry, MongoDB (pre-SSPL),
   Elastic (pre-ELv2), GitLab. Contributor retains copyright but grants
   the maintainer the right to sublicense.

We chose (3). The maintainer doesn't need to *own* the contribution;
the maintainer needs the *option* to relicense it. (3) gives that and
nothing more.

The text is adapted from the Apache Software Foundation Individual
Contributor License Agreement v2.2 (a well-tested template) with one
substantive addition: an explicit "Right to Sublicense and Relicense"
section spelling out the commercial-license use case the CLA exists to
enable.

## What we are NOT doing

The CLA includes a public commitment, in writing:

> the publicly distributed source release of the Project will continue
> to be made available under an OSI-approved free and open source
> software license. We will not use the rights granted in this
> Agreement to take the public release proprietary or
> source-available-only.

This is the load-bearing trust statement. CLAs have a reputation
problem because they're the mechanism that enabled the Elastic →
ELv2, HashiCorp → BUSL, and Redis → SSPL relicensing rug-pulls.
Contributors are right to be wary. The commitment defuses that by
binding the maintainer to the kind of license shift those projects
made.

The CLA does **not** prevent the project from moving between OSI-
approved licenses (e.g., AGPL-3.0 → AGPL-3.0-or-later, or to a future
OSI-approved successor). It only prevents moves *off* OSI-approved
terms for the public release.

## Consequences

- The first outside contribution requires a one-time consent click. The
  bot makes this a few-seconds friction, not a barrier.
- Contributions made before this ADR was adopted are limited to the
  maintainer's own — no retroactive signature-chase is needed.
- If a situation ever arises where granting a user an alternative
  license to the operator's own code is the right call, the CLA makes
  that administratively possible. The upstream Loki/Mimir AGPL imports
  remain a separate problem in that scenario — see ADR 0002.
- If the maintainer ever wants to switch the public release to a
  non-OSI-approved license, that would breach the CLA's commitment.
  Contributors would have grounds to object, and downstream users have
  this ADR on the record as a representation.
- A Corporate CLA template may follow if a company wants to contribute
  on behalf of employees. This ADR does not preempt that.

## References

- Apache ICLA: <https://www.apache.org/licenses/icla.pdf>
- Project Harmony Contributor Agreements: <https://www.harmonyagreements.org/>
- cla-assistant: <https://cla-assistant.io/>
- Background on CLAs and trust: Heather Meeker, "Practical CLA Drafting,"
  Open Source for Business (2nd ed., 2020).
