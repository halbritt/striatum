# Striatum: Domain-Driven Design Foundations

A first-time reader of striatum can come to the wrong conclusion about
why it works. The CLI surface looks like "a workflow runner with a
daemon, a database, and verdicts." Most operators have seen ten of
those. They look at striatum, see familiar parts, and ask: *"what's
load-bearing?"*

If they conclude there isn't any, they will work *around* the
vocabulary instead of *with* it: marker files used as state, prose
used to advance jobs, ad-hoc shell scripts making writes the runner
doesn't see, reviews returned as "looks good" instead of as
`accept_with_findings` + a structured finding artifact. The runner
survives that for a while, and then a six-job workflow with three
reviewers and a `needs_revision` cycle melts down, and the operator
concludes the tool was the problem.

The actual answer is that striatum is a **domain-driven design** of
workflow orchestration. The vocabulary in
[`UBIQUITOUS_LANGUAGE.md`](../reference/ubiquitous-language.md) is the *model*.
Daemon RPC methods are the model's state-mutation boundary; CLI,
MCP, and web surfaces are clients of that boundary. The schemas
(workflow, work packet, artifact front matter) are the model's
grammar. The boundary decisions in
[`DECISION_LOG.md`](../decisions/decision-log.md) (D006, D009, D020, D028) are
the *bounded context*.

This document writes the framing down so a new reader can see what's
load-bearing and what isn't, and so future RFCs cite their domain-
modeling rationale rather than re-deriving it each time.

## Bounded context

What striatum models:

- The lifecycle of a *run*: prepare, branch, start, claim, complete,
  terminal.
- The *coordination* between sessions, jobs, leases, and artifacts.
- The *gate* between an artifact existing and an artifact being
  acceptable: verdicts, cycles, required postures (RFC 0018).
- The *provenance* of every state transition (the `events` log).

What striatum deliberately does **not** model:

- The agent's reasoning (no transcripts, D028).
- The build's correctness (the runner does not run tests on the
  artifact body; reviewers do).
- The repository's deployment, packaging, or distribution. striatum
  is a coordination layer; CI/CD is downstream.
- The agent CLI's internal state (supervisors send DEVNULL to agent
  stdout/stderr; the runner never parses agent output for state).
- The user's intent. The runner records decisions; it does not infer
  them.

The boundary is visible in the client surfaces: every production
mutation passes through a daemon-owned method and every refusal
returns a stable error code. If a feature wants to live outside that
boundary (telemetry, hosted service, transcript capture, automatic
commits), it lives outside striatum.

## Ubiquitous language

[`UBIQUITOUS_LANGUAGE.md`](../reference/ubiquitous-language.md) is the canonical
glossary. Three things to internalize:

- **Every term in the vocabulary is load-bearing.** A reviewer's
  `accept` and `accept_with_findings` are not the same word; the
  runner treats them differently. A `coordinator` and an `operator`
  are not the same role; one sits inside the workflow and one drives
  it.
- **New features add to the vocabulary.** The right way to introduce
  concepts like *adversarial review posture* (RFC 0018) or *agent
  skill bundle* (RFC 0015) is to give them a glossary entry first
  and a flag/field/schema second.
- **Code agrees with the vocabulary.** Class names, function names,
  parameter names, error messages, doctor checks, and CHANGELOG
  bullets all use the same words the glossary uses. Drift is a bug,
  not a stylistic choice.

## Aggregate roots

Each row maps to a daemon-owned PostgreSQL table under the run's
`repository_id` scope; the runner enforces the listed invariants
inside short transactional daemon handlers.

| Aggregate | Table | Identity | Invariants the runner enforces |
|---|---|---|---|
| Run | `runs` | `run_id` | states `prepared → ready → running → {completed, failed, canceled}`; `branch_name` recorded once; `workflow_snapshot_id` immutable |
| Session | `sessions` | `session_id` | states `active → closed`; at most one active supervisor per session (RFC 0009); fresh-session policy (RFC 0002) |
| Job | `jobs` | `job_id` | states `pending → queued → claimed → running → {completed, failed, blocked, canceled, skipped}`; required artifacts before `complete` (RFC 0014); review verdict before downstream `complete` |
| Lease | `leases` | `lease_id` | one active lease per session per packet; lazy expiry; lease ownership required for every mutation |
| Work packet | `work_packets` | `packet_id` | one active packet per claimed message; immutable once issued |
| Artifact | `artifacts` | `artifact_id` | append-only (D008); path inside `write_scope.allowed_paths`; front-matter schema-validated when present (RFC 0003/0004/0005) |
| Verdict | `verdicts` | `verdict_id` | one of `accept | accept_with_findings | needs_revision | reject`; references the source review job; (RFC 0018) carries posture |
| Blocker | `blockers` | `blocker_id` | severities `blocked` and `human_checkpoint`; payload metadata only, no agent prose (D028) |

