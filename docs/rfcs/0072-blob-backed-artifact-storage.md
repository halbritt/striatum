# RFC 0072: Blob-Backed Artifact Storage

Status: accepted (V1 infrastructure shipped; bulk migration is operator-side per [BLOB_TRANSITION.md](../how-to/blob-transition.md))
Date: 2026-05-18
Context: [RFC 0043](0043-postgres-as-sole-substrate-and-daemon-required-runtime.md), [RFC 0044](0044-engram-phase-1-implementation-spec.md), [RFC 0046](0046-lane-evidence-guard-at-publish-artifact.md), [RFC 0066](0066-replay-archive-corpus-v2-foundations.md)

## Problem

The repository carries `docs/dogfood/<id>/` directories for every dogfood
run: 66 directories totalling roughly 2,000–3,000 markdown files of
prompts, role definitions, per-job findings, syntheses, support ledgers,
action-item ledgers, harness-improvement proposals, BUILD_HANDOFF files,
and RUN_SUMMARY files. These artifacts are **provenance and per-run
data**, not the decisional output of the project. The decisional output
— the merged source changes, the accepted RFC text, the
`DECISION_LOG.md` rows, the CHANGELOG entries — already lives in the
working tree and gets human review through normal PR diffs.

Today the per-run material competes for tree space with the deliverables
and bloats every clone, every `find`, every search-the-repo agent
session. It is also not what code review reviews; reviewers look at the
deliverable, not the scaffold that produced it.

## Goals

- Move per-run artifact bodies out of the working tree into S3-compatible
  blob storage, leaving the authoritative reference (artifact id, kind,
  byline, run/job linkage, content sha256) in daemon-owned PostgreSQL.
- Preserve every existing audit and provenance contract: append-only
  artifacts, sha256-anchored audit chain, byline integrity, redacted
  corpus export, replay-stable hashes.
- Make every per-run artifact accessible through the local web UI
  (`/artifacts/<artifact_id>`) so the human principal and AI operator
  can read them without `git pull`.
- Migrate the existing 66 dogfood directories in one bulk pass so the
  working-tree benefit lands immediately, not lazily.

## Non-Goals

- Hosted-S3 endorsement. Striatum speaks S3 API; it does not bless AWS
  or any specific provider. The operator picks the backend.
- Encryption-at-rest as a striatum feature. Operator-provided S3 already
  supports SSE; that is the right layer.
- Moving decisional artifacts out of the working tree. `decision`,
  `escalation`, `work_plan`, `operator_brief`, `operator_report` kinds
  remain git-tracked: they exist for human PR review and cold-start
  reading, not for transient run inspection.
- Encrypted-blob-as-a-feature in V1. The bucket-level controls the
  operator already has are sufficient.
- Dedupe optimization. Path-shaped keys do not dedupe; per-run artifacts
  rarely repeat. Not a performance concern at single-operator scale.

## Proposal

### Boundary

| Artifact kind | Storage |
|---|---|
| `decision`, `escalation`, `operator_brief`, `operator_report`, `work_plan` | Git-tracked. Human PR review. |
| `finding`, `synthesis`, `support_ledger`, `action_item_ledger`, `harness_improvement_proposal`, `findings_ledger`, `progress_note` | Blob (S3). |
| Per-run scaffolds: prompts, role definitions, BUILD_HANDOFF, RUN_SUMMARY | Blob (S3). |
| Source code, docs, RFCs, decision log, CHANGELOG | Git-tracked. Unchanged. |

`docs/dogfood/` is removed from the working tree post-migration and
`.gitignore` is updated to refuse re-commit.

### S3-compatible backend

The operator provides the S3-compatible service. Striatum is configured
against it, not coupled to it. **Each registered target repository gets
its own bucket**: per-repo bucket-level isolation matches the existing
`repository_id`-scoped data boundary in PG and gives teams a clear
unit for permission grants, retention policy, and bulk archive.

Daemon-global config (env or daemon config file):

