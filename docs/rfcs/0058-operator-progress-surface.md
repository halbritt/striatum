# RFC 0058: Operator Progress Surface — brief, plans, progress notes

Status: implemented
Date: 2026-05-15
Context:
[`docs/decisions/decision-log.md`](../decisions/decision-log.md),
[`docs/reference/roadmap.md`](../reference/roadmap.md),
[`docs/reference/todo.md`](../reference/todo.md),
[`docs/records/_frozen/handoffs/`](../records/_frozen/handoffs/),
[`docs/dogfood/FRICTION_LOG.md`](../dogfood/FRICTION_LOG.md),
[`docs/dogfood/<NNN>/OPERATOR_REPORT.md`](../dogfood/),
[`RFC 0017`](0017-readme-and-docs-reorganization.md),
[`RFC 0041`](0041-engram-memory-layer-for-striatum-operators.md),
[`RFC 0044`](0044-engram-phase-1-implementation-spec.md),
[`RFC 0054`](0054-day-zero-usage-guide.md),
[`RFC 0057`](0057-corpus-contract-v2.md),
`src/striatum/artifacts.py` (`ALLOWED_ARTIFACT_KINDS`, `FrontMatterSchema`),
`src/striatum/corpus/enumerator.py` (RFC 0044 V1 export).

## Problem

The operator records progress across five overlapping surfaces today, with
no shared contract and no canonical read order:

- `docs/ROADMAP.md` — opinionated sequencing + "State as of <version>"
  header; 600+ lines, updated on every `vX.Y.0` bump.
- `docs/TODO.md` — numbered status table (stable IDs) + freeform sections;
  860+ lines.
- `docs/handoffs/<date>-<slug>.md` — per-session handoff docs (current
  pattern: `2026-05-15-rfc-0048-postgres-transition.md`).
- `docs/dogfood/<NNN>/OPERATOR_REPORT.md` — per-dogfood run record
  (front matter `schema_version: striatum.operator_report.v1`,
  `artifact_kind: operator_report` — not yet in
  `ALLOWED_ARTIFACT_KINDS`).
- `docs/dogfood/FRICTION_LOG.md` — recurring failure patterns, append-on-top.

