---
schema_version: "striatum.progress_note.v1"
artifact_kind: "progress_note"
note_date: "2026-06-17"
session_slug: "reliability-reset-actual-work-log"
related_brief: "brief_2026-06-16_reliability-reset-closeout"
retrieval_priority: "high"
---

# Reliability Reset Actual Work Log
author: operator-codex-gpt-5-001

This note records what the 2026-06-16 Codex operator session actually changed
and verified. It is not a plan, synthesis, or next-agent handoff.

## Commits pushed to `origin/main`

1. `3dc3d5e1e7d6f9ed72c901d1dddeaabbfab22b71`
   `Fix reliability reset Codex lane command`

   Changed:
   - `docs/operator/workflows/striatum-reliability-reset-2026-06-16/RUN_ON_PROXIMAL.md`
   - `docs/operator/workflows/striatum-reliability-reset-2026-06-16.json`

   Actual effect:
   - Replaced the invalid `striatum codex ...` lane command with direct `codex`
     invocation for the reliability-reset workflow.
   - Fixed the jq status query in `RUN_ON_PROXIMAL.md`.

2. `db67c2e85235b185139456db1c00e16cdec99d3f`
   `docs(operator): close reliability reset frontier`

   Changed:
   - `README.md`
   - `docs/index.md`
   - `docs/operator/BRIEF.md`
   - `docs/operator/artifacts/striatum-reliability-reset-2026-06-16/`
   - `docs/operator/recovery-decisions/FINAL_REVIEW_RECOVERY_DECISION_2026-06-16.md`

   Actual effect:
   - Preserved the checked-out reliability-reset review artifacts.
   - Recorded the operator decision used to recover the final-review lane.
   - Updated the operator brief from stale issue/release state to the live
     frontier after the reset run.
   - Updated the README project status from stale v2.9-era claims to v2.33.0.
   - Updated the docs index so `docs/operator/BRIEF.md` is the live frontier
     source, with roadmap/todo issue lists treated as secondary until refreshed.

3. `86b1e76213e49d1e213d998a0c0aadf48c77455f`
   `docs(operator): publish preserved agent guide materials`

   Changed:
   - `.agy/skills/`
   - `striatum-STRIATUM_AGENT_GUIDE.md`
   - `striatum-STRIATUM_AGENT_GUIDE.manifest.json`
   - `STRIATUM_RUN_RETROSPECTIVE_DI_RUN2_OPUS_4_8_2026-06-16.md`

   Actual effect:
   - Published generated Striatum agent-guide material that had been preserved
     in a pre-run stash.
   - Published the `di-run2` retrospective that had also been preserved in that
     stash.
   - Normalized one hardcoded local repo path in the retrospective from
     `/home/halbritt/git/striatum` to `~/git/striatum` before pushing.
   - Dropped the preservation stash after the content landed on `main`.

## Striatum runs handled

- Canceled failed first reliability-reset attempt:
  `run_25787578973277c272e284af54d899cc`.
- Completed corrected reliability-reset run:
  `run_8489e7d2df3b56e1ed7fdb49ff5c8ba7`.
- The completed run had 8 completed jobs, `completion_mode=lanes_attested`, and
  accepting verdicts with findings.
- The run reproduced the final-review `agent_exited_unsealed` failure class
  twice before a fresh lane sealed the verdict.
- Recovery stayed on the daemon path:
  `recovery requeue-stale`, decision record
  `DECISION-striatum-reliability-reset-final-review-requeue-2026-06-16`, and
  `escalation resolve`.

## Verification actually run

After the pushed changes:

- `git diff --check`
- `make check-docs`
- `./go/bin/striatum doctor --json`
- `./go/bin/striatum operator bootstrap --markdown`
- `gh issue view 302`, `gh issue view 308`, and `gh issue view 309`

Observed final state on 2026-06-17:

- Primary checkout clean at `86b1e762`.
- `origin/main` matched local `main`.
- `doctor ok=true`.
- `doctor problems=0`.
- `doctor warnings=219`.
- `needs_operator=0`.
- `stale_leases=0`.
- #302, #308, and #309 were closed; they are regression references, not open
  work.

## What was not done

- No Go runner bug fix was implemented in this session.
- No recovery/fan-in/daemon restart issue was closed by code.
- `RESET_SYNTHESIS.md` and `SUPPORT_LEDGER.md` were not committed as checkout
  files; they remained blob-backed Striatum run artifacts referenced by the run
  completion record.
- The warning volume in `striatum doctor` was not reduced.
- The current open issue frontier was not triaged beyond confirming #302/#308/#309
  were closed and recording the 19-open-issue snapshot in `docs/operator/BRIEF.md`.

## Files to read for evidence

- Workflow command fix:
  `git show 3dc3d5e1e7d6f9ed72c901d1dddeaabbfab22b71`
- Closeout docs/artifacts:
  `git show db67c2e85235b185139456db1c00e16cdec99d3f`
- Preserved generated materials:
  `git show 86b1e76213e49d1e213d998a0c0aadf48c77455f`
- Live operator state:
  `docs/operator/BRIEF.md`
- Recovery decision:
  `docs/operator/recovery-decisions/FINAL_REVIEW_RECOVERY_DECISION_2026-06-16.md`
