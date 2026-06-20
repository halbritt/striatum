# Design-Run Seed — RFC 0137 `striatumd` Prometheus Exporter

> This document is the **required input** for the RFC 0137 design run. It is
> operator-supplied design-run scaffolding, not the canonical RFC (the canonical
> proposal lives on review branch `rfc/0137-striatumd-prometheus-exporter` / PR
> #450 and stays there until accepted). Read this whole file before producing any
> artifact.

## Charter — what this run must produce

This is a **design run**, not an implementation run. The deliverable is a
**falsifiable implementation spec** for RFC 0137: a concrete, buildable
specification that an impl-run can execute contract-first (TDD), produced by
hardening the RFC proposal against adversarial falsification.

The committed proposal (`SPEC.md`) MUST:

1. **Resolve all five Open Questions** (listed below) with a concrete, defensible
   decision for each — *in V1 / deferred*, *which mechanism*, and *why*. A design
   run that leaves an Open Question unresolved has not cleared the gate.
2. **Build against ACCURATE current source.** The RFC's code anchors have
   **drifted** (see "Anchor verification" below). Re-anchor every claim to the
   real file:line in current `main`. Where the RFC asserts an enum/constant
   "wires to existing source constants" that does **not** exist yet, say so
   explicitly and specify whether the spec *creates* that enum or *aligns* to
   the scattered constants that do exist.
3. **State every load-bearing claim as a falsifiable assertion** paired with the
   named test/check that would prove it false. The three pillars to harden:
   - **Pillar 1 (read path):** "scrape cost is O(1) and lock-disjoint from every
     state mutator." Falsifiable how? (the panic-on-query test + concurrent-scrape
     identity test the RFC names — specify them precisely).
   - **Pillar 2 (privacy/cardinality contract):** "no raw IDs as labels, bounded
     cardinality, fails closed on any forbidden-content leak." Falsifiable how?
     (golden-file + forbidden-regex redaction test; boot-time allowlist hash).
   - **Pillar 3 (failure-mode taxonomy):** apoptosis/necrosis split tagged at the
     lifecycle-termination code site; labels pinned to source-of-truth constants
     by a guardrail test.
4. **Stay inside Striatum's product boundary.** Local-first: no hosted services,
   cloud APIs, push gateway, remote-write, or external persistence. The exporter
   is pull-only and must not leak per-repo private data — that constraint is the
   whole point of Pillar 2. The RFC frames itself as the explicit product
   decision authorizing a *narrowly-scoped local pull surface*; keep it narrow.
5. **Keep the phased roadmap shippable.** Each phase must leave the tree
   shippable and fail closed when the contract is violated; the contract/redaction
   harness is built first (Phase A, contract-first).

Falsifiers: attack the load-bearing claims above. The strongest challenges are
(a) anchor/enum claims that do not hold against current source, (b) a redaction
contract that an adversarial label *value* (not name) can still leak through,
(c) an O(1)/lock-disjoint claim that a real code path violates, and (d) an Open
Question "resolution" that is actually hand-waving. Adjudicator: gate on whether
a *material* challenge landed and was *directly* rebutted.

---

## RFC 0137 — proposed text (verbatim, for reference)

# RFC 0137 — `striatumd` Prometheus Exporter

**Status:** proposed
**Scope:** Daemon observability / operational metrics surface (local-first)

> Design space explored with the `/adhd` divergent-ideation skill (5 cognitive
> frames — 3am-on-call, regulator, hostile-competitor, remove-the-assumption,
> biology — scored and converged). The three pillars below are the surviving
> branches: read-path source, an enforced cardinality/privacy contract, and a
> failure-mode-shaped taxonomy.

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
  (`newDaemonHTTPHandler`, `go/cmd/striatumd/main.go:413`) next to
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
| `striatum_necrosis_total` | counter | `origin`, `reason` (`liveness_deadline_missed`, `agent_exited_unsealed`, `recovery_exhausted`, `worktree_lost`, `panic`) | uncontrolled death; any nonzero rate is directly alertable |
| `striatum_lease_transitions_total` | counter | `from`, `to`, `reason` | stale-lease storms, dead-agent-recovery thrash (`to="stale_lease"` rate; requeue/transfer reasons) |
| `striatum_run_wedge_age_seconds` | histogram | `origin` | wedged runs (high `_bucket` tail = time since last job-state advance while non-terminal) |
| `striatum_liveness_deadline_margin_seconds` | histogram | `origin` | distribution sliding toward zero forewarns liveness misses *before* they become necrosis |
| `striatum_doctor_problems` | gauge | `class` (the seven known integrity classes) | a red `doctor` becomes a paging stop-and-fix alert, not a manual CLI run |
| `striatum_pg_pool_inuse` / `..._max`, `striatum_pg_query_seconds` | gauge / histogram | `query_class`, salted `repo` | PG-boundary saturation that localizes a saturating repo |

The **apoptosis/necrosis split is the spine**: programmed self-termination and
pathological death share the same terminal DB transition, so the distinction
must be tagged *at the code site that ends the lifecycle* (the terminator
declares intent and emits `apoptosis`; only the recovery/liveness paths that
detect an unannounced exit emit `necrosis`). `origin` and `reason` are closed Go
enums wired to existing source constants (necrosis reasons ↔
`go/pkg/reads/escalation_resolve.go`; doctor classes ↔
`go/pkg/reads/doctor_artifact_anchor.go`, `worktree_refs.go`) and pinned by a
guardrail test asserting the metric label set equals the union of those
source-of-truth constants.

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
- `doctor_problems{class}` gauge sourced from existing `reads/doctor_*` checks
  on a bounded cadence (not on every scrape).

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

