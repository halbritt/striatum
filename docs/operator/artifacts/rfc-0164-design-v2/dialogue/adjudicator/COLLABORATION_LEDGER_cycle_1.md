---
schema_version: "striatum.collaboration_ledger.v1.1"
artifact_kind: "collaboration_ledger"
shape: "falsification_gate"
author: adjudicator-author-001
workflow: "rfc-0164-design-v2"
run_id: "run_ec809be24d845a095dbc271fa384bc28"
cycle: 1
topic: "RFC 0164 P0 falsifiable implementation SPEC v2 REVISION — read-side + funnel git neutralization, the complete git-surface taxonomy, and the three-state gate-evidence recovery contract (discharge C1 severance-completeness and C2 false-positive-wedge from the v1 gate, carry forward everything v1 cleared)"
participants:
  - holder
  - falsifier_1
  - falsifier_2
  - adjudicator
entries:
  - kind: claim
    by: holder
    refs: ["dialogue:1"]
    text: "The v2 SPEC claims it discharges the two v1 gate constraints while carrying forward, unregressed, everything v1 cleared. C1 (severance-completeness): §0 replaces the v1 C-2 site-list with a git-surface TAXONOMY across reads/mutations/verifier/agentloop, classifying each route (R/S/C/M/D/W/L/X) and its current cmd.Env; every daemon-run route routes through a closed env (gitEnv() for reads, mutationEnv() for index/commit); the three live in-repo RCEs are closed by concrete mechanisms — status->core.fsmonitor by the demoted fixed -c core.fsmonitor= (A28, incl. the explicit recovery_quarantine_lane.go:425 closure), commit->core.hooksPath by -c core.hooksPath=<empty> on the porter commit (A30), porter add->filter.clean by filter-free StageBlob plumbing hash-object --no-filters + update-index --cacheinfo (A29); the three os.Environ()-sourced false-negatives (recovery_quarantine_lane.go:378, barrier_fanin.go:877, receipt.go:605) switch to mutationEnv() (A31); §4/A12 is reconciled by extending the existing helper-call-site guard (git_invocation_guard_test.go:49-53) to ban any funnel/helper/direct git exec with nil or os.Environ()-sourced env, claimed green in P0 because every funnel is routed through safegit, so A2/A12 are asserted TRUE within the §11 manifest, not retracted; the three corpus rows quarantine_status_fsmonitor / porter_add_filter_clean / porter_commit_hookspath are added red-before/green-after. C2 (false-positive wedge): one coherent THREE-state model — gate.read_gadget_observed (non-blocking telemetry for a recognized key already neutralized by construction, where a benign [alias] co=checkout / [pager] log=less -FRX lands and never blocks), gate.read_gadget_blocked (the reserved blocking state carrying A23 decay-TOCTOU, fired only for a genuinely-unneutralizable condition), gate.read_gadget_refused (hard refusal into the human-cleared recovery.quarantine_lane for an unknown/unattested key); false_positive_benign_test is made load-bearing (A33) with a paired unknown-key negative. Carry-forwards (layered severance, A7/A8/A16, A21/A21b, A22/A23/A25, the four §0 corrections) are asserted INTACT with only additive hardening (A7 strengthened to a direct structural assertion, A8 extended to mutationEnv())."
  - kind: challenge
    by: falsifier_1
    refs: ["dialogue:2"]
    correspondence: landed_unrebutted
    text: "C1 is NOT genuinely discharged: the taxonomy is still incomplete because it omits the CHECKOUT/SMUDGE carrier class. The v2 table classifies mutations/worktree.go:118 (worktree add --detach) and mutations/revision_routing.go:627 (reset --hard) as W-class worktree-admin CLOSED by mutationEnv() alone. That is false: git worktree add and git reset --hard materialize blobs through repo-local filter.<driver>.smudge driven by the target repo's local config + .gitattributes, which mutationEnv() does NOT disable (it removes ambient/global/system env, not the repository's own local config). Locally reproduced outside the tree: a temp repo with [filter \"pwn\"] smudge=sh -c 'touch <sentinel>; cat' and .gitattributes *.pwn filter=pwn creates the sentinel on both git worktree add --detach <wt> HEAD and git reset --hard <commit>. This is the daemon's OWN worktree-creation/reopening path under the daemon identity, not the named arbitrary-render or whole-tree add -A residual, so by the holder's own C1 standard (route every textconv/filter/hook/fsmonitor carrier through minted config OR a typed pre-exec refusal) it must be routed/refused — mutationEnv() is not a fix. Refutes A0, A2, A12, A20, A27 and the §11 green-build claim: a helper-level env invariant can be GREEN while the daemon still executes attacker-controlled repo config. The required taxonomy/corpus rows worktree_add_filter_smudge and reset_hard_filter_smudge (red-before/green-after) are absent. Secondary exhaustiveness failure: a fresh grep finds integrateGit callers the v2 table omits (barrier_run_entity.go:138, barrier_assembly.go:310, recovery_quarantine_lane.go:256, and worktree.go:738/775/1065/1174/1190/1223/1236/1275/1325/1336/1351/1793, plus integrate.go:139/177/225), so A2 is asserted more strongly than the source-backed taxonomy supports. The fix must be one of: minted config, checkout-free controlled materialization that never invokes filters, or typed pre-exec refusal until Slice 2."
  - kind: challenge
    by: falsifier_2
    refs: ["dialogue:3"]
    correspondence: landed_unrebutted
    text: "C2's positive half is materially better than v1 (the non-blocking gate.read_gadget_observed state plus the A33 assertions of no blocker / no recovery.quarantine_lane ref / a clean second read genuinely un-wedge a stable benign [alias]/[pager]). But C2 is still NOT genuinely discharged because the paired NEGATIVE control and the unknown-key route are underspecified/fake. A33's negative plants 'a config key not in the §0 recognized taxonomy' and expects a hard-refuse — but §0 is a git CALL-SITE/route taxonomy, not a config-key registry. Taken literally, ordinary benign repo-local config that is not a git spawn site — e.g. [color] ui=auto, [remote \"origin\"] url=..., [branch \"main\"] merge=refs/heads/main — has no §0 entry and no §5 green-corpus coverage, so it routes to gate.read_gadget_refused, creates a recovery.quarantine_lane ref, and wedges until a human clears: the SAME liveness failure C2 was meant to eliminate, moved from [alias]/[pager] to any unregistered benign key. If instead the intent is 'refuse only unknown EXECUTABLE gadget carriers,' the SPEC never defines that classifier — no config-key registry, no inert-vs-executable candidate-key extractor, no detector language — so the A33 negative can be satisfied by a key git ignores, making the test fake (refusing it proves overblocking; passing it proves nothing about a real future gadget). Refutes A24, A32, A33 and the coherence of the observed/blocked/refused model. Additional state-model contradiction: a recognized-but-NOT-yet-neutralized residual (filter.<driver>.clean on whole-tree add -A) has no coherent state — §8.3 state 1 assigns it to non-blocking 'observed ... and meanwhile refused' while §5/A18 say it is 'refused (not executed) in P0' and §8.3 state 3 reserves 'refused' for unknown keys only — so the single coherent blocker-vs-observability model is not actually delivered. Carry-forward regression check: NO regression found in layered severance, A7/A8/A16, A21/A21b, A22/A23/A25, or the four §0 corrections."
  - kind: constraint
    by: adjudicator
    refs: ["dialogue:2"]
    text: "C1 REMAINS OPEN (carries to the operator; the single revision cycle is now spent). The revised SPEC widened the taxonomy to the mutation funnel and closed the three named in-repo RCEs (fsmonitor-on-status incl. :425, hooksPath-on-commit, filter-on-porter-add) plus the os.Environ() sources — genuine, verified progress — but it does NOT discharge C1 because the taxonomy is still not complete: it omits the checkout/materialization carrier class. worktree.go:118 (worktree add --detach) and revision_routing.go:627 (reset --hard) execute repo-local filter.<driver>.smudge under the daemon identity and are classified as W-admin closed by mutationEnv() alone, which is not a fix (verified in source; smudge driver is repo-local config + attributes, not ambient env). To clear, a future operator-led continuation must: add checkout/materialization carriers (worktree add, reset --hard, and any equivalent blob-materializing checkout) to the taxonomy and the corpus with red-before/green-after rows (e.g. worktree_add_filter_smudge, reset_hard_filter_smudge); route each through minted config OR checkout-free controlled materialization that never invokes filters OR a typed pre-exec refusal until Slice 2; and either enumerate the omitted integrateGit call sites honestly or stop asserting A2/§0 as the complete test allowlist (scope the claim like §1 instead of over-asserting it)."
  - kind: constraint
    by: adjudicator
    refs: ["dialogue:3"]
    text: "C2 REMAINS OPEN (carries to the operator; the single revision cycle is now spent). The three-state model fixes the v1 stable-benign [alias]/[pager] wedge for the positive case (real progress) but does NOT discharge C2 because the negative control is fake/underspecified and the state model is still not fully coherent. To clear, a future operator-led continuation must: define the scanned config-key DOMAIN precisely as a config-key registry (not by reference to the §0 call-site taxonomy), separating inert benign keys from execution-capable candidate carriers; make inert unknown benign keys NON-blocking (so [color]/[remote]/[branch] and other ordinary config never wedge), or justify and test a narrower scanner that never sees them; use an explicit executable-but-unattested fixture for the A33 negative and assert it hard-refuses into recovery.quarantine_lane WITHOUT turning ordinary benign config into a human-cleared blocker; and give the recognized-but-not-yet-neutralized residual (e.g. filter.clean on whole-tree add -A) a single coherent state rather than simultaneously 'observed' (non-blocking) and 'refused' (blocking, unknown-only)."
