# RFC 0073: Surface RFC 0072 blob diagnostics through `striatum daemon doctor`

Status: accepted / implemented (GH #26)
Date: 2026-05-19
Context: [RFC 0072](0072-blob-backed-artifact-storage.md), [BLOB_TRANSITION.md](../how-to/blob-transition.md)

## Problem

RFC 0072 step 5 added a `"blob"` block to the Go daemon's `HandleDoctor`
RPC response (`go/pkg/reads/doctor.go:70`, `go/pkg/reads/doctor_blob.go`).
The block reports three states — `not configured`, `configured but
unreachable`, `configured and reachable` (with per-repo bucket round-trip
when `repository_id` is supplied) — and is the only operator-facing
diagnostic for the blob backend.

This block never reaches the operator's command line. `striatum daemon
doctor` does not call the Go `HandleDoctor` handler; the Python CLI's
`read_doctor_pg` (`src/striatum/daemon_pg/client_admin.py:421`) invokes a
Python-side handler (`src/striatum/daemon_pg/handlers/reads/doctor.py:
doctor_payload`) which emits a different response shape under
`daemon_diagnostics` and has no awareness of the blob configuration.

Concretely, on a v1.56.0 daemon configured for Garage:

```
$ striatum daemon doctor --json | jq '.data.daemon_diagnostics, .data.blob'
{
  "mode": "daemon",
  "problem_records": [...],
  "problems": [...],
  "protocol_version": 1
}
null
```

Discovered during the GH #22/#23/#24 operator session 2026-05-18 →
2026-05-19. Two SCOPE artifacts (synthesis kind, blob-routed per RFC
0072) silently published to repo-path only because the running daemon
binary predated RFC 0072 step 3. There was no daemon-level signal to
the operator that the blob backend was effectively off; the next
publish that *did* land in blob (#22 REVIEW) only worked because the
binary had been rebuilt between the fix and the verify. An operator
who runs `daemon doctor` should be told whether the blob backend is
configured and reachable; today they have to inspect the daemon
process environment with `cat /proc/<pid>/environ` or read individual
artifact rows to find out.

## Goals

- `striatum daemon doctor` reports the same three-state blob block as
  the Go RPC, regardless of whether the operator passes `--repo` or
  not.
- When `--repo` is supplied (or the runtime resolves a repository from
  cwd), the block also reports the per-repo bucket name, whether the
  bucket exists, and the result of the round-trip probe documented in
  `doctor_blob.go:33-90`.
- The non-`--json` form of `daemon doctor` prints a human-readable
  summary line: `blob: configured (endpoint=..., bucket=..., probe=ok)`
  or `blob: not configured` / `blob: unreachable: <error>`.

## Non-Goals

- Reorganizing the doctor handler split between Go and Python. The
  asymmetry is real (Python `doctor_payload` covers audit-chain
  invariants the Go handler doesn't), but unifying them is RFC 0048
  follow-on territory, not this RFC.
- Adding a separate `striatum daemon blob-status` verb. The information
  belongs in `daemon doctor` next to the rest of the readiness state.
- Configuring the blob backend (env vars, credentials). That is
  BLOB_TRANSITION.md territory.

## Approach

There are two natural shapes; the implementer picks one in triage:

### Option A — Python doctor delegates the blob block to Go

`doctor_payload` in
`src/striatum/daemon_pg/handlers/reads/doctor.py` issues a sub-RPC to the
Go daemon's `reads.doctor` method (or a focused
`reads.doctor_blob_block`) and merges the response under
`data.blob`. The Python handler keeps its existing audit-chain
invariants under `data.daemon_diagnostics`; the blob block lives at the
same level as `daemon_diagnostics`.

**Pros**: single source of truth (Go's `blobDoctorBlock`), no duplication
of bucket-existence and round-trip logic.

**Cons**: introduces a sub-RPC inside an already-RPC handler (doctor
calls doctor). Cycle risk if not carefully bounded.

### Option B — Python computes the blob block independently

Add a Python-side `blob_doctor_block` that:

- Reads the daemon's blob environment (already exposed through
  `daemon.config` or an analogous RPC).
- For each registered repository (or the one passed in `--repo`),
  reads `striatumd.repositories.blob_bucket`.
- Optionally probes the bucket via a tiny S3 client (minio Python
  package; only loaded if blob is configured).

**Pros**: no Go ↔ Python sub-RPC; cleaner separation.

**Cons**: duplicates the Go logic in
`go/pkg/reads/doctor_blob.go`. Two implementations of the same probe
will drift.

The triage step should decide based on (a) whether the daemon already
exposes a focused blob-status RPC the Python CLI can call without
re-invoking the full doctor, and (b) the maintainer's preference for
single-source vs. cross-language duplication.

## Acceptance

- `striatum daemon doctor --json` on a daemon with
  `STRIATUM_BLOB_ENDPOINT` set returns
  `data.blob = {"configured": true, "reachable": true, ...}` (or the
  unreachable variant).
- The same command on a daemon without `STRIATUM_BLOB_ENDPOINT` set
  returns `data.blob = {"configured": false}`.
- With `--repo`, the block additionally carries `bucket`,
  `bucket_status` (`ok` / `missing` / `not_provisioned` /
  `head_failed`), and a round-trip probe result.
- The non-`--json` form prints a one-line summary.
- A regression test (`tests/cli/test_dispatch_daemon_doctor.py` or
  equivalent) pins both the `configured: false` and `configured: true,
  reachable: true` paths.
- `make smoke` and `make pg-test` still pass.

## Provenance

Discovered while diagnosing the GH #22/#23/#24 daemon-recovery cluster
on 2026-05-18 → 2026-05-19. Two SCOPE artifacts (#22, #23) published to
repo-path only because the running daemon binary predated RFC 0072 step
3; `daemon doctor` would not have flagged the gap even with the latest
binary because the Python handler does not surface the blob block. The
two artifacts are recoverable via an artifact-backfill path (separate
follow-up); this RFC is about preventing the silent-gap pattern in the
first place.

## Related

- RFC 0072 — blob-backed artifact storage (introduces the doctor block
  on the Go side).
- RFC 0048 — Phase A/B/C handler port (the source of the Go ↔ Python
  doctor asymmetry).
- BLOB_TRANSITION.md — operator runbook for standing up the blob
  backend.