---

## Open Questions to resolve (design calls — restate the RFC's five)

1. **Snapshot staleness as a liar.** Is `snapshot_age` + publish-on-errored-tick
   sufficient, or must the cold DB-projection tier be in Phase A as the
   out-of-band liveness check? Decide and justify.
2. **`lifecycle_balance` as a conservation law.** Promote
   `terminal_count == apoptosis + necrosis` to a first-class, continuously-scraped
   integrity family (a "second doctor"), or keep it a child idea? Decide.
3. **Cold-tier authentication.** A generic `postgres_exporter` introduces a second
   principal under the RFC 0110 PG-auth boundary. Worth the coupling, or keep the
   cold tier as a daemon verb (`striatum metrics --once`) only? Decide.
4. **Event-sourced replay.** Is a one-shot historical exposition
   (`striatum metrics replay --since`) worth a later slice, or is forensic export
   enough (Prometheus tolerates counter resets)? Decide in/out for the roadmap.
5. **`flora_diversity` Shannon index.** A single aggregate vital sign over the live
   lane/state mix — in or out for V1, and if in, what defensible alert threshold?

---

## Anchor verification against current `main` (operator pre-flight, 2026-06-19)

The RFC's code anchors were verified against current source. Treat these as
ground truth; the spec must re-anchor to them.

| RFC claim | Status | Correction (current source) |
| --- | --- | --- |
| `startRecoveryScheduler` @ `go/cmd/striatumd/main.go:752` (sweep tick, default `--sweep-interval-seconds 60`) | **ACCURATE** | Confirmed at `main.go:752`; called at `main.go:331`; flag at `main.go:80`. The once-per-tick snapshot publish point is valid. |
| `newDaemonHTTPHandler` @ `go/cmd/striatumd/main.go:413`, `/v1/health` mounted there | **DRIFTED** | `newDaemonHTTPHandler` is at `go/cmd/striatumd/web_service.go:57` (NOT `main.go:413`). `/v1/health` IS real (handled in `go/pkg/webservice/`). Re-anchor the `/metrics` mount point to `web_service.go:57`. |
| `atomic.Pointer` lock-free copy-on-publish in `go/pkg/db/write_boundary.go` and `authority.go` | **ACCURATE** | `write_boundary.go:48` `var activeWriteBoundary atomic.Pointer[WriteBoundaryPhase]` (Store/Load); `authority.go:29` `var authorityRuntime atomic.Pointer[authorityRuntimeState]`. The pattern the snapshot reuses is real. |
| "necrosis reasons" wired to `go/pkg/reads/escalation_resolve.go` (`liveness_deadline_missed`, `agent_exited_unsealed`, `recovery_exhausted`, `worktree_lost`, `panic`) | **DRIFTED** | Source is NOT `escalation_resolve.go`. Stall classes live in `go/pkg/mutations/recovery_decision_tree.go:150-158` (`agent_pid_dead`, `agent_exited_unsealed`); `recovery_exhausted` is a *blocker kind* in `recovery_escalation.go:15`; `liveness_deadline_missed` is used as an *event type*, not a stall-class constant; **`worktree_lost` and `panic` do NOT exist as constants.** The spec must decide: introduce a consolidated necrosis-reason enum, or align labels to the scattered real constants — and pin the guardrail test to whatever actually exists. |
| "seven known doctor integrity classes" in `doctor_artifact_anchor.go` / `worktree_refs.go` | **DRIFTED** | No discrete set of seven. `doctor_artifact_anchor.go:17-32` defines 8 artifact codes (4 red: `artifact_anchor_hash_mismatch`, `artifact_anchor_missing_file`, `artifact_blob_metadata_missing`, `artifact_blob_body_verify_failed`; 4 warning incl. `artifact_superseded_on_default_branch`, `artifact_acknowledged_loss`). Many more problem classes are scattered across 10+ `doctor_*.go` files. The `doctor_problems{class}` gauge must enumerate the REAL class set (and the guardrail test must source it from the actual checks), not a fictional seven. |
| RFC references 0115 (token telemetry), 0110 (PG-auth), 0043 (per-repo capability), 0033 (storage substrate) | **ACCURATE** | All four RFC files exist under `docs/rfcs/` and are correctly characterized. Precedent (0115 local-only telemetry; 0043/0110 capability+PG-auth boundary the cold tier and capability-scoped `/metrics` must respect) holds. |
| `origin` enum (`daemon-core`/`reconcile-sweep`/`supervisor`/`lane`) and `recovery_class` constants "are closed Go enums wired to existing source constants" | **DRIFTED** | No Go `origin` enum exists today (the values are doc strings in §3). No systematic `recovery_class` enum either (only `recovery_exhausted` blocker kind + the stall classes above). These enums are **NEW** — the spec must define them and decide what existing constants (if any) they map to, then pin them with the union guardrail test it proposes. |

**Net design implication:** the RFC's headline mechanism (tick-published
`atomic.Pointer` snapshot; lock-disjoint O(1) `/metrics`) rests on **accurate**
anchors. The drift is concentrated in the *taxonomy's* claim that its enums
already exist as source constants — they largely do **not**. The spec must
either define them as new closed enums (and wire the guardrail test to the
real, scattered termination/doctor constants) or scope the taxonomy to what the
union guardrail can actually pin. Falsifiers should press exactly here.
