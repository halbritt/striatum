# RFC 0046 — Lane evidence guard at publish-artifact

**Status:** accepted / landed
**Scope:** V1.7 (single-version)
**Closes:** GH #2, GH #5

## Background

The byline derivation at `src/striatum/identity.py::artifact_author_identity`
already differentiates on `attested` — an unattested session correctly yields
`author: operator` (never a model byline). What it does **not** verify is
that the supervised subprocess actually produced the artifact. `supervise
start` attaches a wrapper process to the session; the runner treats that as
attestation; the wrapper may exit immediately without the lane CLI emitting
any output; an operator can then write the file and call `submit-review`
on behalf and the resulting artifact lands with a model byline like
`reviewer-codex-gpt-5.5-001` even though no codex CLI produced it.

This was discovered during the v1.42.0 GH issue triage and is documented
in `~/.claude/projects/.../project_lane_attestation_gap.md`. The fix
lives at the publish-artifact layer, not the byline layer.

## Goals

- At publish-artifact time, refuse to attest a model byline unless the
  session has daemon evidence for the declared artifact path.
- Provide an explicit operator opt-out
  `--allow-no-process-execution` that records the override in the
  audit chain.
- Pin a regression test that exercises the byline forgery path and
  confirms the refusal triggers.

## Non-goals (V1.7)

- Cryptographic signature of artifact bytes (RFC 0031 future).
- Multi-process attestation (single supervisor per session today).
- Removing the operator-on-behalf flow entirely. The override path
  stays; it just becomes visibly qualified.

## Design

### Trust boundary

Daemon evidence is the authoritative record. For current supervised
wrappers, path-specific evidence is a `supervisor.artifact_observed`
event whose payload includes the artifact repo-relative path. For
legacy wrappers that have not yet emitted path observations, a clean
`process_executions` row remains a compatibility fallback.

For each artifact published under a model byline:

- If the session has any `supervisor.artifact_observed` events, at least
  one observed path must normalize to the artifact's repo-relative path.
- Otherwise, there must exist at least one `process_executions` row for
  the session with `state='exited'` and `exit_code=0`.

### Refusal path

`publish_artifact` in the daemon/Postgres workflow-loop handler:

1. After existing scope + byline + schema validation, compute the
   `expected_author_line` for `(job, session)`.
2. Parse the canonical byline: if it matches the operator template
   (`author: operator` or `author: operator [self-declared: ...]`),
   pass through. The trust gap doesn't apply.
3. If the byline is a model byline (`<role>-<model>-<ord>`), check
   path-specific supervisor observations first, then the legacy clean
   process-execution fallback.
4. If no match, raise `ArtifactError("lane_evidence_missing: artifact
   path <path> not present in any process_executions row for session
   <sid>; pass --allow-no-process-execution to override with an
   operator rationale.")`.

### Override path

`publish-artifact --allow-no-process-execution --override-rationale "..."`:

- Refuses without `--override-rationale` text (operator must record
  why).
- Writes a `provenance.publish_without_process_execution` event into
  the run's event log with the artifact id, session id, byline, and
  rationale.
- Stores the rationale on the artifact row in a new column
  `attestation_override_rationale TEXT` (schema migration in this RFC).

### Schema migration

Add to the daemon/Postgres schema:

```sql
ALTER TABLE striatumd.artifacts
  ADD COLUMN IF NOT EXISTS attestation_override_rationale text;
```

Migration 0008 carries this change. Existing artifacts continue to read
the column as `NULL`; downstream consumers treat `NULL` as "no override".

### CLI surface

```
striatum publish-artifact \
  --session-id <sid> --job-id <jid> --lease-id <lid> \
  --kind <kind> --logical-name <lname> --path <p> \
  [--allow-no-process-execution --override-rationale "<text>"]
```

The dispatch layer (`src/striatum/cli/dispatch.py::_resolve_publish_defaults`
already added in v1.41.0) chains the new override flag through to
`publish_artifact`.

### Web UI

Per `../records/_frozen/research/CLAUDE_DESIGN_UI_REWORK_PROMPT.md`:

- `LaneAttestationChip` shows `attested` / `unattested:<reason>`.
- New `LaneEvidenceChip` shows `process_execution_present` /
  `process_execution_missing` / `override:<rationale>`.
- The artifact view surfaces the override rationale prominently when
  present.

### Dashboard parity

Deferred. The landed V1 guard records override evidence on the artifact
row and provenance event; dashboard summarization can be added as a
separate visibility slice if operators need it.

## Acceptance

- `tests/daemon_pg/handlers/workflow_loop/test_publish_artifact.py`:
  - Session with path-specific `supervisor.artifact_observed` evidence
    → publish succeeds with model byline.
  - Session with observations for other paths
    → publish refuses with exit code 6 and the named error.
  - Session without any artifact-observed events but with a clean legacy
    `process_executions` row
    → publish succeeds with model byline.
  - Session without daemon evidence + model byline
    → publish refuses with exit code 6 and the named error.
  - `--allow-no-process-execution` without `--override-rationale`
    → refuses with exit code 2.
  - `--allow-no-process-execution --override-rationale "..."`
    → publish succeeds, event and artifact rationale recorded.
  - Operator-byline (no model byline) → publish passes through
    regardless of daemon evidence.
- `tests/daemon_pg/test_migration_0008_lane_evidence.py` pins the
  migration version and column.

## Migration

Existing repos: add the `attestation_override_rationale` column with
`ALTER TABLE`. Existing artifacts pass through; the guard only refuses
**new** publish-artifact calls.

## Rollout

- Post-v1.55 backlog burndown: ship PG migration 0008 + guard + override
  storage + tests.
- Exit-code register unchanged: reuse exit code 6 (artifact error) for
  refusal; no new code.

## Open questions

1. Should the guard also check `expected_artifacts[].author_line`
   directly when supplied (vs always recomputing
   `expected_author_line`)? Default: recompute, keep behavior single-
   sourced through `identity.py`.
2. For multi-process supervisor sessions (e.g. sub-agent dispatch in
   RFC 0027), do we require evidence from the parent process or any
   process? Default V1.7: any matching path-specific supervisor
   observation counts; legacy process rows are used only when no
   observations exist for the session.
