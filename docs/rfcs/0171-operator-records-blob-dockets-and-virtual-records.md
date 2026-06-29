# RFC 0171: Operator records blob dockets and virtual records

Status: accepted (D273; partially implemented: import/materialize/verify, generated-record integrity, generated-record-backed artifact-anchor doctor, and historical generated-doc deletion pilots shipped)
Date: 2026-06-28
Context: [RFC 0072](0072-blob-backed-artifact-storage.md),
[RFC 0123](0123-blob-routed-lane-exhaust-and-git-publication-specs.md),
[RFC 0170](0170-self-culling-repository-and-cull-workflow-class.md),
the 2026-06-28 architecture review finding on provenance accumulation
([audit](../audits/STRIATUM_DEEP_ARCHITECTURE_REVIEW_GPT_5_CODEX_2026-06-28.md)).

## Problem

Striatum still tracks too many generated operator records and run-shaped
documents in git. The raw disk pressure is modest, but the architectural cost is
high: cold starts, search results, review diffs, and repo hygiene checks must
all sort through historical lane outputs, operator reports, workflow records,
and other generated bodies that are not current product source.

RFC 0072 added blob-backed artifact storage. RFC 0123 added explicit artifact
placement, including `blob_exhaust`, `git_publication`, and
`git_pointer_manifest`. RFC 0170 P0 adds read-only culling nomination, but it
does not by itself give operators a safe replacement for git-tracked run bodies.
Without a reviewable pointer surface, "move the records out of git" would lose
the affordance that made the records useful: reviewers can inspect what existed,
which hashes were accepted, and how to reconstruct the body later.

## Goals

- Store generated operator record and lane-exhaust bodies in daemon-indexed blob
  storage by default when a docket/blob route exists.
- Keep PostgreSQL as the authoritative index for each artifact or virtual
  record: repository, run, job, artifact or record id, placement, hash, blob key,
  source path, retention class, and reconstruction metadata.
- Keep git useful for review by committing small dockets and pointer manifests,
  not generated bodies.
- Make old and new records readable through `striatum://` URIs and
  materializable into ignored scratch paths with hash verification.
- Provide a read-only historical inventory and import verifier before any broad
  deletion of tracked records.
- Teach `doctor` and `check-docs` to validate the new record/docket boundary.

## Non-Goals

- Do not move accepted RFCs, source/docs changes, the decision log, the current
  operator brief, current front-door indexes, or intentional publication specs
  out of git.
- Do not require hosted storage. Blob storage remains operator-provided
  local/S3-compatible infrastructure under the RFC 0072 and RFC 0123
  local-first boundary.
- Do not delete historical tracked records in the first build unless a small
  pilot is protected by byte-identical reconstruction proof.
- Do not introduce transcript capture, provider-output warehousing, telemetry,
  or external persistence outside the existing blob-storage decision.
- Do not make blob storage the workflow state authority. Daemon-owned
  PostgreSQL remains the live state and index; blobs hold bodies.

## Proposal

### Core model

`blob_exhaust` bodies live in blob storage. Existing `artifacts` rows continue
to index new run artifacts by run, job, logical name, placement, content hash,
blob key, and repo path when present.

Add a generated-record index for run-shaped records that are not cleanly modeled
as current artifacts. A record row represents a materializable body such as a
historical operator record, dogfood file, audit supplement, or workflow-run
document. It records:

- repository id;
- `record_id`;
- original/source path and source commit when available;
- record class;
- optional run id, job id, and artifact id linkage;
- `content_sha256`, blob key, blob sha256, content type, and size;
- placement and retention class;
- import batch or bundle id;
- reconstruction metadata;
- status.

Git keeps compact dockets and pointer manifests. A docket is the
human-reviewable bridge between git history and blob-resident evidence. It must
list the records or artifacts included in a run, their placements and hashes,
the Merkle root over the docket entries, and the command needed to hydrate or
materialize the bodies.

### Dockets