verdict: "needs_revision"
rationale: "needs_revision. The v2 revision is a substantial, source-anchored improvement on v1 and makes real progress against both constraints — but neither C1 nor C2 is GENUINELY discharged, and both falsifier challenges land as material, verified, and unrebutted (the trajectory ends at the two falsifier turns; the holder had no further turn). GENUINE v2 PROGRESS (carry into any operator-led continuation; do NOT re-derive): the §0 taxonomy was widened from the read funnel to the full mutation/integration funnel; recovery_quarantine_lane.go:425 status->fsmonitor is explicitly closed (A28); the porter add->filter.clean route is closed filter-free via StageBlob (A29) and commit->hooksPath via -c core.hooksPath=<empty> (A30); the three os.Environ()-sourced false-negatives switch to mutationEnv() (A31); A7 is strengthened to a direct structural assertion and A8 extended to mutationEnv(); and the three-state model's NON-blocking gate.read_gadget_observed genuinely un-wedges a stable benign [alias]/[pager] for the positive case. C1 STILL OPEN (severance-completeness): I verified in current go/ source that the taxonomy omits the CHECKOUT/SMUDGE carrier class — worktree.go:118 (worktree add --detach) and revision_routing.go:627 (reset --hard) run through the nil-env defaultRunGitWorktreeCommand funnel and materialize blobs through repo-local filter.<driver>.smudge under the daemon identity; the v2 table classifies them as W-admin closed by mutationEnv() alone, which does NOT disable repo-local config/.gitattributes, so a live daemon-identity RCE remains open after P0 as written. By the holder's own C1 standard (route every filter/textconv/hook/fsmonitor carrier through minted config or typed refusal) checkout IS a filter carrier and must be routed/refused; the helper-level env invariant (A12) can be GREEN while this RCE persists, and the §11 green-build claim is therefore not a completeness proof. The secondary exhaustiveness failure is also real: many integrateGit call sites (barrier_run_entity.go:138, barrier_assembly.go:310, recovery_quarantine_lane.go:256, worktree.go:738/775/1065/1174/1190/1223/1236/1275/1336/1351/1793, integrate.go:139/177/225) are absent from a table asserted as the complete test allowlist, so A2 is over-asserted. C2 STILL OPEN (false-positive + classifier): A33's negative half plants 'a config key not in the §0 recognized taxonomy', but §0 is a call-site/route table, not a config-key registry — taken literally this either hard-refuses ordinary benign config (e.g. [color] ui=auto, [remote \"origin\"] url=..., [branch \"main\"] merge=...), recreating the wedge through a new route, or it tests an inert key and proves nothing about a real executable gadget (a fake negative control); the SPEC defines no config-key registry, inert-vs-executable classifier, or detector language. The state model is also still not fully coherent: a recognized-but-not-yet-neutralized residual (filter.clean on whole-tree add -A) is simultaneously assigned non-blocking 'observed' and blocking 'refused' (reserved for unknown keys), so the single blocker-vs-observability model is not delivered. CARRY-FORWARDS INTACT: no regression in layered severance, A7/A8/A16, A21/A21b, A22/A23/A25, or the four §0 corrections C-1..C-4 (the v2 changes to them are additive hardening; Falsifier 2's carry-forward check concurs and I found none). The rubric requires BOTH constraints genuinely discharged with no standing regression and no new material challenge for a clearing verdict; here both remain open and two new material challenges stand, so the gate does not clear. This is needs_revision, not reject: the posture is correct, the v1-cleared substance and the genuine v2 progress are intact, and both residual defects are concrete and bounded (add checkout/smudge carriers to the taxonomy+corpus with minted-config/refusal; define the config-key registry and a real executable-but-unattested negative). This is the single allowed v2 revision cycle, so this second needs_revision ends the gate UNCLEARED and routes to the operator; the downstream commit_proposal/final_summary jobs must NOT publish on this verdict."
findings:
  - id: F-CHECKOUT-SMUDGE-CARRIER-OMITTED
    severity: critical
    posture: severance_incomplete
    status: converted_to_constraint
    challenge: "The v2 git-surface taxonomy omits the checkout/materialization carrier class. mutations/worktree.go:118 (git worktree add --detach) and mutations/revision_routing.go:627 (git reset --hard) run through the nil-env defaultRunGitWorktreeCommand funnel (worktree.go:1603-1604, cmd.Dir=repoRoot) and materialize blobs through repo-local filter.<driver>.smudge driven by the target repo's local config + .gitattributes — executing attacker-controlled code under the daemon identity. The v2 table classifies them as W-class worktree-admin closed by mutationEnv() alone; mutationEnv() removes ambient/global/system env but does NOT disable repo-local config/attributes, so the smudge driver still fires (locally reproduced; verified in source). This is the daemon's own worktree-creation/reopening path, not the named arbitrary-render or whole-tree add -A residual, so by the holder's own C1 standard it must be routed through minted config / checkout-free materialization / typed refusal. A helper-level env invariant (A12) can be GREEN while this RCE persists; the §11 green-build claim is therefore not a completeness proof. Required corpus rows worktree_add_filter_smudge and reset_hard_filter_smudge (red-before/green-after) are absent. Secondary: integrateGit callers omitted from a table asserted complete (barrier_run_entity.go:138, barrier_assembly.go:310, recovery_quarantine_lane.go:256, worktree.go:738/775/1065/1174/1190/1223/1236/1275/1336/1351/1793, integrate.go:139/177/225) over-assert A2."
    affected_invariants: ["A0", "A2", "A12", "A20", "A27"]
    source_refs: ["dialogue:2"]
  - id: F-UNKNOWN-KEY-NEGATIVE-CONTROL-FAKE
    severity: critical
    posture: false_positive_wedge
    status: converted_to_constraint
    challenge: "C2's negative control and unknown-key route are fake/underspecified. A33's negative plants 'a config key not in the §0 recognized taxonomy', but §0 is a git call-site/route taxonomy, not a config-key registry. Literally, ordinary benign repo-local config with no §0 entry and no §5 corpus coverage ([color] ui=auto, [remote \"origin\"] url, [branch \"main\"] merge) routes to gate.read_gadget_refused, creates a recovery.quarantine_lane ref, and wedges until a human clears — recreating the C2 wedge through a new route. If the intent is 'refuse only unknown executable gadget carriers,' the SPEC defines no such classifier (no config-key registry, no inert-vs-executable candidate-key extractor, no detector language), so the A33 negative is satisfiable by a key git ignores, making the test fake. Additional state-model contradiction: a recognized-but-not-yet-neutralized residual (filter.clean on whole-tree add -A) is simultaneously assigned non-blocking 'observed' (§8.3 state 1) and blocking 'refused' (§8.3 state 3, reserved for unknown keys), and §5/A18 call it 'refused (not executed)' — so the single coherent blocker-vs-observability model is not delivered. Refutes A24, A32, A33 and the model-coherence claim."
    affected_invariants: ["A24", "A26", "A32", "A33"]
    source_refs: ["dialogue:3"]
