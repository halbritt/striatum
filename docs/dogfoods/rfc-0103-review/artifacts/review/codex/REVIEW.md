---
artifact_kind: finding
schema_version: striatum.finding.v1
title: RFC 0103 soundness review
author: reviewer-codex-gpt-5.5-xhigh-001
created_at: 2026-06-02T04:41:53Z
run_id: run_05c653068a094c25ca8ce2da0b190a33
verdict_intent: needs_revision
severity: medium
---

## Verdict

`needs_revision`.

RFC 0103 is directionally sound: the seven workstreams form a real partition of
the named 17 issues, the trust-first dependency ordering is coherent, and most
per-workstream acceptance checks are falsifiable. I would not accept it yet
because two acceptance surfaces are still loose enough to let the RFC pass
without proving the production-grade self-hosting property it claims.

## Trust Boundaries And Attack Surfaces

- **Lane process to daemon MCP:** W1 correctly identifies bearer-token scope,
  session impersonation, and environment leakage as the core boundary.
- **Lane process to durable repository files:** W1 correctly treats credentialed
  adapter settings in the target worktree as provenance contamination.
- **Lane process to daemon PostgreSQL:** W1 correctly names direct libpq/socket
  access as a bypass of the artifact API and daemon authority boundary.
- **Adapter wrapper to live session state:** W2 and W3 cover session re-entry,
  MCP discovery stalls, `work.ack`, and helper reattachment after transport
  churn.
- **Reviewer retry to panel lifecycle:** W4 covers the interrogation window that
  must survive reviewer replacement without wedging.
- **Workflow packet to artifact publisher:** W5 covers schema/front-matter
  contract drift and downstream `write_scope` mismatch.
- **Frozen run snapshot and shared resources:** W6 covers stale workflow edits
  and DB-backed review-gate contention.
- **Operator surface to local diagnostics:** W7 covers the risk of falling back
  to tmux/systemctl/psql and the privacy-sensitive trajectory/log surface.

The RFC acknowledges these boundaries; the required revisions below are about
making the acceptance gates prove them.

## Findings

### 1. W3 acceptance allows the main churn scenario to pass by escalating

RFC 0103 says W3 is about a lane surviving daemon and transport churn, including
helper reattachment after restart and non-substitutable `work.ack`
(`docs/rfcs/0103-self-hosting-production-hardening.md:102`). The stated
acceptance, however, lets the daemon-restart fault pass if the job "completes
(or escalates) within budget" (`docs/rfcs/0103-self-hosting-production-hardening.md:113`).

That is too weak for this RFC's own production-grade bar. A restart fault that
escalates may be honest liveness, but it does not prove that the lane survives
daemon churn, and it can still fail the umbrella requirement that a self-hosting
dogfood completes hands-off and lands the fix
(`docs/rfcs/0103-self-hosting-production-hardening.md:190`).

Required revision: split W3 acceptance into two cases. For expected daemon
restart/socket recreation, require helper rebind, lane reconnection, `work.ack`
integrity, and job completion through production handlers. Reserve escalation
only for an explicitly unrecoverable injected fault class, with a separate
assertion that it does not wedge the run.

### 2. Umbrella acceptance does not pin the adapter/fault matrix tightly enough

The umbrella acceptance requires "at least two distinct adapter seats" and one
injected lane/daemon fault (`docs/rfcs/0103-self-hosting-production-hardening.md:190`).
But W2 states that `agy` is not currently viable and that, until fixed,
`claude/codex` are the supported multi-lane shape
(`docs/rfcs/0103-self-hosting-production-hardening.md:91`,
`docs/rfcs/0103-self-hosting-production-hardening.md:96`). The review brief
calls out the same risk: the umbrella could collapse to a claude+codex checkbox
while the declared adapter-seat problem remains unresolved
(`docs/dogfoods/rfc-0103-review/artifacts/REVIEW_BRIEF.md:44`).

The per-workstream W2 acceptance does require an `agy` two-turn
claim-publish-claim conformance fixture, so the RFC is close. The umbrella gate
should explicitly say whether the production-grade dogfood must include every
declared supported adapter after W2 lands, or whether claude+codex is the scoped
support set. It should also name the minimum injected-fault matrix, because one
fault is not enough to exercise the distinct attack surfaces in W1, W3, and W4.

Required revision: define the adapter set and fault matrix for umbrella
acceptance. At minimum, require the final dogfood to include the W2-supported
adapter set or explicitly defer non-supported adapters, and require separate
coverage for lane credential isolation, daemon/transport churn, and reviewer
replacement/interrogation.

### 3. W7 acceptance needs a privacy and falsifiability clause

W7 says the operator should be able to drive a full run without dropping to
tmux/systemctl/psql, and that a lane trajectory is readable from "the one
surface" (`docs/rfcs/0103-self-hosting-production-hardening.md:164`). That is
directionally right, but "normal loop", "one surface", and "trajectory" are not
test predicates. The same RFC also has a hard non-goal against durable transcript
capture/export (`docs/rfcs/0103-self-hosting-production-hardening.md:199`), so
trajectory extraction is a privacy-sensitive attack surface.

Required revision: define W7's testable operator workflow and state where
trajectory material may live. The acceptance should assert that the operator can
observe and act on headline asks through the Striatum-mediated surface without
raw durable transcript capture, and that any local diagnostics remain private
operator scratch.

## Soundness Checks

- **Partition:** The workstreams account for the 17 named open issues exactly:
  W1 has 3, W2 has 4, and W3 through W7 have 2 each. I did not find a dropped or
  duplicated issue in the RFC's own issue list.
- **Dependency ordering:** W1 first is correct because the lane credential and
  Postgres bypass issues are the highest-risk trust boundary. W2/W3/W4 are
  reasonably parallel as multi-lane viability work. W5/W6 can ship independently
  but must be in place before the umbrella dogfood is claimed as production
  evidence. W7 is correctly last because it consumes the lower-layer signals.
- **Acceptance criteria:** W1, W2, W4, W5, and W6 have concrete negative or
  conformance-style checks. W3, W7, and the umbrella gate need the revisions
  above before the RFC's acceptance criteria fully prove the stated property.
