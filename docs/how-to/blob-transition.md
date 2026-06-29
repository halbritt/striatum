# Blob Transition Runbook (RFC 0072)

Status: V1 shipped, operator migration pending
Date: 2026-05-18
Context: [RFC 0072](../rfcs/0072-blob-backed-artifact-storage.md), [postgres-transition.md](../how-to/postgres-transition.md)

This runbook walks the maintainer through transitioning a striatum
deployment to blob-backed artifact storage. The infrastructure
(daemon S3 client, publish/get_content/list_for_run RPC handlers,
doctor block, web UI viewer, bulk-migration script) shipped in
commits `154fac4` through `4fc41ae`. The remaining work is
operator-side: stand up an S3-compatible service, register or re-register repos
against it, then bulk-migrate the historical `docs/dogfood/`
content once the public Go CLI migration wrapper exists.

## Prerequisites

- A reachable S3-compatible service. Recommended local choice:
  [Garage](https://garagehq.deuxfleurs.fr/) or
  [MinIO](https://min.io/). Cloud S3 works too but breaks the
  local-first invariant; pick deliberately.
- The service's endpoint URL, access key, and secret key.
- A running `striatumd` (Go daemon) connected to PostgreSQL.

Garage smoke test reference (the maintainer's working deployment):

```
S3 endpoint    | http://127.0.0.1:3900 (region garage, path-style)
Access Key ID  | GK<...redacted...>
Secret         | in /root/garage-credentials.txt (0600)
```

## Step 1 — Configure the daemon

The daemon reads these environment variables at startup:

```bash
export STRIATUM_BLOB_ENDPOINT=http://127.0.0.1:3900   # required
export STRIATUM_BLOB_ACCESS_KEY=<access key>          # required
export STRIATUM_BLOB_SECRET_KEY=<secret>              # required
export STRIATUM_BLOB_REGION=garage                    # default us-east-1
export STRIATUM_BLOB_PATH_STYLE=true                  # default true; required for MinIO/Garage
export STRIATUM_BLOB_BUCKET_PREFIX=striatum-          # default striatum-
```

Restart `striatumd`. The startup log will include
`blob storage configured` when `STRIATUM_BLOB_ENDPOINT` is set.

**Credential placement gotcha**: if the daemon runs as a non-root
user but the secret is at `/root/garage-credentials.txt` (mode 0600,
root-only), the daemon cannot read it. Either move the credentials
to a path the daemon's user can read (e.g.,
`~/.config/striatum/blob-credentials` mode 0600) and `EnvironmentFile=`
them from systemd, or run the daemon as root for V1 and tighten
later.

## Step 2 — Verify with `doctor`

```bash
striatum --repo /path/to/registered/repo doctor --verbose --json | jq .blob
```

When blob storage is configured and reachable, the doctor blob block reports:

```json
{
  "configured": true,
  "reachable": true,
  "errors": []
  "bucket": "striatum-<repository_id>",
  "bucket_exists": true,
  "bucket_status": "ok",
  "round_trip_ms": 12,
  "round_trip_sha256": "...",
  "errors": []
}
```

When the bucket status is `ok`, repository-scoped doctor also runs a
placement-aware artifact integrity check for completed repo-write jobs.
`git_publication` and `git_pointer_manifest` artifacts are checked against the
durable git anchor (`run_branch` or `job_pin`) for matching file content.
Mismatches and missing files make doctor fail with stable
`artifact_anchor_hash_mismatch` or `artifact_anchor_missing_file` problems.
`blob_exhaust` artifacts are checked for blob metadata and, when blob storage is
configured, a sha-verified blob body. Missing or unreadable blob bodies produce
`artifact_blob_metadata_missing` or `artifact_blob_body_verify_failed`.
`doctor --verbose --json` includes problem records with run, job, artifact,
placement, path/hash, and anchor/blob metadata. The check does not print
artifact bodies or blob credential material.

The artifact-anchor check skips cleanly when blob storage is disabled,
unreachable, not repo-provisioned, or has any bucket status other than `ok`.
The existing blob diagnostics remain the source of truth for those setup
states.

`bucket_status: "not_provisioned"` means the repo row has NULL
`blob_bucket`; you have not re-run `striatum repo add
--apply-blob-creation` for this repo yet (step 3).

`bucket_status: "missing"` means the bucket name is set but the
bucket does not exist on the endpoint. Re-run `repo add
--apply-blob-creation`.

## Step 3 — Adopt repositories against the bucket

For a new repo:

```bash
striatum repo add /path/to/repo --init --apply-blob-creation --json
```

The `--apply-blob-creation` flag tells the daemon to create the
per-repo bucket if it does not exist. Without it, an unprovisioned
bucket refuses with `blob_apply_required`.

For an already-registered repo (no blob_bucket yet — the repo was
registered before blob was configured):

```bash
striatum repo add /path/to/repo --apply-blob-creation --json
```

The daemon detects the existing registration and backfills
`blob_bucket` on the `striatumd.repositories` row.

To explicitly choose the bucket name (instead of the default
`<prefix><repository_id>`):

```bash
striatum repo add /path/to/repo \
  --apply-blob-creation \
  --blob-bucket striatum-myrepo \
  --json
```

**Exit code 12 (`repo_blob_conflict`)** means the chosen bucket is
already claimed by a different repository (a claim marker
`_striatum_repo_marker` exists with a different repository_id) or
contains striatum-shaped keys without any claim marker. Either pick
a different bucket name or empty the conflicting bucket.

## Step 4 — Verify artifact placement

Trigger a workflow that declares `expected_artifacts[].placement`. Use
`blob_exhaust` for lane exhaust that should live in blob storage and
`git_publication` for source-like or human-reviewable records that should stay
anchored in git. Older workflows that omit placement still use the legacy kind
default: `finding`, `synthesis`, ledgers, `harness_improvement_proposal`, and
`progress_note` resolve to `blob_exhaust`; other kinds resolve to
`git_publication`. After the publish:

```bash
BASE_URL=$(sed 's#/mcp$##' "${XDG_RUNTIME_DIR}/striatum/mcp-http-endpoint")
TOKEN=$(cat "${XDG_RUNTIME_DIR}/striatum/client-token")
curl -H "Authorization: Bearer ${TOKEN}" \
  "${BASE_URL}/v1/runs/<run_id>/artifacts" | jq .
```

Expected: each artifact row reports `placement`. A `blob_exhaust` artifact
carries non-null `blob_key` and `blob_sha256` when the repository has a
provisioned blob bucket. A `git_publication` artifact may have `blob_key: null`
and must be reachable through its durable git anchor.

Fetch the raw artifact body through the Go web service:

```bash
curl -H "Authorization: Bearer ${TOKEN}" \
  "${BASE_URL}/v1/artifacts/<artifact_id>/raw" > /tmp/artifact-body
```

## Step 5 — Bulk-migrate `docs/dogfood/`

The current Go daemon registers the per-file
`corpus.migrate_historical_dogfood_file` RPC handler, but the public Go CLI no
longer exposes the old `striatum corpus migrate-historical-dogfoods` wrapper.
Do not run the retired command shape below or remove `docs/dogfood/` until a
current CLI wrapper lands.

**This step is destructive for the working tree**: after it lands,
1,305 files across 66 dogfood directories move from git tracking
into blob storage.

Before running:

- Confirm `striatum --repo <striatum repo> doctor --verbose --json | jq .blob`
  reports `bucket_status: "ok"` for the striatum repo.
- Look up the bucket name:
  `psql -c "SELECT blob_bucket FROM striatumd.repositories WHERE repo_root = '/path/to/striatum'"`.

## Step 6 — Remove `docs/dogfood/` from the working tree

Only after a current migration wrapper exists and reports zero errors:

```bash
git rm -r docs/dogfood/
# Append docs/dogfood/ to .gitignore so future runs don't re-commit.
echo "docs/dogfood/" >> .gitignore
git add .gitignore
git commit -m "RFC 0072 step 8: docs/dogfood/ migrated to blob storage"
git push
```

After this commit, the working tree drops by ~1,300 files and
all of that content lives in
`s3://<bucket>/dogfood-historical/<dogfood_id>/<rel_path>`.

## Verifying the round trip after the cutover

```bash
# Web UI viewer (blob_exhaust artifacts):
$EDITOR / web-browser → http://localhost:<port>/run/<run_id>/artifacts/<artifact_id>

# Corpus export should produce the same redacted bundle:
striatum corpus export --since <ref> --out /tmp/corpus

# Doctor stays green:
striatum --repo /path/to/striatum doctor --verbose --json | jq '.blob.bucket_status'
# → "ok"
```

## What stays in git

Per RFC 0123, placement is explicit. The following artifact roles normally use
`git_publication` because they are PR-review-shaped or durable operator records,
not lane exhaust:

- `decision` (decision log entries)
- `escalation` (human-principal blocker artifacts)
- `work_plan` (run planning artifacts)
- `operator_brief` (cold-start cache for the maintainer)
- `operator_report` (per-run summaries the maintainer reads in PRs)

Source code, RFCs, CHANGELOG, ROADMAP, the `docs/operator/` cold-start
cache, and the documentation tree itself are also git-tracked
(unchanged behavior).

## Rollback

If something goes catastrophically wrong post-migration:

- The on-disk content is in git history at the pre-migration commit.
  `git revert <step-8 commit>` restores the files.
- The blob copies are not deleted by the migration script; they remain
  in S3. A re-run after rollback skips re-uploading (idempotent).

The only durably destructive case is losing the S3 backend itself
(LUKS key loss, hosted account closure). Mitigate with periodic
`aws s3 sync` or `garage repair` backups, especially before any
re-org of the storage layer.