Plus ad-hoc burndown lists pasted into RFC sections (e.g. RFC 0039 V1.5
findings F1–F5, RFC 0048 V1.5 F2/F3/F4 + HIGH#1/#2), and per-handoff
"Remaining" sections that duplicate the same content with different shape.

The pains:

1. **Cold-start cost.** A new operator session must read AGENTS.md → INDEX
   → ROADMAP → TODO → most recent handoff → most recent OPERATOR_REPORT →
   sometimes FRICTION_LOG → sometimes a per-RFC V1.5 followup table. Six to
   eight files, ~1500–3000 lines, much of which is redundant or stale.
2. **No supersession.** Two handoffs from adjacent sessions disagree on
   "current state" and there's no rule that says which wins. ROADMAP §1
   ("State as of v1.48.2") goes stale the moment v1.49.0 ships, but
   nothing forces an update.
3. **No schema.** Of the five surfaces, only `OPERATOR_REPORT.md` carries
   front matter, and that kind isn't in the publisher's allowed-kinds set
   so corpus export (RFC 0044 V1) treats it as untyped.
4. **No retrieval-readiness.** RFC 0041/0044 export the operator corpus, but
   without per-document `retrieval_priority`, supersedes chains, or stable
   scope identity, optional memory/retrieval consumers will rank a 2026-03
   handoff equal to the 2026-05 brief that supersedes it.
5. **No closure.** Plans accrete ("V1.5 followup", "deferred to V2") but
   the moment a plan closes there's no canonical "this plan is done, here
   is the final shape" artifact — readers re-derive completion from
   commits + decision log + the RFC's status field.

The pattern across the five surfaces is real and load-bearing — each
serves a distinct purpose (latest-state vs. long-lived plan vs.
session-narrative vs. run-record vs. cross-cutting friction). The
problem is not that we have too many; it's that none of them has a
schema, a canonical location, a supersession rule, or a context budget.

## Goals

- Define a **small fixed set** of operator artifact kinds with V1
  front-matter schemas, validated by the existing publisher pipeline.
- Pin **canonical paths** under `docs/operator/` so a new operator's
  cold-start reading set is enumerable in one sentence.
- Add a **context budget** rule the brief must enforce on itself
  (line cap + bounded link count) so "look here and here and here" is
  structurally impossible.
- Make every kind **corpus-export-ready** (RFC 0044 V1 already enumerates
  markdown under `docs/`; we add per-kind metadata so optional
  memory/retrieval consumers can rank correctly without a code change).
- Specify **supersession semantics** so two briefs cannot both claim to
  be "current" and a retrieval query can chase the chain.
- Specify **closure semantics** for plans so a closed plan stays as
  provenance without polluting the active reading set.
- Preserve `docs/ROADMAP.md` and `docs/TODO.md` as **derived indexes**
  over the new surface, not as the surface itself.
- Preserve per-dogfood `OPERATOR_REPORT.md` as a **run record** (artifact
  of a specific run), distinct from operator state.

## Non-Goals

- A daemon-side operator-state store. Markdown files in the target
  repository remain the source of truth (per RFC 0043's product
  boundary — repo files are durable provenance).
- A memory/retrieval dependency. RFC 0041's augmentation-not-dependency rule
  holds: the operator surface must work with every optional retrieval consumer
  unavailable.
- Replacing `docs/DECISION_LOG.md`. Decisions stay there; operator state
  references decisions but does not duplicate them.
- Replacing `docs/dogfood/<NNN>/OPERATOR_REPORT.md`. Those stay as
  per-run records and get their front-matter schema pinned (item 4 below)
  but their shape and location do not change.
- New CLI verbs for editing the operator surface. Operators write
  markdown like every other artifact; the publisher validates front
  matter at `publish-artifact` time (existing path).
- A web UI surface for editing briefs/plans. Read-only rendering via
  the existing RFC 0023 `/view/` viewer is sufficient for V1.
- Mandatory operator-state tracking inside dogfood workflows. V1 ships
  as an opt-in convention for the operator; making dogfood workflows
  emit `progress_note` artifacts is a separate follow-up.

## Proposal

### 1. Three core artifact kinds (+ one ledger kind already in place)

Add to `ALLOWED_ARTIFACT_KINDS` in `src/striatum/artifacts.py`:

| Kind | Lifetime | Cardinality | Path |
|---|---|---|---|
| `operator_brief` | latest-wins, supersedes-chain | exactly one current | `docs/operator/BRIEF.md` + archive at `docs/operator/briefs/YYYY-MM-DD-<slug>.md` |
| `work_plan` | open → in_progress → closed | one per scope (RFC / phase / initiative) | `docs/operator/plans/<scope-slug>.md` |
| `progress_note` | append-only | one per session-day | `docs/operator/progress/YYYY-MM-DD-<slug>.md` |
| `operator_report` (existing, formalize) | per-run | one per dogfood | `docs/dogfood/<NNN>/OPERATOR_REPORT.md` (unchanged) |

The fourth kind (`operator_report`) is already in use; this RFC pins its
schema and adds it to the allowed-kinds set so corpus export typing is
consistent with the others.

### 2. Front-matter schemas (V1)

All schemas extend the existing `FrontMatterSchema` machinery. Required
fields are validated at publish time; unknown fields are preserved
(non-strict, matching existing kinds).

**`operator_brief.v1`:**

```yaml
---
schema_version: "striatum.operator_brief.v1"
artifact_kind: "operator_brief"
brief_id: "brief_2026-05-15_rfc-0048-postgres"        # stable id
supersedes: "brief_2026-05-14_rfc-0048-substrate"     # previous brief_id, or null for the first
scope_links:                                          # ≤ 5 entries; each MUST be a work_plan path or RFC path
  - "docs/operator/plans/rfc-0048-postgres-transition.md"
  - "docs/rfcs/0048-daemon-side-substrate-migration.md"
context_budget_lines: 300                             # see §3
retrieval_priority: "high"                           # high | normal | low
status: "current"                                     # current | superseded
---
author: operator
```

Body sections (recommended, not enforced): `## State` (what shipped /
what's running), `## Next 1–3 actions`, `## Blockers`, `## Hazards / do-not`,
`## Pointers` (the closed enumeration of links the next operator should
read).

**`work_plan.v1`:**

```yaml
---
schema_version: "striatum.work_plan.v1"
artifact_kind: "work_plan"
plan_id: "plan_rfc-0048-postgres-transition"
scope_kind: "rfc"                                     # rfc | phase | initiative | bugfix
scope_ref: "docs/rfcs/0048-daemon-side-substrate-migration.md"
state: "in_progress"                                  # open | in_progress | closed
opened_at: "2026-05-13"
closed_at: null                                       # ISO date when state=closed
closure_summary: null                                 # required when state=closed; short prose
supersedes: null                                      # plan_id, when a plan replaces an earlier shape
retrieval_priority: "normal"                         # high | normal | low
---
author: operator
```

Body: `## Outcome` (one paragraph), `## Workstreams` (table with state
column), `## Decisions made` (links into DECISION_LOG), `## Open
questions`.

**`progress_note.v1`:**

```yaml
---
schema_version: "striatum.progress_note.v1"
artifact_kind: "progress_note"
note_date: "2026-05-15"
session_slug: "rfc-0048-go-mutations"
related_plan: "plan_rfc-0048-postgres-transition"     # plan_id, or null
related_brief: "brief_2026-05-15_rfc-0048-postgres"   # brief_id, or null
retrieval_priority: "low"                            # progress notes default low
---
author: operator
```

Body: free-form append-only timestamped entries. Each entry begins with
`### HH:MM` (or `### HH:MM:SSZ` if precision matters).

**`operator_report.v1` (formalize existing):**

The current pattern (`schema_version: striatum.operator_report.v1`,
`artifact_kind: operator_report`, `author: operator`) is grandfathered.
This RFC adds `operator_report` to `ALLOWED_ARTIFACT_KINDS` and
declares the existing fields as the V1 schema; no body-shape changes.

### 3. Context budget

The `operator_brief` is the single mandatory hop for cold-start. To make
"look here and here and here" structurally impossible:

- `operator_brief` body MUST be ≤ `context_budget_lines` (default 300,
  declared in front matter so it can be tuned per-repo).
- `scope_links` MUST be ≤ 5 entries.
- Cold-start reading set is fixed: `AGENTS.md` (+ `CLAUDE.md` if present)
  → `docs/operator/BRIEF.md` → the ≤ 5 `scope_links`. Nothing else is
  required reading.
- `docs/operator/BRIEF.md` is a regular file (no symlink — Windows-safe
  per RFC 0028's portability note); rotation copies the prior contents
  to `docs/operator/briefs/<date>-<slug>.md` and writes the new brief in
  place.

V1 initially emitted a `front_matter_warning` when
`context_budget_lines` was exceeded. V1.5 promotes this to an artifact
schema error: publishing or parsing an `operator_brief` with a body
longer than its declared budget is refused.

### 4. Canonical paths and the docs/operator/ tree

Introduce one new directory:

```
docs/operator/
├── BRIEF.md                              # always the current operator_brief
├── briefs/
│   └── YYYY-MM-DD-<slug>.md              # superseded briefs (provenance)
├── plans/
│   ├── rfc-0048-postgres-transition.md   # in_progress
│   ├── rfc-0050-operator-ui.md           # closed
│   └── ...
└── progress/
    └── YYYY-MM-DD-<slug>.md
```

`docs/operator/INDEX.md` is a thin one-screen index pointing at the
current brief, the open plans, and the last seven progress notes.
Maintained by hand; the rendering belongs in the existing web UI
(`/view/` already serves this tree, no new code).

### 4.1 Current Brief CLI (V1.5)

`striatum operator current-brief [--operator-docs-root <path>] [--json]`
is a local read-only command that reads the current operator brief from
`docs/operator/BRIEF.md` by default. It returns or prints:

- `path`
- `brief_id`
- `supersedes`
- `scope_links`
- `context_budget_lines`
- `retrieval_priority`
- `cold_start_paths`

The command refuses a missing brief, a symlink, a non-regular file,
invalid `operator_brief` front matter, or a brief whose `status` is not
`current`. It is explicitly exempt from daemon-required enforcement and
daemon RPC routing because this surface is repository Markdown
provenance, not live workflow state.

V1.5 does not implement operator-tree initialization or brief rotation.

### 5. Composition with corpus export (RFC 0044 V1)

`src/striatum/corpus/enumerator.py` already walks `docs/` and emits
markdown to JSONL with redaction. This RFC adds two enumerator changes
(both small, non-breaking):

- Tag corpus rows from `docs/operator/**` with their `artifact_kind`
  field from front matter, so a retrieval ingester can rank
  `operator_brief` ahead of `progress_note`.
- Emit `retrieval_priority` and `supersedes` as first-class JSONL
  columns when present, so retrieval ranking has stable keys without
  parsing markdown.

No schema change to RFC 0044 V1's manifest is required; these are
additive fields. RFC 0057's corpus contract V2 SHOULD absorb them as
declared fields when V2 lands.

The augmentation-not-dependency rule (RFC 0041) is preserved: nothing
in `docs/operator/` is read by the daemon at runtime. Briefs and plans
are read by humans and future retrieval-backed operators only.

### 6. Supersession + closure semantics

- **`operator_brief`**: at most one row has `status: current`. When a new
  brief lands: (a) copy the old `BRIEF.md` to
  `docs/operator/briefs/<old_date>-<old_slug>.md` with its front matter
  changed to `status: superseded`; (b) write the new `BRIEF.md` with
  `supersedes: <old_brief_id>`; (c) commit both in one commit. The
  publisher MAY (V1.5) refuse to publish a second `current` brief if one
  exists; V1 leaves this to discipline + a lint rule.
- **`work_plan`**: closure requires `state: closed`, a non-null
  `closed_at`, and a non-null `closure_summary` body section. A closed
  plan is not deleted — it stays as provenance. The current brief's
  `scope_links` SHOULD NOT reference a closed plan unless explaining a
  reopen.
- **`progress_note`**: never superseded, never closed. Append-only. Old
  notes are pruned from the active reading set by the brief's
  `scope_links` discipline, not by deletion.

### 7. Deprecations, kept-as-derived

- **`docs/handoffs/`**: deprecated in V1. README pointer added that
  redirects readers to `docs/operator/BRIEF.md`. Existing handoff docs
  are not deleted — they remain provenance. New handoffs are written as
  `operator_brief` artifacts.
- **`docs/ROADMAP.md`**: kept, but reframed as a derived index over
  open `work_plan` artifacts. The §1 "State as of <version>" header is
  superseded by `docs/operator/BRIEF.md` and SHOULD be removed in V1.5
  to eliminate the dual source of "current state."
- **`docs/TODO.md`**: kept, but reframed as a stable-ID catalog over
  closed-and-open work. Status detail moves to per-plan files; the
  TODO.md table keeps only `ID | Item | Status | Plan link`.
- **`docs/dogfood/FRICTION_LOG.md`**: kept unchanged. Friction is a
  cross-cutting ledger over runs, orthogonal to operator state.
- **`docs/dogfood/<NNN>/OPERATOR_REPORT.md`**: kept unchanged
  (canonical run record). V1 adds `operator_report` to
  `ALLOWED_ARTIFACT_KINDS` and pins the V1 schema.

### 8. Phased rollout

- **V1** (this RFC's acceptance + one dogfood):
  - Add `operator_brief`, `work_plan`, `progress_note`, `operator_report`
    to `ALLOWED_ARTIFACT_KINDS`.
  - Register four `FrontMatterSchema` entries.
  - Create `docs/operator/{BRIEF.md, briefs/, plans/, progress/, INDEX.md}`
    in the Striatum repo (per §9: this is the in-repo seed; in
    *target* repos the path is configurable + collision-checked).
  - Seed `docs/operator/BRIEF.md` by porting the most recent handoff
    (`docs/handoffs/2026-05-15-rfc-0048-postgres-transition.md`).
  - Seed one `work_plan` per currently-open RFC initiative (RFC 0048
    V1.5, RFC 0050 V2, RFC 0046 V1, RFC 0047 V1, …).
  - Add deprecation pointer to `docs/handoffs/README.md`.
  - Add new tree to `docs/INDEX.md`.

- **V1.5** (landed 2026-05-18):
  - Promote `context_budget_lines` warning to error.
  - Add a `striatum operator current-brief` CLI verb (read-only — prints
    the current brief metadata and cold-start paths) so the agent harness
    can fetch the cold-start set without grepping.
  - Treat optional operator-tree init/rotation as deferred work outside
    this RFC.

- **V2** (separate RFC):
  - Move `docs/operator/INDEX.md` rendering into the web UI as a
    first-class page.
  - Optionally add `dogfood.publish_progress_note` MCP tool (RFC 0040)
    so dogfood workflows emit progress notes automatically when an
    operator-on-behalf intervention occurs.

### 9. Adoption in target repositories — collisions + configurability

The schemas + semantics in §1–§7 are repo-agnostic; the path defaults in
§4 (`docs/operator/`) collide with existing conventions in some target
repos. Striatum runs against *registered* target repositories
(RFC 0028 + RFC 0043 product boundary): a single Striatum installation
may drive several repos, each of which already has its own docs layout.
This section pins the adoption contract so initialization is
predictable and non-destructive. The wider "what does the recommended
target-repo layout look like" question stays the responsibility of
[RFC 0056](0056-consumer-repo-directory-structure-opinions.md); this
RFC owns only the operator-surface paths.

**Per-repo scope.** Operator artifacts are scoped to a single target
repository — they live inside that repo's working tree, in that repo's
git history, and (when exported) under that repo's `repository_id` in
the corpus JSONL. Nothing is shared across registered repos. An
operator who works in two repos in parallel maintains two briefs.

**Configurable root.** The default operator-tree root is
`<repo_root>/docs/operator/`. The default is overridable by:

- A `striatum.operator_docs_root` field in the target repo's
  `striatum.toml` (single source per repo; preferred), or
- A `workflow.operator_docs_root` field on `workflow.json` (per-workflow
  override — useful when a repo runs multiple workflow shapes against
  different doc roots).

When both are present, `workflow.json` wins. When neither is present,
the default applies. The resolved root MUST be inside the repo's write
scope and MUST NOT overlap `.striatum/` (RFC 0043 scratch boundary).

**Deferred collision detection at install.** A future
`striatum operator init` or rotation command should refuse to write into
a non-empty operator tree without an explicit override. The V1 in-repo seed
was committed directly in this repository, and V1.5 intentionally shipped
only the read-only `current-brief` command. The deferred write command should
follow RFC 0021's DDD-scaffold pattern:

- `--dry-run` prints the would-create paths and any pre-existing
  conflicts with `would_collide` / `would_create` status vocabulary;
  no writes.
- Plain run with no flag: refuse with exit code 4 + remediation pointing
  at `--dry-run`, `--operator-docs-root <path>`, or `--force`.
- `--force --operator-docs-root <path>`: each existing file at a
  would-overwrite path is captured by `prior_sha256` in an audit-chain
  event (`operator.tree_force_init`), then overwritten. Same shape as
  RFC 0021 V1.5's overwrite audit.

A target repo that already uses `docs/operator/` for unrelated ops
content has three clean options: (a) point Striatum at a different root
via `striatum.toml`; (b) move existing content and accept the default;
(c) `--force` after backing up. None of the three requires editing
Striatum source.

**Striatum's own repo.** Striatum-on-Striatum dogfoods use the default
root because the path is currently free; this RFC's V1 seed claims it.
This is the same self-targeting pattern as RFC 0021 (Striatum runs the
DDD scaffold against itself) — no special case needed.

**Validation.** The publisher validates `artifact_kind` + front-matter
schema regardless of which root the file lives under. The corpus
exporter (RFC 0044 V1) enumerates from `<repo_root>` and tags each row
with the resolving repo's `repository_id`; the operator-tree root is
not hard-coded in the enumerator. This keeps the schemas decoupled from
the paths and avoids a second-source-of-truth between `striatum.toml`
and Python constants.

**What is explicitly NOT a goal here.** Cross-repo brief aggregation
(one operator-state view across all registered repos) is a future
concern, likely a web-UI feature once RFC 0028's multi-repo dashboard
matures. V1 expects an operator working in repo A and repo B to read
each repo's brief separately.

## Acceptance Criteria

V1 lands when:

1. `ALLOWED_ARTIFACT_KINDS` includes the four kinds and the publisher
   validates V1 front matter for each (with the same exit code 6
   behavior used by existing schemas).
2. `docs/operator/` exists with `BRIEF.md`, `INDEX.md`, and at least
   one seeded plan per currently-open RFC initiative.
3. `docs/operator/BRIEF.md` is the latest-state authority; its
   `scope_links` ≤ 5 entries; its body ≤ `context_budget_lines`.
4. `src/striatum/corpus/enumerator.py` emits `artifact_kind`,
   `retrieval_priority`, and `supersedes` as JSONL columns when the source
   markdown has them.
5. `docs/handoffs/README.md` exists and redirects to
   `docs/operator/BRIEF.md`.
6. `docs/INDEX.md` references `docs/operator/INDEX.md`.
7. `AGENTS.md` and the Day-Zero guide (RFC 0054) reference
   `docs/operator/BRIEF.md` as the cold-start hop.
8. Corpus export regression test asserts `docs/operator/**` rows carry
   the kind + priority + supersedes columns (augmentation-boundary
   coverage already exists per RFC 0044 V1).
9. Per §9: `striatum operator current-brief` accepts
   `--operator-docs-root` for local reads. Broader `striatum.toml` /
   `workflow.operator_docs_root` precedence and collision-checked
   write initialization are deferred outside RFC 0058.

## Open Questions

1. **Multiple briefs in flight?** The model assumes one current brief.
   When two unrelated initiatives are active (e.g. RFC 0048 mutations
   work + RFC 0050 UI rework), do we want one brief that links both, or
   two briefs each with `status: current` scoped by `scope_kind`? V1
   assumes one brief; V1.5 can split if the single-brief
   shape produces information loss.
2. **Should `work_plan` be promoted to a daemon-tracked aggregate?** A
   plan with `state` and `closed_at` looks aggregate-shaped (RFC 0019
   DDD framing). V1 keeps it as markdown; if retrieval-backed operators
   need structured queries, a future RFC can mirror plan state into
   `striatumd.work_plans` without changing the markdown contract.
3. **`progress_note` cardinality.** One per session-day is the V1 rule.
   Long sessions that span midnight UTC will fragment notes; long
   sessions inside a single calendar day will accrete. Either is a
   minor irritation; revisit after one dogfood's lived experience.
4. **Retention.** Should `docs/operator/progress/` be pruned (e.g. keep
   only the last 30 days in-tree, export the rest)? V1 keeps these
   provenance files in-tree; pruning/export policy is deferred until an
   explicit corpus/archive decision or until the directory exceeds 1000
   files.
5. **`scope_links` to plans only vs. plans + RFCs + decision rows?** V1
   allows plans + RFC paths. Adding decision-log anchor links (`#D094`)
   is tempting but creates anchor-rot risk; defer.
6. **Operator-on-behalf bylines.** Briefs and plans are operator-edited
   markdown; bylines stay `author: operator`. Progress notes generated
   by a future dogfood-side MCP tool (RFC 0040) would need a model
   byline; that contract lands with the V2 MCP tool, not this RFC.
7. **`striatum.toml` first, or workflow.json only?** §9 introduces a
   `striatum.toml` field as the preferred location for
   `operator_docs_root`. No `striatum.toml` exists in the project today
   — this would be the first field. The alternative is workflow.json
   only, accepting that operators who run multiple workflows must
   duplicate the override. V1 leaves the resolution rule in place
   (`workflow.json` overrides `striatum.toml` overrides default) but
   ships only the workflow.json field; `striatum.toml` lands when
   another field demands it (likely RFC 0056 territory).
8. **Audit-chain event kind for `--force` overwrites.** §9 names
   `operator.tree_force_init`. RFC 0030's daemon event vocabulary uses
   dotted method-style names (`session.*`, `work.*`); this fits. If
   the operator tree is repo-local enough that audit-chain anchoring
   feels over-engineered, V1 may downgrade to a plain on-disk
   `prior_sha256.json` sidecar in the operator-tree root. Defer the
   call to the implementing dogfood.

## Domain Modeling

Per [`docs/DDD.md § "Adding to the model"`](../reference/domain-driven-design.md#adding-to-the-model):

- `operator_brief`, `work_plan`, `progress_note`, and `operator_report`
  are **artifacts** — repository-resident value objects whose identity
  is path + content hash, immutable once published (briefs supersede
  rather than mutate). They are NOT aggregate roots; the publisher
  remains the existing artifacts aggregate.
- The four kinds extend `ALLOWED_ARTIFACT_KINDS` (existing closed set)
  and register `FrontMatterSchema` entries — the established extension
  point for new artifact-kind validation.
- `supersedes` introduces a **directed-acyclic link** between briefs
  (and optionally between plans); this is a relation between artifacts,
  not a new aggregate. The chain is walked by readers (humans, future
  retrieval consumers), not by the daemon.
- No new boundary, no new aggregate, no new event type. Domain events
  (`artifact.published`) already cover the publication path; nothing
  about brief supersession needs runtime modeling.

This RFC is a vocabulary + path + schema addition. Its weight is in the
convention and the cold-start contract, not in code.
