#!/usr/bin/env python3
"""Unattended DoD driver: drive a multi-lane, review-gated, revision-capable run
to completion with ZERO operator rescue, N times consecutively.

"Unattended / zero rescue" = the driver delegates lane reconciliation to
`striatum run drive`, which uses normal lane lifecycle verbs and never calls a
rescue verb (requeue-stale, override-verdict, retry-job, force). A run that reaches
`completed` this way is a clean pass; a run that hits needs_operator/failed (the
daemon escalated -> a human would be needed) is a FAIL.

Per RFC 0105's outside-CI acceptance: the standing proof of "no human intervention".
"""
import json
import subprocess
import sys

REPO = sys.argv[1] if len(sys.argv) > 1 else "/tmp/striatum-floor-acceptance"
WORKFLOW = sys.argv[2] if len(sys.argv) > 2 else "workflow.json"
N = int(sys.argv[3]) if len(sys.argv) > 3 else 1
RUN_TIMEOUT = 1500   # 25 min per run (draft -> review -> optional revision cycle)
POLL = 15


def cli(*args, check=True):
    """Run a striatum CLI verb scoped to REPO; return parsed JSON (or None)."""
    cmd = ["striatum", "--repo", REPO, *args]
    p = subprocess.run(cmd, capture_output=True, text=True)
    out = (p.stdout or "").strip()
    if p.returncode != 0 and check:
        return {"_error": (p.stderr or out).strip(), "_rc": p.returncode}
    try:
        return json.loads(out)
    except Exception:
        return {"_raw": out, "_err": (p.stderr or "").strip(), "_rc": p.returncode}


def run_state(summary):
    r = summary.get("run")
    if isinstance(r, dict):
        return r.get("state")
    return summary.get("state")


def drive_run(run_id):
    """Drive one run to terminal state through the product run-drive loop.
    Returns (ok, reason). ok=True iff the run reached `completed`."""
    cmd = [
        "striatum", "--repo", REPO,
        "run", "drive",
        "--run-id", run_id,
        "--interval", str(POLL),
        "--json",
    ]
    try:
        result = subprocess.run(cmd, text=True, timeout=RUN_TIMEOUT)
    except subprocess.TimeoutExpired:
        return False, "run timeout"
    if result.returncode == 0:
        return True, "completed"
    summary = cli("run", "summary", "--run-id", run_id, check=False)
    st = run_state(summary) if isinstance(summary, dict) else None
    if st in ("needs_operator", "failed", "canceled"):
        return False, f"run state={st} (escalation / would need rescue)"
    if st in ("waiting_human", "blocked_human"):
        return False, f"run state={st} (human checkpoint / revision budget exhausted)"
    if st:
        return False, f"run state={st}; run drive exit={result.returncode}"
    return False, f"run drive exit={result.returncode}"


def main():
    passes = 0
    for i in range(1, N + 1):
        print(f"=== DoD run {i}/{N} ===", flush=True)
        prep = cli("run", "prepare", "--workflow", WORKFLOW, check=False)
        run_id = prep.get("run_id") if isinstance(prep, dict) else None
        if not run_id:
            print(f"  prepare failed: {prep}", flush=True); break
        # --no-drive: this harness drives the run itself (drive_run below), so it
        # opts out of run start's default background auto-drive (#212) to avoid two
        # drivers racing the same run.
        start = cli("run", "start", "--run-id", run_id, "--no-drive", check=False)
        print(f"  run {run_id} start={start.get('state') if isinstance(start,dict) else start}", flush=True)
        ok, reason = drive_run(run_id)
        print(f"  run {i} -> {'PASS' if ok else 'FAIL'} ({reason}) [{run_id}]", flush=True)
        if not ok:
            # cancel the wedged run so it doesn't dangle, then stop the streak.
            cli("run", "cancel", "--run-id", run_id, "--reason", "DoD run did not complete unattended", check=False)
            print(f"=== STREAK BROKEN at run {i}: {reason} ===", flush=True)
            break
        passes += 1
    print(f"=== DoD RESULT: {passes}/{N} consecutive clean unattended passes ===", flush=True)
    sys.exit(0 if passes == N else 1)


if __name__ == "__main__":
    main()
