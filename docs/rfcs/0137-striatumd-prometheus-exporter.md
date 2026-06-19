# RFC 0137 — `striatumd` Prometheus Exporter

**Status:** proposed
**Scope:** Daemon observability / operational metrics surface (local-first)

> Design space explored with the `/adhd` divergent-ideation skill (5 cognitive
> frames — 3am-on-call, regulator, hostile-competitor, remove-the-assumption,
> biology — scored and converged). The three pillars below are the surviving
> branches: read-path source, an enforced cardinality/privacy contract, and a
> failure-mode-shaped taxonomy.

> **Revision — design-run hardening (2026-06-19).** This spec was driven through
> a Striatum `falsification_gate` design run
> (`run_04db6534e5590c81a7ad8c8a27b650da`). The adjudicator returned
> `needs_revision` on two material, source-grounded findings, both since confirmed
> against current `main` (and sealed by a `striatum verifier run`
> `builtin:go-build` receipt, `seal_digest 8e9eeaf1…`): **(F-A8)** the
> `doctor_problems{class}` label must be pinned to a *static* problem-code field —
> the always-returned `problems` strings carry dynamic ids in their prefix
> (`run_needs_operator.<run_id>`, `supervisor_liveness.<supervisor_id>`,
> `recovery_sweep_cursor_latch_error.<run_id>`; `doctor.go:109/147/369`) — or A3
> (value leak) and A5 (cardinality) break; and **(F-A6)** `liveness_deadline_missed`
> is a *reversible* pre-death observation (`session.liveness_recovered` exists,
> `recovery.go:1244`; `ProbeBasisDeadlineElapsedOnly`, `liveness.go:155`), not
> necrosis, so it must not enter the apoptosis/necrosis conservation law. Both are
> folded in below. Code anchors were also re-pinned to current source (the
> earlier draft had drifted). The RFC remains **proposed**; this revision answers
> the design gate, not an acceptance.

## Background

`striatumd` is the daemon-owned authority for all live workflow state (RFC 0033
/ RFC 0043): runs, lanes, sessions, jobs, leases, recovery sweeps, supervisors,
checkpoints, and artifacts, scoped per registered target repository in
daemon-owned PostgreSQL. Today an operator observes it through point-in-time CLI
verbs — `striatum status`, `striatum dashboard --once`, `striatum doctor` — and
by reading daemon logs. There is no time-series surface, so the failure classes
that actually page an operator are only visible *after* they become acute and
only by SSH-grepping logs.

The motivating incident is the phantom-supervisor reconcile storm
(`project_417_phantom_supervisor_storm`): 562 supervisors stranded `attached`
across terminal runs drove the reconcile loop into a churn storm that wedged
`striatum status` for ~30s and pinned daemon CPU at ~71%. There was no leading
indicator — the storm was inferred from the CPU aftermath. A time-series export
of the right signals (stranded-supervisor count, reconcile churn, lease-steal
rate) would have shown a ramp minutes earlier.

Prometheus is the natural fit: pull-based, local-first, file-or-localhost
scrape, no hosted dependency. But a naive in-process `/metrics` handler is a
liability on two axes the rest of this RFC must address:

1. **It can become the next outage.** A handler that runs `SELECT COUNT(*)`
   across every run on each scrape, or that takes the same mutex as the
   reconcile/recovery sweeps, turns the Prometheus poll interval into a
   self-DoS clock — precisely the contention class that wedged `status`.
2. **It is an exfiltration and cardinality surface.** Run-ids, repo filesystem
   paths, git branches/shas, agent argv/prompt fragments, and role bylines can
   all leak into label *values*; per-run / per-job / per-session IDs as labels
   explode the series count and can OOM both the daemon registry and the
   downstream Prometheus. The endpoint may be reachable on the tailnet, not
   only on `localhost`.

## Product-boundary fit

The product boundary forbids "hosted services, cloud APIs, telemetry, durable
transcript capture/export, or external persistence without an explicit product
decision." This RFC **is** that explicit decision, narrowly scoped:

- The exporter exposes **aggregate operational counts and timings**, never
  durable transcripts, artifact bodies, prompt text, or per-repo private
  content. It is observation of the *runner*, not capture of *work product*.