| Variable | Purpose |
|---|---|
| `STRIATUM_BLOB_ENDPOINT` | Base URL, e.g. `http://localhost:9000` for local MinIO |
| `STRIATUM_BLOB_REGION` | Region, default `us-east-1` |
| `STRIATUM_BLOB_ACCESS_KEY` | Daemon's S3 access key (bucket-create + bucket-read/write) |
| `STRIATUM_BLOB_SECRET_KEY` | Daemon's S3 secret key |
| `STRIATUM_BLOB_PATH_STYLE` | Boolean; force path-style addressing for MinIO compatibility. Default `true`. |
| `STRIATUM_BLOB_BUCKET_PREFIX` | Optional prefix for auto-generated bucket names. Default `striatum-`. |

Per-repository config (stored in `striatumd.repositories`):

| Column | Purpose |
|---|---|
| `blob_bucket` | Per-repo bucket name, e.g. `striatum-<repo_slug>` |
| `blob_created_at` | Timestamp the daemon created/verified this bucket |

The daemon owns the S3 credentials globally; per-repo isolation is at
the bucket level, not the credential level. A future RFC may introduce
per-repo credentials if the threat model requires it; V1 keeps the
credential count to one.

`daemon doctor` is extended with a `blob` block: daemon-global
connectivity, plus per-registered-repo bucket existence, read/write
permission, and a sample PUT+GET round-trip. Startup refuses if a
registered repo's `blob_bucket` is unreachable.

### Adopt flow

`striatum adopt` (and `repo add --init`) gain a blob step:

1. Determine bucket name: `--blob-bucket <name>` (explicit) or
   `${STRIATUM_BLOB_BUCKET_PREFIX}<repository_id>` (default).
2. `HEAD` the bucket. If it exists and is empty, claim it. If it
   exists and contains striatum-shaped keys for a *different*
   `repository_id`, refuse with exit code 12 (`repo_blob_conflict`).
3. If it does not exist, create it with default ACL (private,
   versioning disabled) — flag-gated by `--apply-blob-creation`,
   mirroring the existing `daemon doctor --apply-migrations` pattern.
4. Record `blob_bucket` and `blob_created_at` in the
   `striatumd.repositories` row.

`daemon doctor --first-run` verifies the adopt-time bucket setup
end-to-end (round-trip read/write).

### Blob key naming

Path-shaped, human-readable, browsable in the bucket's MinIO console:

```
runs/<run_id>/jobs/<job_id>/artifacts/<logical_name>
dogfood-historical/<dogfood_id>/<original_relative_path>
```

No `repository_id` prefix — that's baked into the bucket name. The PG
row carries the canonical `blob_sha256` for integrity; the key need
not encode the hash. Browsers see filenames; integrity is checked on
read.

### Schema changes (migration 0009)

```sql
ALTER TABLE striatumd.artifacts
  ADD COLUMN blob_key         TEXT,
  ADD COLUMN blob_sha256      TEXT,
  ADD COLUMN blob_content_type TEXT;

CREATE INDEX idx_artifacts_blob_key ON striatumd.artifacts(blob_key);

ALTER TABLE striatumd.repositories
  ADD COLUMN blob_bucket      TEXT,
  ADD COLUMN blob_created_at  TIMESTAMPTZ;

-- Per-repo bucket name must be unique daemon-wide; multiple repos
-- pointing at the same bucket would defeat the isolation contract.
CREATE UNIQUE INDEX idx_repositories_blob_bucket
  ON striatumd.repositories(blob_bucket)
  WHERE blob_bucket IS NOT NULL;
```

`blob_sha256` is the authoritative integrity anchor. The existing
artifact audit-chain row already carries the content hash; `blob_sha256`
makes the same hash explicit on the artifact row for direct read paths.

A new daemon doctor check refuses to claim "production-ready" if any
artifact row has `blob_key IS NOT NULL` but the blob is unreachable,
or if any registered repo has a non-null `blob_bucket` that does not
exist on the configured S3 endpoint.

### Publish path

When an AI operator publishes a blob-routed artifact kind:

1. Operator writes the artifact body to its working tree at the path
   from the work packet (unchanged).
2. Operator calls `striatum publish-artifact --path <p> --kind <k> …`.
3. Daemon validates: front-matter (if applicable), kind, byline,
   write-scope, file existence, sha256.
