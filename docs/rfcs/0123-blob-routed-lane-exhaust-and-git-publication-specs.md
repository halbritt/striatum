# RFC 0123: Blob-routed lane exhaust and git publication specs

Status: accepted (D190; implemented)
Date: 2026-06-13
author: proposer-codex-gpt-5-001
Context: [RFC 0072](0072-blob-backed-artifact-storage.md) (blob-backed
artifact storage), [RFC 0117](0117-worktree-branch-ref-safety.md) (durable
git anchors for repo-write commits), [RFC 0118](0118-gate-run-completion-on-attested-provenance.md)
(attested completion), [RFC 0122](0122-scheduler-principal-auto-spawn.md)
(parallel RFC number, not superseded), [Blob Transition Runbook](../explanation/blob-transition.md),
and current code in `go/pkg/mutations/artifact.go`,
`go/pkg/reads/artifact_content.go`, and
`go/pkg/reads/doctor_artifact_anchor.go`.

## Problem

RFC 0072 introduced blob-backed artifact storage, but its V1 boundary is still
mostly **kind-based**: `finding`, `synthesis`, ledgers, proposals, and
`progress_note` route to blob when blob storage is configured, while
`decision`, `escalation`, `work_plan`, `operator_brief`, and
`operator_report` keep repo-path semantics. Other historical kinds still fall
back to repo paths.

That was good enough to ship blob infrastructure and start the
`docs/dogfood/` migration path, but it is not the right product model. The
distinction the operator actually wants is:

- **Lane exhaust** belongs in blob storage. These are per-lane outputs,
  intermediate findings, review notes, evidence, generated reports, and other
  run-local bodies produced so the workflow can reason.
- **Git publication specs** belong in git. These are the synthesis/build spec,
  accepted RFC text, source/docs changes, explicit owner decisions, and small
  pointer manifests that should be reviewed as source-like provenance.

The current model blurs that boundary. A `synthesis` can be the durable build
spec that should appear in a PR, or it can be an intermediate phase synthesis
that is lane exhaust. Conversely, a `handoff`, `test_report`, or
`patch_summary` can be lane exhaust even though V1 keeps those kinds
repo-path-only.

The recently added artifact-anchor doctor check is therefore a useful
transition guard, but not the final invariant. It checks that completed
repo-write artifacts with `repo_path` and `content_sha256` are present in the
durable git anchor (`run_branch` or `job_pin`). The desired end state is
different: git should contain the publication spec and blob pointers, while
blob storage contains the lane-exhaust bodies.

## Goals

1. Make **artifact placement** explicit in workflow contracts instead of
   deriving it only from `artifact_kind`.
2. Route lane exhaust bodies to blob storage by default.
3. Keep synthesis/build specs, accepted decisions, RFC text, source/docs
   changes, and pointer manifests in git.
4. Preserve Striatum's existing provenance guarantees: append-only artifact
   rows, stable `content_sha256`, byline checks, front-matter validation,
   attempt scoping, audit events, corpus export, and web/UI reads.
5. Change doctor from "every artifact body is in the git anchor" to "each
   artifact is in the correct storage class, and every pointer hash resolves."
6. Preserve the local-first boundary: S3-compatible blob storage remains
   operator-provided local infrastructure, not a hosted service requirement.

## Non-Goals

- No removal of RFC 0117's git ref-safety invariant. Repo-write commits still
  need durable `run_branch` or `job_pin` reachability before worktree release.
- No hosted storage, telemetry, external persistence service, or durable
  transcript capture.
- No immediate deletion of historical repo-path artifacts. Legacy reads remain
  supported during migration.
- No claim that every `synthesis` is git-retained or blob-routed. Placement is
  role-based: the workflow declares whether a particular artifact is publication
  spec or lane exhaust.
- No new write authority for lanes. Publishing to blob still goes through
  daemon `artifact.publish` and the existing session, lease, byline, scope, and
  front-matter checks.

## Proposal

### Artifact placement classes

Add an explicit placement class to expected artifacts and artifact rows:

- `blob_exhaust`: body of record is blob storage (`blob_key`,
  `blob_sha256`). Intended for lane exhaust: findings, reviews, ledgers, raw
  generated reports, test reports, prompts, handoffs, intermediate syntheses,
  phase scratch, and other run-local evidence.