- Nothing is pushed anywhere. The daemon serves a local pull surface (and,
  optionally, a textfile under `.striatum/scratch/` — already classified as
  private diagnostics). The operator's Prometheus is theirs; Striatum ships no
  remote-write, no gateway, no cloud.
- This mirrors the precedent in RFC 0115 (precise token-usage telemetry), which
  kept usage accounting strictly local.

The cardinality/privacy contract (§2) is what makes this boundary *enforced in
code and CI* rather than promised in prose.

## Goals

- Expose a Prometheus-format `/metrics` surface from `striatumd` whose **scrape
  cost is O(1) and lock-disjoint from every state mutator**.
- Make the set of emittable series a **reviewable, testable contract**: no raw
  IDs as labels, bounded cardinality, and a CI test that fails closed on any
  forbidden-content leak.
- Ship a **failure-mode-shaped metric taxonomy** whose families map 1:1 to the
  real incident classes (wedged runs, phantom-supervisor storms, stale-lease /
  dead-agent thrash, liveness misses, doctor integrity problems).
- Default to `localhost` binding; reuse the existing per-repo capability
  boundary (RFC 0043) when the surface is exposed beyond loopback.

## Non-goals

- No hosted/remote metrics, push gateway, or remote-write.
- No durable metric storage inside the daemon (Prometheus owns the TSDB).
- No per-keystroke / per-agent-token streaming telemetry (that is RFC 0115's
  domain; this RFC may *reference* those counters but does not re-implement
  them).
- No Grafana dashboards bundled in this RFC (recording/alerting rules are in
  scope as version-controlled artifacts; dashboards are follow-up polish).

## Design Sketch

### 1. Read path — a lock-disjoint reconcile-tick snapshot

The exporter never queries PostgreSQL or takes a runner mutex at scrape time.

- A new `go/pkg/metrics` package defines an immutable `MetricsSnapshot` and a
  package-level `atomic.Pointer[MetricsSnapshot]` (the same lock-free
  copy-on-publish pattern already used in `go/pkg/db/write_boundary.go` and
  `go/pkg/db/authority.go`).
- The snapshot is folded **once per resident recovery sweep tick**
  (`startRecoveryScheduler`, `go/cmd/striatumd/main.go:752`, default
  `--sweep-interval-seconds 60`), reusing the rows that tick already scanned, and
  published with `.Store()` at the end of the tick.
- `/metrics` mounts into the existing daemon HTTP handler
  (`newDaemonHTTPHandler`, `go/cmd/striatumd/web_service.go:57`) next to
  `/v1/health`. The handler does exactly: `Load()` → render text → write. No PG
  round-trip, no shared mutex, so **N concurrent scrapers cost the same as
  one** and live on the `http.Server`'s own goroutines, lock-domain-disjoint
  from reconcile/recovery/status.
- Compute frequency is pinned to the tick; scrape frequency is whatever
  Prometheus wants. The two are fully decoupled.
- The snapshot's build time is exported as
  `striatum_metrics_snapshot_age_seconds`.

**Complementary cold tier (optional, later phase).** A stateless
`striatum metrics --once` rendering read-only SQL projections (or a stored-proc
catalog behind a generic `postgres_exporter` pointed at the daemon-owned DB)
keeps answering *through* a daemon hang or restart. It wins for forensic /
always-answerable rollups where freshness matters less than availability; the
in-process snapshot wins for hot operational gauges we explicitly do **not**
want a polling exporter re-deriving against the live DB. Prometheus can federate
both, and the cold tier doubles as an out-of-band liveness check on the
in-process tier.

### 2. Cardinality and privacy as an enforced, testable contract

- **Classification.** Every metric `Family` carries a `Classification`
  (`Operational` | `Provenance` | `Forbidden`). `Register()` refuses a
  `Forbidden`-classified family at construction (panic in tests, hard boot abort
  in prod) — a forbidden series can never reach the wire.