constraints:
  - id: C1-FUNNEL-TAXONOMY-AND-ROUTE
    source_finding: F-CHECKOUT-SMUDGE-CARRIER-OMITTED
    posture: severance_incomplete
    severity: critical
    kind: gate
    binding: true
    text: "C1 REMAINS OPEN. Discharge requires the git-surface taxonomy to be genuinely COMPLETE and every daemon-run config-sensitive route closed by minted config or typed refusal. The v2 revision closed status->fsmonitor (incl. recovery_quarantine_lane.go:425), commit->hooksPath, porter add->filter.clean, and the os.Environ() sources, but left the CHECKOUT/SMUDGE carrier class open: worktree add (worktree.go:118) and reset --hard (revision_routing.go:627) still execute repo-local filter.<driver>.smudge under mutationEnv() alone. A future operator-led continuation must add checkout/materialization carriers to the taxonomy and corpus (worktree_add_filter_smudge, reset_hard_filter_smudge red-before/green-after), route each through minted config OR checkout-free controlled materialization that never invokes filters OR typed pre-exec refusal, and either enumerate the omitted integrateGit call sites or scope A2/§0 honestly rather than asserting it as the complete test allowlist."
    source_refs: ["dialogue:2"]
    verification:
      gate: "worktree_add_filter_smudge and reset_hard_filter_smudge corpus rows are red on current source and green after the fix; no daemon checkout/materialization route executes a repo-local smudge/clean/textconv driver under the daemon identity; the build-time invariant flags any checkout-class carrier left under mutationEnv() alone; and the §0 taxonomy either enumerates every daemon-identity git call site (incl. all integrateGit callers) or honestly scopes its completeness claim."
      expected_stage: "operator-led continuation (gate uncleared) + rfc-0164-build"
    final_review_required: true
  - id: C2-FALSEPOS-NONBLOCKING-STATE
    source_finding: F-UNKNOWN-KEY-NEGATIVE-CONTROL-FAKE
    posture: false_positive_wedge
    severity: critical
    kind: gate
    binding: true
    text: "C2 REMAINS OPEN. Discharge requires a benign config to NEVER wedge AND unknown executable keys to hard-refuse AND a load-bearing false_positive_benign_test with a REAL negative control. The v2 three-state model un-wedges the positive [alias]/[pager] case but its negative control is fake: A33 plants 'a config key not in the §0 recognized taxonomy', and §0 is a call-site table, not a config-key registry. A future operator-led continuation must define the scanned config-key domain as an explicit registry separating inert benign keys from execution-capable carriers; keep inert unknown benign keys NON-blocking (so [color]/[remote]/[branch] never wedge); use an explicit executable-but-unattested fixture for the A33 negative (assert hard-refuse into recovery.quarantine_lane without making ordinary benign config a human-cleared blocker); and assign the recognized-but-not-yet-neutralized residual a single coherent state rather than both 'observed' and 'refused'."
    source_refs: ["dialogue:3"]
    verification:
      gate: "false_positive_benign_test asserts a planted benign [alias]/[pager] AND ordinary benign config ([color]/[remote]/[branch]) produce no job/run blocker, no recovery.quarantine_lane ref, and a clean second read; the paired negative uses an executable-but-unattested gadget fixture and asserts it hard-refuses into the human-cleared lane; the config-key registry defines inert-vs-executable precisely; and no residual config key occupies two states at once."
      expected_stage: "operator-led continuation (gate uncleared) + rfc-0164-build Slice 3"
    final_review_required: true
