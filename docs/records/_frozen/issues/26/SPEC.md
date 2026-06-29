# GH #26 — RFC 0073: surface blob diagnostics through striatum daemon doctor

Source: https://github.com/halbritt/striatum/issues/26
RFC: https://github.com/halbritt/striatum/blob/main/docs/rfcs/0073-daemon-doctor-blob-parity.md

## Summary

The RFC 0072 `blob` diagnostics block lives on the Go daemon's `HandleDoctor` (`go/pkg/reads/doctor.go:70`, `go/pkg/reads/doctor_blob.go`) but `striatum daemon doctor` calls a Python handler (`src/striatum/daemon_pg/handlers/reads/doctor.py:doctor_payload`) that emits a different response shape and never surfaces the blob block. Operators don't see whether the blob backend is configured or reachable.

```
$ striatum daemon doctor --json | jq '.data.blob, .data.daemon_diagnostics'
null
{
  "mode": "daemon",
  "problem_records": [...],
  "protocol_version": 1
}
```

This is the bug RFC 0073 fixes. The fix lands the blob block in the same `daemon doctor` response operators already read.

## Acceptance / Definition of done

Pinned in [RFC 0073 § Acceptance](../../../../rfcs/0073-daemon-doctor-blob-parity.md#acceptance):

1. `striatum daemon doctor --json` on a daemon with `STRIATUM_BLOB_ENDPOINT` set returns `data.blob = {"configured": true, "reachable": true, ...}` (or the unreachable variant).
2. The same command on a daemon without `STRIATUM_BLOB_ENDPOINT` set returns `data.blob = {"configured": false}`.
3. With `--repo`, the block additionally carries `bucket`, `bucket_status` (`ok` / `missing` / `not_provisioned` / `head_failed`), and a round-trip probe result.
4. Non-`--json` form prints a one-line summary (e.g. `blob: configured (endpoint=..., bucket=..., probe=ok)` or `blob: not configured` / `blob: unreachable: <error>`).
5. A regression test (`tests/cli/test_dispatch_daemon_doctor.py` or equivalent) pins both the `configured: false` and `configured: true, reachable: true` paths.
6. `make smoke` and `make pg-test` still pass.

## Approach

RFC 0073 names two options:

- **Option A — Python delegates to Go**: `doctor_payload` issues a sub-RPC to the Go daemon's `reads.doctor` (or a focused `reads.doctor_blob_block`) method and merges the response under `data.blob`. Single source of truth (Go's `blobDoctorBlock`); risks doctor-calling-doctor cycle.
- **Option B — Python computes independently**: a Python `blob_doctor_block` reads the daemon's blob environment (config RPC), the per-repo `blob_bucket`, and optionally probes the bucket. No sub-RPC; duplicates logic.

Triage picks one with concrete justification.

## Provenance

Discovered during GH #22/#23/#24 operator session 2026-05-18 → 2026-05-19. Two SCOPE artifacts silently published to repo-path because the running daemon binary predated RFC 0072 step 3; the silent-gap pattern would have been visible if `daemon doctor` reported the blob block. Backfill landed in commit `363969a` via the new `artifact.backfill_blob` RPC; this issue closes the doctor-visibility gap so future silent gaps don't accumulate.