## Value objects

Immutable, equality-by-value, no identity. Constructed at
validate-time and never mutated in flight. A finding's verdict is
recorded once; "changing the verdict" means recording a new verdict
on a new attempt.

- `verdict` (`accept | accept_with_findings | needs_revision | reject`)
- `write_scope` (`allowed_paths`, `forbidden_paths`, `mode`)
- `harness_profile` (passthrough projection on the work packet)
- `byline` (`<role>-<model>-<ordinal>` lowercase string)
- `posture` (RFC 0018: `neutral | security | threat_model | …`)
- `adapter_constraint` + `enforcement_level` (`enforced |
  advisory_strict | advisory | unsupported`)
- `state-class` (Mermaid + ANSI palette key, RFC 0007/0016)
- `recovery_policy` + `sweep envelope` (RFC 0020)

## Domain events

The `events` table is literally a DDD-style event log:

- Every row is an immutable append.
- Every row carries `event_type`, `created_at`, the affected
  aggregate id, and a small structured `payload_json`.
- Reads (status, why, dashboard, evidence export) replay the log;
  mutations append to it.
- Subscribers (RFC 0012's SSE stream, future webhook adapters)
  observe the log; they don't observe the SQL state directly.

This is not "we happened to write events." It's the load-bearing
shape: the runner's read model is *derived* from events; the SQL
state is the materialized projection.

## Daemon Methods As The Write Surface

D006/D009 in DECISION_LOG name this directly. In DDD terms:

- The runner is an *application service* whose production mutations
  are daemon RPC methods.
- Direct database writes from outside the daemon are forbidden even
  when the file permissions or local Postgres role allow them; they
  bypass the invariant checks and break the model.
- CLI, MCP, web, process-adapter, and supervisor surfaces all route
  state changes through the same daemon-owned method vocabulary.
  Local file-authoring helpers such as `workflow validate` and
  `workflow generate` do not mutate live workflow state.

This is what makes the vocabulary load-bearing: a reviewer cannot
return `looks good` because the CLI doesn't accept that. The
vocabulary is enforced by *what the API will let you say*.

## Adding to the model

When a new RFC introduces a concept:

1. **Glossary first.** Add an entry to
   [`UBIQUITOUS_LANGUAGE.md`](../reference/ubiquitous-language.md). If the
   concept doesn't have a name yet, propose one in the RFC.
2. **Identify the pattern.** Is the new concept an aggregate root,
   a value object, or a domain event? RFCs cite which.
3. **Validator next.** If the concept is part of the workflow
   schema, the validator rejects unknown values. If it's a
   work-packet field, the build path emits it deterministically.
   If it's a verdict-time concept (RFC 0018 postures), the
   `submit-review` mutation records it.
4. **Surface in introspection.** `status`, `why`, `doctor`,
   `evidence export`, and the dashboard show the concept.
5. **CHANGELOG and DECISION_LOG cite the vocabulary entry.**

Concrete recent examples:

- RFC 0010 added `harness_profile` (value object) — glossary entry,
  validator rule, packet exposure.
- RFC 0015 added `skill bundle` and `skills manifest` (value
  objects) — glossary, install path, doctor checks.
- RFC 0018 added `review posture` (value object) and
  `required_review_postures` (build-job invariant) — glossary,
  validator rule, packet exposure, verdict recording, and introspection.
- RFC 0020 V1 added `recovery_policy` and `sweep envelope` (value
  objects) — validator rule, sweep dispatcher, doctor surface.

## What this isn't

- A justification for adding more abstractions.
- A reason to refactor existing code.
- An assertion that DDD is the only valid framing.

It's the framing the model already has. This document writes it
down so a reader can see it instead of reverse-engineering it.

## See also

- [`UBIQUITOUS_LANGUAGE.md`](../reference/ubiquitous-language.md) — the glossary;
  this document's load-bearing dependency.
- [`SPEC.md`](../reference/spec.md) — the implementation contract; what the
  runner accepts and refuses, in concrete terms.
- [`DECISION_LOG.md`](../decisions/decision-log.md) — the boundary decisions;
  D006/D009/D020/D028 are this document's load-bearing
  precedents.
- [`PRD.md`](../reference/prd.md) — the original product framing; this document
  describes how that framing maps onto the implementation.
- [`rfcs/`](../rfcs) — every RFC adds to the model in the pattern
  above.