branches:
  severance_incomplete: "blocked"
  false_positive_wedge: "blocked"
---

# Collaboration Ledger — RFC 0164 P0 design v2 REVISION (cycle 1)

**Verdict: `needs_revision`.** This is the single allowed v2 revision cycle, so this
second `needs_revision` ends the gate **uncleared** and routes to the operator. The v2
revision is a substantial, source-anchored improvement and makes real progress against
both v1 constraints, but **neither C1 nor C2 is genuinely discharged**, and **both**
falsifier challenges land as material, verified, and unrebutted (the trajectory ends at
the two falsifier turns — the holder had no further turn).

## Per-constraint disposition (the two v1 gate constraints)

| Constraint | v2 status | Basis |
|---|---|---|
| **C1 — severance is complete** | **OPEN (not discharged)** | The taxonomy was genuinely widened to the mutation funnel and the three named in-repo RCEs (`status`→fsmonitor incl. `recovery_quarantine_lane.go:425`, `commit`→hooksPath, porter `add`→filter.clean) plus the `os.Environ()` sources are closed — but the taxonomy **omits the checkout/smudge carrier class**. `worktree.go:118` (`worktree add --detach`) and `revision_routing.go:627` (`reset --hard`) materialize blobs through repo-local `filter.<driver>.smudge` under the daemon identity and are classified as W-admin closed by `mutationEnv()` alone, which does not disable repo-local config/attributes. Verified in source; locally reproduced by Falsifier 1. **A2/A12/A20/A27 refuted; the §11 green-build claim is not a completeness proof.** Secondary: many `integrateGit` call sites are omitted from a table asserted as the complete allowlist, over-asserting A2. |
| **C2 — false-positive never wedges** | **OPEN (not discharged)** | The non-blocking `gate.read_gadget_observed` state genuinely un-wedges a stable benign `[alias]`/`[pager]` (the positive half of `false_positive_benign_test` is now real — a clear improvement on v1). But the **negative control is fake/underspecified**: A33 plants "a config key not in the §0 recognized taxonomy", and **§0 is a call-site/route table, not a config-key registry**. Literally it either hard-refuses ordinary benign config (`[color]`/`[remote]`/`[branch]`) — recreating the wedge through a new route — or tests an inert key and proves nothing about a real gadget. The SPEC defines no config-key registry or inert-vs-executable classifier. The state model is also still not fully coherent: the recognized-but-not-yet-neutralized residual (`filter.clean` on whole-tree `add -A`) is assigned both non-blocking `observed` and blocking `refused`. **A24/A26/A32/A33 not established.** |