4. Daemon `PUT`s the body to S3 at the canonical key for the run/job.
5. Daemon inserts the `artifacts` row with `blob_key`, `blob_sha256`,
   `blob_content_type`, and the same audit-chain row it always wrote.
6. Daemon returns success. The operator may then delete the working-tree
   file or leave it transient; the bytes-of-record live in S3.

The publish verb is idempotent on the canonical (run_id, job_id,
logical_name): a second publish refuses unless `--override-rationale` is
present (existing RFC 0046 V1 contract preserved).

### Daemon RPC

Two new methods:

- `artifact.get_content(artifact_id)` → `{content_type, body_base64,
   blob_sha256_verified}`. Daemon fetches from S3, verifies sha256,
  returns. Audit row logs the read (artifact_id, requester, timestamp)
  without body content.
- `artifact.list_for_run(run_id)` → `[{artifact_id, kind, logical_name,
   blob_key, byline, published_at, ...}]`. Index for the web UI.

`artifact.publish` is extended to upload to S3 before recording the row.
Failures (network, permission, sha256 mismatch on round-trip) refuse the
publish with a stable error code; the artifact row is not recorded.

### Web UI viewer

| Route | Purpose |
|---|---|
| `GET /runs/<run_id>/artifacts` | List run's artifacts |
| `GET /artifacts/<artifact_id>` | Render one artifact |
| `GET /artifacts/<artifact_id>/raw` | Serve raw blob body |

Server-rendered Jinja; no new React island. Markdown rendered via the
existing `markdown-it-py` pipeline used by the artifact view route.
Front-matter displayed in a metadata header above the body.

Existing `/runs/<run_id>` page gets a sidebar list of artifacts linking
to the new route.

### Historical migration

`striatum corpus migrate-historical-dogfoods` (one-shot command):

1. Walk `docs/dogfood/` for every file under every numbered run.
2. For each file:
   - Hash (sha256), read content-type from extension.
   - Determine canonical blob key:
     `striatum/dogfood-historical/<id>/<relative-path-within-run>`.
   - PUT to S3.
   - Insert/update artifact row in PG. If a row already exists for the
     logical path (e.g., from D107-era runs), update its blob_key /
     blob_sha256 in place; otherwise insert a backfill row tagged
     `provenance: 'historical_migration'`.
3. Verify: re-fetch every uploaded blob, sha256 matches the PG row.
4. On full success: `git rm -r docs/dogfood/`, append
   `docs/dogfood/` to `.gitignore`, commit.

Idempotent: a second run skips already-migrated files (matched by
canonical key + sha256). Refuses to delete from the working tree until
every backfilled row is verified.

### Corpus export

`striatum corpus export` continues to produce a redacted JSONL bundle.
Internal change: artifact bodies are fetched from S3 via the daemon's
`artifact.get_content` instead of from the filesystem. The output
contract — same JSONL shape, same redaction rules, same replay-stable
sha256 — is unchanged. Consumers (Engram, RFC 0044) see no difference.

### Replay

Replay loads events from PG, fetches artifact content from S3 by
sha256-verified `artifact.get_content`, reconstructs the run's state.
Unchanged in shape; one new dependency (blob reachability) at replay
time.

## Acceptance Criteria

- `docs/dogfood/` is removed from the working tree; `git ls-files |
  grep "^docs/dogfood/"` returns empty.
- The number of tracked files in the working tree drops by ~2,000–3,000.
- Every historical dogfood artifact is fetchable via the web UI at
  `/artifacts/<artifact_id>` and renders the original markdown.
- New `publish-artifact` calls for blob-routed kinds write to S3 and
  record `blob_key` + `blob_sha256` in PG; a `git status` after a
  successful publish shows no new staged files for that artifact.
- `daemon doctor --json` reports blob backend reachability,
  read/write permissions, and a successful round-trip.
- `make release-check` passes.
- A fresh-clone install verifies blob-backend reachability before
  declaring `adopt` successful (extension of P0-INSTALL-SMOKE).
