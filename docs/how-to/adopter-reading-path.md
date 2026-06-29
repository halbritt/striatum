# Adopter Reading Path

A six-RFC reading list for a team adopting striatum. Aimed at the
maintainer-or-tech-lead who has installed the Go binaries and now
needs to understand how the system *thinks* before authoring a
workflow or wiring an agent.

These six explain the model. The other 66+ RFCs are decision-trail and
do not need to be read first; consult them as specific questions come
up. The full RFC index lives at [`rfcs/README.md`](../rfcs/README.md).

## Read in this order

### 1. [RFC 0019 — Domain-Driven Design Foundations](../rfcs/0019-domain-driven-design-foundations.md)

**Why this matters.** The vocabulary in
[`UBIQUITOUS_LANGUAGE.md`](../reference/ubiquitous-language.md) is the model — not a
glossary, not bookkeeping. Aggregate roots, value objects, the event
log, and the daemon-method-as-write-boundary invariant are
load-bearing.

**What to take away.** A reviewer cannot return "looks good" because
the API doesn't accept it. The vocabulary is enforced by *what the API
will let you say*. Drift between code and vocabulary is a bug, not a
stylistic choice.

### 2. [RFC 0026 — Lane Attestation and Operator Byline Honesty](../rfcs/0026-lane-attestation-and-operator-byline-honesty.md)

**Why this matters.** Artifact bylines are computed at publish time
from the session's live lane-liveness attestation, not from
workflow-declared trust. "An agent published this" requires a live
process binding the runner can verify; an operator publishing on behalf
of an agent gets a different byline.

**What to take away.** Provenance honesty is mechanical, not
aspirational. If a team adopter expects byline trust to flow from
workflow config, they will be surprised; it flows from observed process
state.

### 3. [RFC 0028 — Long-Running Daemon and Multi-Repository Control Plane](../rfcs/0028-long-running-daemon-and-multi-repository-control-plane.md)

**Why this matters.** One human principal pilots 8+ AI operators
across 3+ registered repositories. The daemon owns live state and
coordinates per-repo workflow runs under a `repository_id` scope.

**What to take away.** This is *not* an embedded library and *not* a
one-repo-per-invocation runner. The shape is: one local daemon, many
registered repos, capability-token-scoped clients.

### 4. [RFC 0030 — Daemon RPC Server and Version Skew Protocol](../rfcs/0030-daemon-rpc-server-and-version-skew-protocol.md)

**Why this matters.** The RFC 0030 envelope (`schema_version`,
`request_id`, dotted `method`, `params`, `capability_token`,
`deadline_ms`) is the wire contract. CLI, MCP, and web clients all
route through it. The version-skew protocol defines how
client/daemon mismatches refuse with stable exit codes.

**What to take away.** Adding a CLI verb is fundamentally adding a
method to `contracts/daemon_methods.json` and a handler in the daemon.
The CLI is a thin client.

### 5. [RFC 0043 — PostgreSQL as the Sole Substrate and Daemon-Required Runtime](../rfcs/0043-postgres-as-sole-substrate-and-daemon-required-runtime.md)

**Why this matters.** D094 / RFC 0043 retired the V1 repo-local SQLite
substrate. The daemon is now a hard prerequisite for every striatum
verb. Three independent reasons stacked: concurrent appender
contention from 8+ ops, audit-chain row-lock semantics, and
operational ergonomics across many repos.

**What to take away.** Exit codes 11 (`daemon_unreachable`) and 12
(`repo_not_migrated`) are the daily refusal surface. Operators see
them; understand what each means before debugging.

### 6. [RFC 0053 — Human Principal as Escalation-Only Role + Terminology Truing](../rfcs/0053-human-principal-and-terminology-truing.md)

**Why this matters.** "Operator" in striatum means the AI agent
driving CLI verbs, not the human. The human principal is escalation
only — they resolve blockers the operator AI judges itself stuck on.
The same CLI surface, role-scoped by function.

**What to take away.** Workflow language was renamed to reflect this.
"Human checkpoint" is retained as a schema field name for compat, but
the actor is the operator role by default. A team adopting striatum
who reads "operator" as "human" will be confused; it is the AI.

## Where to go next

After these six, the reader has enough framing to read any other RFC
without losing context. Suggested second-tier reads, by interest:

- **Workflow authoring** → [RFC 0034](../rfcs/0034-workflow-generator-and-template-catalog.md) (generator), [RFC 0010](../rfcs/0010-tool-harness-profiles.md) (harness profiles).
- **Review semantics** → [RFC 0002](../rfcs/0002-reviewer-independence-policy.md), [RFC 0018](../rfcs/0018-focused-adversarial-review-postures.md) (postures), [RFC 0046](../rfcs/0046-lane-evidence-guard-at-publish-artifact.md) (publish-time guard).
- **Recovery and supervision** → [RFC 0009](../rfcs/0009-long-lived-process-supervision.md), [RFC 0020](../rfcs/0020-autonomous-stalled-run-recovery.md).
- **Operator skill bundles** → [RFC 0015](../rfcs/0015-self-contained-agent-skills.md), [RFC 0025](../rfcs/0025-agent-cli-plugin-bundles.md).
- **MCP** → [RFC 0040](../rfcs/0040-mcp-driven-dogfood-harness.md), [RFC 0036](../rfcs/0036-mcp-harness-for-daemon-v2-mutation-surface.md).

If a specific RFC ID is referenced in a code comment, decision row,
operator brief, or another RFC, read that one in context — don't read
linearly through `rfcs/`.