- **Enumerated labels only.** Every `LabelSpec` is a closed enumerated domain
  (run-state, `job_type`, `recovery_class`, `origin`, and a salted
  repo-surrogate `= HMAC(daemon-secret, repo_id) mod K` rendered as a small
  integer). **A raw run/job/session ID is never a label.** The salt is
  per-daemon and never exported, so surrogate buckets are stable on-box but
  carry no cross-repo-linkable meaning off-box.
- **Series budget.** A per-family LRU budget registers the first N distinct
  label-tuples; overflow collapses onto a reserved `{bucket="other"}` series and
  increments `striatum_metrics_cardinality_clipped_total{family}`, so neither the
  daemon registry nor a downstream Prometheus can be ID-bombed into OOM. The
  clip counter is itself alertable (silent dimension-loss is visible).
- **Boot-time allowlist self-audit.** Before the HTTP listener binds, a
  `metrics_allowlist` check (living beside the `go/pkg/reads/doctor_*` checks and
  mirroring the generated-route/error-catalog guardrail precedent) collects the
  sorted `(family, label_names, classification)` set, SHA-256s it, and compares
  against a checked-in `metrics_allowlist.json`. Drift fails the guardrail test
  in CI and aborts daemon startup — adding a label becomes a deliberate,
  diff-reviewed manifest edit.
- **Golden-file redaction test.** A CI test stands up a fixture daemon seeded
  with deliberately distinctive sentinel values (repo path, branch, 40-char sha,
  argv/prompt fragment, `author:` byline), scrapes `/metrics` once, asserts the
  body byte-for-byte against a committed golden, and additionally fails if the
  body matches any forbidden-content regex (filesystem paths, hex sha runs,
  branch-name shapes, prompt/argv fragments, bylines). This executable contract
  is the true backstop, because the allowlist hash only catches changed label
  *names*, not a leaky *value* under an already-allowed name.

### 3. Metric taxonomy — failure-mode-shaped

All families carry the low-cardinality `origin` enum
(`daemon-core | reconcile-sweep | supervisor | lane`), which by itself turns the
#417 phantom-supervisor storm into a directly-countable signal (a flood of
`origin="supervisor"` series and reconcile-rate spike) without a bespoke metric.

| Family | Type | Key labels | Detects |
| --- | --- | --- | --- |
| `striatum_apoptosis_total` | counter | `origin`, `reason` (closed set: `run_completed`, `job_succeeded`, `lease_handoff`, `supervisor_drained`, `session_closed_clean`) | healthy programmed self-termination — never confused with damage |
| `striatum_necrosis_total` | counter | `origin`, `reason` (the **confirmed-dead** classes only: `agent_pid_dead`, `agent_exited_unsealed`, `recovery_exhausted`) | uncontrolled death; any nonzero rate is directly alertable. **F-A6:** `liveness_deadline_missed` is deliberately **excluded** — it is reversible (see the non-terminal liveness counter below) and would make the OQ2 conservation gauge fire on a recoverable stall |
| `striatum_lease_transitions_total` | counter | `from`, `to`, `reason` | stale-lease storms, dead-agent-recovery thrash (`to="stale_lease"` rate; requeue/transfer reasons) |
| `striatum_run_wedge_age_seconds` | histogram | `origin` | wedged runs (high `_bucket` tail = time since last job-state advance while non-terminal) |
| `striatum_liveness_deadline_margin_seconds` | histogram | `origin` | distribution sliding toward zero forewarns liveness misses *before* they become necrosis |
| `striatum_liveness_deadline_events_total` | counter | `reason` (`deadline_missed`, `recovered`) | **F-A6:** the non-terminal home for `session.liveness_deadline_missed`/`session.liveness_recovered` (`recovery.go:1229/1244`). Outside the apoptosis/necrosis conservation law — a reversible stall moves these, never `necrosis_total` |
| `striatum_doctor_problems` | gauge | `class` (**static** problem-code only — `problem_records[*].check`, never the dynamic-id `problems` prefix) | a red `doctor` becomes a paging stop-and-fix alert, not a manual CLI run |
| `striatum_pg_pool_inuse` / `..._max`, `striatum_pg_query_seconds` | gauge / histogram | `query_class`, salted `repo` | PG-boundary saturation that localizes a saturating repo |