- `striatum corpus export` produces a redacted bundle whose
  artifact-body sha256s match the pre-migration values for every
  historical run.

## Dependencies

- Go: `github.com/minio/minio-go/v7` (daemon-side S3 client).
- Python (CLI/migration script): `minio` (or `boto3`; choose `minio`
  for the lighter dependency footprint).

Both libraries are MIT-licensed and stable.

## Trade-offs

**For:**

- ~2,000–3,000 fewer tracked files. Working tree, search, and
  agent-session context all benefit.
- Sharper boundary between "code" (in repo) and "data" (in blob),
  matching the user's mental model.
- Append-only blob storage matches D008 (artifact append-only) better
  than git's mutable history.
- Team-shared MinIO supports multi-machine operators without a NAS.

**Against:**

- New dependency for adopters: an S3-compatible service alongside
  Postgres. `daemon doctor` and onboarding docs must own the install
  story.
- Loss of free `git log` / `git blame` on artifact content. Mitigated
  by the audit chain in PG and the publish-time sha256 anchor.
- Loss of free PR-review surface on artifact content. Mitigated by
  the artifact-kind split: decisional content stays git-tracked; only
  per-run data moves to blob.
- A second source of truth for "where is the artifact" (PG row +
  blob). Mitigated by daemon-mediated fetch and sha256 verification.

## Open Questions

- **Bulk migration scope**: do the operator briefs and plans under
  `docs/operator/` qualify as per-run data, or do they stay git-tracked
  as the cold-start cache? Default in this RFC: stay git-tracked. If
  the maintainer changes their mind, a follow-on RFC widens the split.
- **Bucket multi-tenancy**: resolved in favor of per-repo bucket-level
  isolation (see § S3-compatible backend). Path-prefix-in-shared-bucket
  was the cheaper alternative; bucket-per-repo wins for permission
  grants, retention policy scoping, and bulk-archive convenience at
  the cost of one extra `CreateBucket` call per adopt.
- **Encryption-at-rest as a future extension**: client-side encryption
  with operator-held keys is technically tractable but operationally
  expensive (key custody). Defer to a separate RFC once a real
  threat-model justifies it.

## Implementation Notes

- The minimum-viable V1 is one Go daemon change (S3 client + two RPC
  methods + publish hook + adopt-time bucket provisioning), one PG
  migration (covers both `artifacts` and `repositories`), one Python
  migration script, two Jinja templates, one `daemon doctor` block,
  and the `.gitignore` update. No new React islands.
- The historical migration is a one-shot; the script can be deleted
  after it lands successfully in `main`.
- `STRIATUM_BLOB_*` environment variables are loaded by the daemon at
  startup. Per-repo `blob_bucket` is recorded at adopt time in
  `striatumd.repositories`. `daemon doctor --first-run` flags missing
  daemon-global config or any registered repo whose bucket is
  unreachable.

## Reference Touchpoints

- `src/striatum/daemon_pg/handlers/workflow_loop/artifact_publish.py`
  — publish hook gains a blob upload step (or its Go equivalent).
- `go/pkg/blob/` — new package; daemon-side S3 client + RPC handlers.
- `src/striatum/blob/` — Python migration script + light client for
  fetch-from-blob paths if any survive on the Python CLI side.
- `src/striatum/web/templates/artifact_view.html` — new.
- `docs/POSTGRES_TRANSITION.md` — extended to a generic substrate doc
  covering PG + blob, or paired with a new
  `docs/BLOB_TRANSITION.md`.

## Related

- RFC 0044 (Engram corpus consumer): unchanged contract on the export
  bundle; internal implementation now reads from blob.
- RFC 0046 (lane evidence guard at publish-artifact): the publish-time
  guard runs before the blob upload; failures refuse before any S3
  traffic.
- RFC 0066 (replay/archive/corpus V2 foundations): blob storage gives
  V2 a natural body store; this RFC unblocks that work.
- D008 (append-only artifacts): preserved. Blobs are immutable by
  sha256-key plus PG anchor.
- D028 (no transcript capture by default): preserved. This RFC is
  about provenance-class artifacts, not transcripts.