## Per-falsifier disposition

- **Falsifier 1 — C1 / checkout-smudge gap (dialogue:2): MATERIAL, STANDING (`landed_unrebutted`).**
  Verified in `go/` source: `worktree.go:118` (`worktree add --detach`) and
  `revision_routing.go:627` (`reset --hard`) run through the nil-env
  `defaultRunGitWorktreeCommand` (`worktree.go:1603-1604`, `cmd.Dir = repoRoot`) and run
  repo-local `filter.<driver>.smudge` under the daemon identity. The v2 table routes them
  through `mutationEnv()` only — not a fix, by the holder's own C1 standard. The secondary
  exhaustiveness failure is also real (omitted `integrateGit` callers). → **C1 stays open.**
- **Falsifier 2 — C2 / unknown-key negative control + residual state (dialogue:3): MATERIAL, STANDING (`landed_unrebutted`).**
  The A33 negative references `§0` (a call-site taxonomy) as the "recognized taxonomy" for
  config keys, which is a category error; the SPEC never defines a config-key registry, so
  the negative is fake and the unknown-key route either over-blocks benign config or proves
  nothing. The residual-state contradiction (`observed` vs `refused`) is also present in the
  SPEC text. Falsifier 2's own carry-forward check found no regression. → **C2 stays open.**