The **apoptosis/necrosis split is the spine**: programmed self-termination and
pathological death share the same terminal DB transition, so the distinction
must be tagged *at the code site that ends the lifecycle* (the terminator
declares intent and emits `apoptosis`; only the recovery/liveness paths that
detect an unannounced exit emit `necrosis`).

`origin`, the `apoptosis`/`necrosis` `reason` sets, and `recovery_class` are
**new, CREATE-defined closed Go enums** — they do **not** exist as source
constants today (the earlier draft's "wired to existing constants" claim was
false; F-A6/anchor verification). The new enums are *anchored to* the real
constants that do exist and pinned by a guardrail test:

- **necrosis `reason`** is the union of the **confirmed-dead** stall classes —
  `agent_pid_dead`, `agent_exited_unsealed` (`go/pkg/mutations/recovery_decision_tree.go`,
  which explicitly separates these from the *reversible* `sessionliveness.*`
  protocol stalls, ~line 146) plus the `recovery_exhausted` blocker kind
  (`go/pkg/mutations/recovery_escalation.go`). The guardrail test asserts the
  necrosis domain equals exactly that set, so a new stall class cannot silently
  appear in (or `liveness_deadline_missed` re-enter) the necrosis label.
- **doctor `class`** is the set of **static `problem_records[*].check` codes**
  emitted by the `go/pkg/reads/doctor_*.go` checks — never the always-returned
  `problems` prefix strings, which interpolate run/gate/supervisor ids
  (`doctor.go:109/147/369`, `doctor_quorum.go`). The guardrail test seeds
  adversarial run/gate ids and asserts no dynamic id ever reaches `class` and the
  series count stays constant (see Acceptance Criteria).

### 4. Binding, auth, and consent

- **Default `localhost`.** The surface binds loopback unless the operator opts
  into a wider bind, consistent with the existing daemon HTTP boundary.
- **Capability-scoped when exposed.** When reachable beyond loopback, `/metrics`
  requires the bearer's per-repo capability (RFC 0043) and filters the response
  to families/series for repos that token authorizes — a tailnet scraper holding
  only repo-A's token cannot see repo-B's surrogate buckets. This reuses the
  same scope boundary that gates RPC, rather than inventing a parallel ACL.
- **Opt-in provenance consent.** `Provenance`-classified families for a repo
  register only when an explicit per-repo product-decision flag (persisted in
  the daemon-owned DB) is set; `striatum_metrics_repo_consent{bucket}` exposes
  the consent state so its *absence* is itself a scrapeable fact. `Operational`
  families default on; `Provenance` defaults off per repo.

## Roadmap

Each phase leaves the tree shippable and fails closed when the contract is
violated. Build the safety/contract harness **first** (TDD), then the taxonomy.

### Phase A — read-path skeleton + redaction harness (contract-first)
- `go/pkg/metrics` package: `MetricsSnapshot` + `atomic.Pointer`, published from
  the sweep tick; `/metrics` mounted in `newDaemonHTTPHandler`;
  `snapshot_age_seconds` gauge; `localhost` bind.
- Seed metrics: stranded-supervisor count, run-state counts, `builtAt`.
- The **failing** golden-file/forbidden-regex redaction test lands here first
  and defines the exfiltration contract.
- Unit test asserts the handler issues zero DB queries (inject a runner whose
  `Query` panics) and that 1000 concurrent scrapes return the identical
  snapshot pointer.

### Phase B — failure-mode taxonomy
- `go/pkg/metrics/taxonomy.go`: closed `Origin` / `*Reason` enums wired to
  source constants + the union guardrail test.
- `apoptosis_total`, `necrosis_total`, `lease_transitions_total`,
  `run_wedge_age_seconds`, `liveness_deadline_margin_seconds`. Emit
  apoptosis/necrosis at the lifecycle-termination code sites.

### Phase C — contract enforcement + doctor-as-collector
- `Classification` taxonomy + `Register()` refusal of `Forbidden`.
- Per-family series budget + `cardinality_clipped_total`.
- Boot-time `metrics_allowlist.json` hash check (guardrail test + boot abort).
- `doctor_problems{class}` gauge sourced from the **static
  `problem_records[*].check` codes** of the existing `reads/doctor_*` checks
  (never the dynamic-id `problems` prefix), on a bounded cadence (not on every
  scrape). Ship `TestDoctorClassRejectsDynamicIdentifiers` (F-A8) in this phase.

