# Consumer-repo directory-structure recommendations

Per [RFC 0056](../rfcs/0056-consumer-repo-directory-structure-opinions.md):
**these are defaults, not enforcement.** Striatum's runtime does not
refuse repositories that ignore them. The recommendations exist so
new operators don't have to invent conventions from scratch and so
reviewers know where to look.

## At a glance

The shape:

```
target-repo/
├── README.md
├── AGENTS.md          # or CLAUDE.md — operator-facing project instructions
├── docs/
│   ├── SPEC.md            # implementation contract (RFC 0021 DDD scaffold)
│   ├── PRD.md             # product requirements
│   ├── DECISION_LOG.md    # architectural decisions (D-numbered)
│   ├── UBIQUITOUS_LANGUAGE.md
│   ├── DDD.md             # domain-driven design rationale
│   ├── rfcs/
│   │   ├── README.md
│   │   ├── 0001-template.md
│   │   └── 0NNN-*.md      # accepted + proposed RFCs
│   └── dogfood/
│       └── 001/ ...       # per-run records (optional but recommended)
├── striatum/
│   ├── workflows/
│   │   └── code-change/
│   │       └── workflow.json    # generated workflow tree
│   └── code-change/             # artifact root for that workflow
│       └── <artifact outputs land here>
└── .striatum/              # gitignored — runtime scratch only
```

## The load-bearing decisions

### `.striatum/` is runtime scratch, not the bus

Per RFC 0043 V1.6 and D094, every Striatum verb requires the daemon
and the daemon's authoritative live state is PostgreSQL. `.striatum/`
next to a target repo only holds operational scratch: supervised
wrapper FIFOs, pidfiles, transient supervisor scratch, and supervisor
stdout sinks. The daemon runtime token lives under the daemon runtime
directory as `client-token`. **Never the workflow source of truth; never an artifact
destination.**

- Should be in `.gitignore`.
- Do not write your own files here.
- Do not assume anything in `.striatum/` survives a `serve` restart.

### Workflow files live under `striatum/workflows/`

Striatum has no enforced workflow-file location, but the recommended
home is under `striatum/workflows/` next to the target repo root.
Use `striatum/workflows/<name>.json` for a single hand-authored
workflow, or `striatum/workflows/<name>/workflow.json` for a
generated workflow tree with role and prompt files. Reasons:

- Operators reading the repo see "this project orchestrates with
  Striatum" without having to grep for the file.
- Per-workflow assets (templates, snippets, prompt overrides) can
  live alongside in `striatum/workflows/<name>/`.
- The directory is target-repo-scoped, not striatum-scoped — your
  repository owns the workflow definition.

Alternative if your project already has a `workflows/` directory at
root for an unrelated system, use `striatum/workflows/` to
disambiguate. A `workflow.json` at the repo root also works for
single-workflow projects; the runner accepts any path you point it
at.

### Artifacts land under `striatum/<workflow-name>/`

Workflow JSON declares each job's `expected_artifacts[].path` and
its `write_scope.allowed_paths`. The recommended convention:

- One directory per workflow run-family, named after the workflow.
- All `expected_artifacts` paths begin with that prefix.

```
striatum/code-change/
├── prompts/
├── findings/
├── synthesis/
└── ... per-job outputs
```

This keeps run output visually grouped, makes "delete all generated
material for workflow X" a one-command operation, and avoids
sprinkling artifact-named directories across the repo.

**Alternative — `docs/<workflow-name>/`.** Some projects prefer
keeping reviewable Markdown outputs under `docs/` where reviewers
already look. That works; the trade is that `docs/` ends up mixed
between human-authored docs and AI-authored artifacts. The default
recommendation is `striatum/<workflow-name>/` because the typed
front-matter on artifacts makes the source obvious, and grouping
under `striatum/` signals "this directory is workflow output, not
hand-authored documentation."

### Decision artifacts go in `docs/DECISION_LOG.md`

Striatum records every workflow decision through the audit chain
regardless of where the Markdown body lives, but RFC 0021's DDD
scaffold ships `docs/DECISION_LOG.md` and that is the canonical
home. The format is a numbered append-only table; see Striatum's
own [`docs/DECISION_LOG.md`](../decisions/decision-log.md) for the shape.

### RFCs go in `docs/rfcs/`

If you adopt the RFC convention, use `NNNN-kebab-case-title.md` with
a header comment and `Status:` line. Current Go builds do not scaffold
these files automatically.

### `docs/dogfood/<NNN>/` for runs that produce auditable records

Striatum's own development workflow accumulates one directory per
significant run under `docs/dogfood/`, numbered sequentially. Each
directory typically contains:

- `OPERATOR_REPORT.md` — what happened, what the operator decided,
  what went wrong, what to fix in the harness.
- `workflow.json` — the workflow that drove the run (or a symlink).
- `roles/`, `prompts/` — role bundles for the run's lanes.
- `review/`, `synthesis/`, `decision/` — per-phase artifacts.

For projects doing structured multi-phase runs, replicate this. For
projects doing single-shot code changes, skip it; the audit chain
in Postgres is the durable record.

### `.gitignore` expectations

Keep `.striatum/` ignored. You decide whether the artifact roots
(`striatum/<workflow-name>/`) are committed:

- **Committed** if artifacts are durable provenance you want in
  history (typical for dogfood-style projects, RFC ledgers,
  research syntheses).
- **Ignored** if artifacts are ephemeral build output (typical for
  pure code-change workflows whose deliverable is the resulting
  source diff, not the artifact bodies).

Either choice is fine; the runner is agnostic.

## Adoption mid-life

If your repository already has its own conventions, **don't fight
them**. Striatum is generic. Concrete recommendations:

- Workflow file wherever it lives today. Pass `--workflow <path>`
  on every `striatum workflow validate` / `run prepare`.
- Artifact paths wherever your workflow JSON declares them.
- DDD docs are the cheapest of these to adopt manually: add the
  files that fit your repo and let the target repo own their content.
- For new workflow trees, run `striatum workflow generate` with
  `--scaffold-root striatum/workflows/<workflow-slug>` and
  `--artifact-root striatum/<workflow-slug>`.

The only hard requirement is `.striatum/` next to the target
repo for operational scratch; everything else is convention.

## Dogfood-heavy projects

If your project's primary output is structured runs (dogfood corpus,
RFC ledger, research synthesis ledger), the
default recommendations work but extend in two ways:

1. Add a `docs/dogfood/` index or README listing each run by number,
   workflow type, and outcome. Striatum's own
   [`docs/INDEX.md`](../index.md) cross-links to per-dogfood
   `OPERATOR_REPORT.md` headers.
2. Use the operator-on-behalf publish pattern (RFC 0051 / V1.48.1
   wrapper auth) so the operator AI can finalize phase boundaries
   without manual byline forging.

## See also

- [RFC 0021](../rfcs/0021-ddd-layout-scaffold-on-init.md) — the DDD doc
  scaffold this layout composes with.
- [RFC 0034](../rfcs/0034-workflow-generator-and-template-catalog.md) —
  the workflow generator that writes `workflow.json` files.
- [RFC 0043](../rfcs/0043-postgres-as-sole-substrate-and-daemon-required-runtime.md) — fixes `.striatum/` as runtime scratch only.
- [`docs/USING_STRIATUM.md`](../how-to/using-striatum.md) — the day-zero
  walkthrough that uses this layout in its examples.
- [`docs/WRITING_WORKFLOWS.md`](../how-to/writing-workflows.md) — how to
  author a `workflow.json` that declares the artifact paths
  described above.