Add a deterministic docket format that can render as JSON and as compact
Markdown. Docket entries include:

- `run_id`, `job_id`, artifact id or record id;
- logical name or original path;
- artifact kind or record class;
- placement;
- retention class;
- `content_sha256`;
- blob key or repo path;
- content type and size;
- `striatum://artifact/<artifact_id>` or `striatum://run/<run_id>` URI.

The Merkle root is computed over normalized entries sorted by stable identity.
The first shipped renderer is a compact review surface. The hydrate/materialize
slice will add the reconstruction command after byte-stable materialization and
hash verification are implemented.

`striatum records docket <run_id> [--format markdown|json]` is read-only. It
renders from daemon-indexed artifact/record rows and never deletes or moves
files.

### Virtual record resolver and materializer

The read surface accepts `striatum://artifact/<id>` and
`striatum://run/<id>` URIs. Resolution is daemon-backed when the daemon is
available. `check-docs` may also resolve through an explicit cached index for
offline documentation checks.

Materialization writes to ignored scratch, not tracked docs. Materialized
Markdown includes generated front matter:

- source URI;
- rendered time;
- daemon schema version;
- content hash and docket Merkle root where available;
- a warning that the materialized file is not authoritative.

Repeated materialization of the same indexed body must verify hashes and be byte
stable except for explicitly timestamped metadata.

### Placement and fail-closed posture

Generated multi-lane workflows should route ordinary lane outputs,
intermediate findings, ledgers, review notes, and other exhaust to
`blob_exhaust`. Final build specs, accepted RFC text, source/docs changes,
operator decisions, and dockets stay `git_publication` or
`git_pointer_manifest`.

If a repository, run, or workflow declares blob-required posture and blob
storage is unavailable, publishing a blob-routed body fails with a stable error
instead of silently falling back to git. Compatibility-mode workflows may retain
legacy behavior, but doctor must report the posture honestly.

### Historical inventory and import proof

Add a read-only historical inventory command for paths such as `docs/operator`,
`docs/audits`, `docs/records/_frozen`, `docs/dogfood`, and `docs/dogfoods`.
The command emits deterministic JSON with path, size, sha256, source commit,
inferred record class, proposed import mode, and a classification of
`safe_to_blob_index`, `keep_in_git`, or `manual_review`.

The import/delete gate is proof-first:

1. Capture the original manifest.
2. Create the blob/index representation.
3. Reconstruct into ignored scratch.
4. Compare reconstructed and original manifests exactly.
5. Refuse deletion unless the comparison matches.

Missing blobs, corrupt bytes, swapped blob keys, missing metadata, and bundle
ordering errors must each produce stable problem codes.

### Doctor and check-docs

`doctor --verbose --json` gains a record/docket integrity block that checks
row-to-blob reachability, content hash, Merkle root consistency, orphaned import
rows, and compatibility path lookup. Doctor output must never include artifact
body content, provider output, transcripts, blob credentials, or private blob
data.

`make check-docs` validates `striatum://artifact/...` and
`striatum://run/...` links through the daemon or an explicit cached index while
leaving ordinary local Markdown link checks unchanged. Failures name the
unresolved URI and the resolution source.

### Repo hygiene guard

Repository hygiene checks should reject newly tracked generated operator bodies
when a docket/blob route exists. The guard must whitelist accepted RFCs,
product docs, current brief/index files, intentional `git_publication` dockets,
and other source-like records.

## Build Slices

1. Schema and authority inventory for the generated-record index.
2. Pure record/docket domain package with deterministic rendering and Merkle
   root tests.
3. Read-only docket daemon method and `striatum records docket`.
4. `striatum://` resolver plus export/materialize/hydrate into ignored scratch.
5. Blob-required artifact publish posture and fail-closed tests.
6. Workflow generator defaults for explicit placement.
7. Historical inventory dry-run command.
8. Historical import and reconstruction verifier.
9. Doctor record/docket integrity extension.
10. `check-docs` resolver for Striatum URIs.
11. Repo hygiene guard for newly tracked generated record bodies.
12. Concise runbook/spec/brief updates.