### Phase D — multi-tenant hardening, consent, alert rules
- Capability-scoped `/metrics` filtering by authorized repos.
- Opt-in `metrics_repo_consent` gauge gating `Provenance` families.
- `snapshot_age_seconds` staleness SLI alert; publish on partial/errored ticks
  with a `tick_status` label.
- Ship version-controlled Prometheus recording + alerting rules
  (`NecrosisRate`, `DoctorRed`, `WedgeAgeTail`, `LivenessMarginCollapse`,
  `SupervisorOriginFlood`) next to the exporter.
- Optional: cold DB-projection tier (`striatum metrics --once`) for federation.

## Acceptance Criteria

- `/metrics` serves valid Prometheus text with **zero** PG queries and **zero**
  shared-mutex acquisition at scrape time (proven by a panic-on-query test and a
  concurrent-scrape identity test).
- The golden-file/forbidden-regex test passes and is wired into `make check`;
  the boot-time allowlist hash matches the checked-in manifest.
- Each family in §3 exists with closed-enum labels pinned to source constants by
  a guardrail test; cardinality cannot grow with the number of runs/jobs.
- **`TestDoctorClassRejectsDynamicIdentifiers` (F-A8):** seeds quorum/recovery
  failures with adversarial run/gate ids, scrapes `/metrics`, and asserts only
  static `problem_records[*].check` codes appear as `class`, no dynamic id leaks,
  and the series count stays constant.
- **`TestLivenessMissCanRecoverWithoutNecrosis` (F-A6):** drives
  `active → liveness_deadline_missed → liveness_recovered` and asserts
  `striatum_liveness_deadline_events_total` moved, `striatum_necrosis_total` did
  **not** increment, and the OQ2 lifecycle-balance gauge stayed zero.
- A reconstructed #417-shaped fixture produces a visible `origin="supervisor"`
  ramp on the relevant families.

## Open Questions

1. **Snapshot staleness as a liar.** Because the read path is decoupled, a
   wedged reconcile loop lets scrapers serve last-good numbers while the daemon
   dies. Is `snapshot_age` + publish-on-errored-tick sufficient, or does the
   cold DB-projection tier need to be in-scope from Phase A as the out-of-band
   liveness check?
2. **`lifecycle_balance` as a conservation law.** If every lifecycle
   termination must emit exactly one apoptosis-or-necrosis event, a persistent
   gap (`terminal_count != apoptosis+necrosis`) is a provable blind spot in the
   runner — making the metrics layer a *second doctor*, a continuously-scraped
   integrity assertion. Promote this from a child idea to a first-class family?
3. **Cold-tier authentication.** A generic `postgres_exporter` pointed at the
   daemon-owned DB introduces a second principal under the RFC 0110 PG-auth
   boundary. Worth the operational coupling, or keep the cold tier as a daemon
   verb (`striatum metrics --once`) only?
4. **Event-sourced replay.** Reconstructing counter trajectories from durable
   history (`striatum metrics replay --since`) is demoted to forensic export
   here (Prometheus already tolerates counter resets). Is a one-shot historical
   exposition worth a later slice?
5. **`flora_diversity` Shannon index.** A single aggregate vital sign over the
   live lane/state mix catches monocultures (everything `stale_lease`, all
   `origin="supervisor"`) before any individual run wedges — but needs a
   defensible alert threshold. In or out for V1?

## Domain Modeling

Per `docs/explanation/domain-driven-design.md § "Adding to the model"`, the
exporter is a **boundary clarification plus a read-model projection**, not a new
aggregate root. The `MetricsSnapshot` is a derived value object computed from
existing aggregates (runs, leases, supervisors, jobs) at a reconcile tick;
`apoptosis`/`necrosis` are domain events emitted at existing lifecycle-transition
sites, given metric form. No new write boundary is introduced — the daemon
method vocabulary and the PostgreSQL substrate remain the sole authority, and the
exporter is a strictly read-only observer of them.