- `git_publication`: body of record is the durable git anchor (`repo_path`,
  `content_sha256`). Intended for publication specs: final synthesis/build
  spec, accepted RFC text, owner decisions, docs/source changes, operator
  report, and other source-like records a human should review in a diff.
- `git_pointer_manifest`: body of record is the durable git anchor, but the
  body stays small. Intended for compact manifests inside the publication spec
  that list blob artifact ids, logical names, hashes, kinds, and reader links
  without embedding lane-exhaust bodies.

The placement class is a property of an expected artifact, not a synonym for
`artifact_kind`. Existing kind validation and front-matter schemas remain.

### Workflow contract

Extend `expected_artifacts[]` with a field such as:

```json
{
  "logical_name": "implementation_review",
  "kind": "finding",
  "path": "striatum/run/reviews/codex.md",
  "placement": "blob_exhaust",
  "required": true
}
```

The existing `path` remains the lane's publish input during the transition.
For `blob_exhaust`, that path is not durable repo provenance after successful
publish. For `git_publication` and `git_pointer_manifest`, the path is the
durable repo path and must resolve in the completed job's git anchor.

Compatibility default:

- If `placement` is absent, use the current RFC 0072 kind-based routing.
- New workflow generator output should write `placement` explicitly.
- New multi-lane review or design workflows should default ordinary lane
  outputs to `blob_exhaust` and reserve `git_publication` for the final
  synthesis/build spec.

### Publish path

`artifact.publish` keeps the same trust boundary:

1. Read the lane-produced file from the active worktree/source path.
2. Validate session, lease, attempt, write scope, artifact kind, byline, and
   front matter.
3. Compute `content_sha256`.
4. Route by `placement`:
   - `blob_exhaust`: upload the body to the per-repository blob bucket, record
     `blob_key`, `blob_sha256`, and `content_sha256`, and treat any `repo_path`
     as transient source metadata rather than body-of-record provenance.
   - `git_publication`: record the repo path and later require the body to be
     present at that path in the durable git anchor.
   - `git_pointer_manifest`: validate the manifest body and require it in the
     durable git anchor, but keep the manifest compact and pointer-only.
5. Emit the same append-only artifact and event records, extended with
   `placement`.

When blob storage is configured and a `blob_exhaust` artifact cannot be
uploaded or verified, publish fails and no artifact row is recorded. When blob
storage is not configured, the compatibility path may continue to preserve
repo-path semantics, but doctor must report that the repository is not in the
desired RFC 0123 posture.

### Pointer manifest

Every publication spec that summarizes lane exhaust should include a compact
machine-readable pointer block or adjacent manifest with:

- `run_id`, `job_id`, `artifact_id`, `logical_name`, and `artifact_kind`;
- `placement`;
- `content_sha256`;
- `blob_key` when placement is `blob_exhaust`;
- `repo_path` when placement is `git_publication` or `git_pointer_manifest`;
- a human-readable route such as `/v1/artifacts/<artifact_id>/raw` or the web
  UI artifact route.

The manifest is the bridge between reviewable git history and blob-resident
evidence. It lets a reviewer audit "what lane output existed and what hash was
accepted" without turning git into a warehouse for every lane body.

### Run completion

Required artifacts remain completion gates regardless of placement:

- A required `blob_exhaust` artifact is satisfied only if its blob body exists
  and verifies against `content_sha256`.
- A required `git_publication` artifact is satisfied only if the durable git
  anchor contains the file at `repo_path` with matching `content_sha256`.
- A required `git_pointer_manifest` is satisfied only if the manifest is in the
  durable git anchor and every required pointer resolves to an artifact row with
  matching hash and placement.

This keeps RFC 0118's attested-completion model intact: completion depends on
required artifacts and verdicts being present and provenance-correct, not on
where the artifact body is stored.

### Doctor posture

Replace the final target of the #217-style artifact-anchor check with a
placement-aware integrity block.

For `git_publication` and `git_pointer_manifest`, doctor checks the current
anchor invariant:

- durable anchor exists;
- file exists at `repo_path`;
- anchored file hash matches `content_sha256`.

For `blob_exhaust`, doctor checks the blob invariant:

- repository blob storage is configured, reachable, and bucket status is `ok`;
- `blob_key` and `blob_sha256` are present;
- blob body fetch verifies against `content_sha256`;
- no lane-exhaust body is accidentally present as a git-publication body in
  the durable anchor when the workflow declared `blob_exhaust`.

Stable problem families should distinguish placement failures:

- `artifact_blob_missing`
- `artifact_blob_hash_mismatch`
- `artifact_pointer_manifest_missing`
- `artifact_pointer_hash_mismatch`
- `artifact_git_publication_missing`
- `artifact_git_publication_hash_mismatch`
- `artifact_exhaust_committed_to_git`

Verbose problem records must include repository id, run id, job id, artifact
id, logical name, placement, artifact kind, content hash, blob key or repo path,
anchor kind/ref/commit when relevant, and failure reason. They must not include
artifact bodies, transcript text, provider output, or blob credentials.

### Corpus export and web reads

`artifact.get_content` already reads either blob (`blob_key`) or legacy
repo-path bodies and verifies hashes. RFC 0123 keeps that API shape but makes
`placement` visible in list/detail/export projections.

Corpus export should read artifact bodies through the same content API and
emit placement metadata. Redaction behavior is unchanged: the export contains
redacted provenance, not private blob credentials or raw transcripts.

## Migration Plan

### Phase 0 — RFC only

Accept the placement model and mark the current kind-based RFC 0072 boundary as
a compatibility default, not the target model.

### Phase 1 — Schema and validator

- Add `placement` to artifact rows and workflow expected-artifact validation.
- Teach workflow validation to reject impossible combinations, such as
  `blob_exhaust` without blob storage when the workflow requires RFC 0123
  posture.
- Teach workflow generator output to write explicit placement fields.
- Keep current kind-based routing as the backfill/default for old workflows.

### Phase 2 — Publish/read split

- Route publish by explicit placement.
- Keep legacy repo-path reads.
- Make `artifact.list_for_run`, artifact detail, run summary, and evidence
  export show placement.
- Add pointer-manifest validation for `git_pointer_manifest`.

### Phase 3 — Doctor and completion posture

- Narrow the existing artifact-anchor check to `git_publication` and
  `git_pointer_manifest`.
- Add blob integrity checks for `blob_exhaust`.
- Add posture output that reports whether a repository is still in
  compatibility mode or in RFC 0123 placement mode.

### Phase 4 — Workflow/default migration

- Update generated workflows so ordinary lane outputs are `blob_exhaust`.
- Update design/review/implementation workflow templates to keep only the
  synthesis/build spec and pointer manifest in git.
- Migrate historical `handoff`, `test_report`, `patch_summary`, and other
  repo-path-only lane-exhaust kinds where the operator chooses to opt in.

## Acceptance Criteria

- A new generated multi-lane workflow declares explicit placement for every
  expected artifact.
- In a blob-configured repository, ordinary lane outputs publish to blob and do
  not need to be present in the completed job's durable git anchor.
- A final synthesis/build spec can be declared `git_publication`; doctor
  verifies it remains present in the durable git anchor with matching
  `content_sha256`.
- A pointer manifest declared `git_pointer_manifest` is present in git and
  every pointer resolves to a blob or git artifact with matching hash.
- `artifact.get_content` and the web UI can fetch both blob-exhaust and
  git-publication artifacts through one artifact id.
- `doctor --verbose --json` reports placement-aware problem records without
  printing artifact bodies, transcripts, provider output, or credentials.
- Legacy artifacts without `placement` continue to read through existing
  repo-path/blob fallback behavior until migrated.
- The #217 artifact-anchor check no longer fails a correctly published
  `blob_exhaust` artifact just because its body is absent from git.

## Open Questions

1. Should `blob_exhaust` publish delete or ignore the transient source file
   after successful upload, or should cleanup remain workflow/operator-owned?
2. Should `git_pointer_manifest` be a new artifact kind, a placement class on
   `synthesis`, or a required front-matter section inside `synthesis`?
3. Should a repository with blob storage disabled refuse new `blob_exhaust`
   workflows, or allow compatibility repo-path fallback with a loud doctor
   warning?
4. Which existing unschemaed kinds should default to `blob_exhaust` first:
   `handoff`, `test_report`, `patch_summary`, `prompt`, `marker`, or `other`?

## Domain Modeling

This RFC introduces **artifact placement** as a value object on the artifact
contract. It is not a new aggregate root and not a new live-state authority.
The artifact aggregate remains daemon-owned PostgreSQL rows plus the selected
body store. Placement clarifies the boundary between durable reviewable
provenance (`git_publication` / `git_pointer_manifest`) and run-local evidence
(`blob_exhaust`), matching
[`docs/DDD.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model).