Implementation state as of 2026-06-29: slices 1, 2, 3, 5, 6, 7, 8, 9, 10, 11,
and the brief/changelog/reference-doc updates in slice 12 are implemented. The
artifact-anchor doctor also accepts an indexed `generated_records` row at the
same artifact path as a historical revised-form body, and accepts an exact
path/hash match as clean, with blob health still enforced by
`generated_record_integrity`. Slice 4 is implemented for imported generated
records through `records.migration.materialize` into ignored
`.striatum/scratch`; broader `striatum://record` documentation-link resolver
coverage remains follow-up.

Two separately authorized deletion pilots have retired historical generated
Markdown bodies from git after daemon/blob reconstruction proof:

- 2026-06-28: five dogfood `OPERATOR_REPORT.md` bodies from import batch
  `inventory-d0c894978b26b00f`; pilot manifest
  `/tmp/striatum-rfc0171-deletion-pilot-manifest-2026-06-28T2231Z.json`.
- 2026-06-29: 1,755 historical operator generated Markdown bodies, split as
  500 `docs/operator/artifacts/**` records and 1,255
  `docs/operator/workflows/**` records. Import batches
  `rfc0171-bulk-artifacts-20260629`,
  `rfc0171-bulk-workflows-a-20260629`, and
  `rfc0171-bulk-workflows-b-20260629` verified and materialized with
  `checked_count=1755`, `problem_count=0`, and 0 source/manifest/materialized
  SHA mismatches. The proof manifest is
  `/tmp/striatum-rfc0171-bulk-deletion-manifest-2026-06-29T0255Z.json`; the
  mismatch report is
  `/tmp/striatum-rfc0171-bulk-sha-mismatches-2026-06-29T0255Z.tsv`.

Broad historical source deletion outside explicitly scoped generated-record
classes is still not authorized. Future deletion requires the same byte-identical
reconstruction proof and an explicit operator decision or pilot scope.

## Acceptance Criteria

- A generated multi-lane workflow can publish ordinary lane outputs to blob and
  commit only a docket or pointer manifest for review.
- `striatum records docket <id>` produces deterministic output
  with artifact ids, hashes, placement, retention class, and Merkle root.
- `records migration materialize` reconstructs imported generated records into
  ignored scratch and verifies hashes before writing.
- Historical inventory can scan the target directories and produce a
  deterministic JSON manifest without writing.
- The import/delete gate refuses deletion; `records migration verify` compares
  original and reconstructed manifests exactly and reports stable problem codes.
- Doctor catches missing, corrupt, swapped, duplicate-source, and
  metadata-missing generated-record blob/index records with stable problem
  codes.
- Doctor does not require a retired generated artifact body to remain in git
  when the artifact path is indexed as a generated record; exact path/hash
  matches are clean, while revised-form path matches are warnings. Broken
  generated-record blobs still red through the generated-record integrity block.
- `check-docs` validates `striatum://artifact/...` and `striatum://run/...`
  links through daemon or an explicit cached index.
- New generated operator provenance bodies are rejected by hygiene checks when
  a docket/blob route exists.

## Open Questions

1. Which remaining historical record classes should be promoted after the
   operator artifact/workflow Markdown batches?
2. Should cloud S3 remain accepted but discouraged, or should the operator
   documentation explicitly prefer local MinIO/Garage-compatible storage?
3. Should docket Merkle roots be anchored in a new table, an artifact row, or
   both?
4. Which record classes are durable provenance that must stay in git regardless
   of body size?

## Domain Modeling

The docket is a value object: deterministic, hashable, and reproducible from
indexed artifact/record rows. The generated-record index is a repository-scoped
read model over durable bodies, not a replacement for run/job/artifact aggregate
state. `striatum://` is a resolver boundary that exposes virtual records without
turning materialized scratch files into source truth.