## Carry-forwards — INTACT (no regression)

| Carried from v1 (CLEARED) | v2 status |
|---|---|
| Layered-severance posture (denylist demoted to telemetry) | INTACT — not reopened |
| Omission IS neutralization (`GIT_CONFIG_COUNT` beats `-c`) — A7/A16 | INTACT — A7 additively **strengthened** to a direct structural assertion |
| Env floor REFUSES, never degrades (`ErrGitEnvUnavailable`) — A8 | INTACT — additively extended to `mutationEnv` |
| Slice-0 no-truncated-graph + Slice-2 parity harness — A21/A21b | INTACT |
| Evidence mechanics — A22/A23/A25 | INTACT (attestation surface widened to the funnel; mechanics unchanged) |
| The four §0 source corrections C-1..C-4 | INTACT — extended, not re-found |

No carry-forward regressed (concurring with Falsifier 2's explicit check and my own).

## Genuine v2 progress (carry into any operator-led continuation; do NOT re-derive)

- §0 widened from the read funnel to the full mutation/integration funnel.
- `recovery_quarantine_lane.go:425` `status`→fsmonitor explicitly closed (A28).
- Porter `add`→`filter.clean` closed filter-free via `StageBlob` (A29); `commit`→hooksPath
  via `-c core.hooksPath=<empty>` (A30).
- The three `os.Environ()`-sourced false-negatives switch to `mutationEnv()` (A31).
- A7 strengthened to a direct structural assertion; A8 extended to `mutationEnv`.
- The three-state model's non-blocking `gate.read_gadget_observed` un-wedges the benign
  `[alias]`/`[pager]` positive case.

## What remains open (the bounded residual for the operator)

1. **C1** — add the checkout/materialization carrier class (`worktree add`, `reset --hard`,
   any blob-materializing checkout) to the taxonomy and corpus with red-before/green-after
   rows (`worktree_add_filter_smudge`, `reset_hard_filter_smudge`); route each through minted
   config / checkout-free materialization / typed pre-exec refusal; and enumerate or honestly
   scope the `integrateGit` call sites so A2/§0 is not over-asserted.
2. **C2** — define the scanned config-key **registry** (not the §0 call-site taxonomy)
   separating inert benign keys from execution-capable carriers; keep inert unknown benign
   keys non-blocking; use a real executable-but-unattested fixture for the A33 negative; and
   give the recognized-but-not-yet-neutralized residual a single coherent state.

This is the single allowed v2 revision cycle: this `needs_revision` ends the gate uncleared
and routes to the operator. The downstream `commit_proposal` / `final_summary` jobs must not
publish on this verdict.
