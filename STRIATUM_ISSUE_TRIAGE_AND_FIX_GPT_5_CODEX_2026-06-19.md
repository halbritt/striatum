# Striatum Issue Triage And Fix

Run id: `2026-06-19_7ffc9d64_9ac92843`
Author: gpt-5-codex

## Run Basis

- Target: `/home/halbritt/git/striatum`
- Head: `7ffc9d64` (`origin/main`, after rebase onto PRs #487/#486/#488)
- Dirty state: local patch prepared; no commits, pushes, issue comments, labels, PRs, or closes applied.
- Prompt: `/home/halbritt/git/prompts/ISSUE_TRIAGE_AND_FIX.md`
- Issue source: `gh issue list --state open --limit 300 --json number,title,labels,updatedAt,createdAt,url,body,comments`
- Initial snapshot: 17 open issues, hash `ae500f814ffcbfba97c6dc4dade6c595a37ce9c5e67c398158464d187dcdbc08`
- Current snapshot: 14 open issues, hash `9ac9284393f976adacc85dfee93e1817518051dc2e1d03928ef85661e37182da`
- Snapshot drift: #402, #453, and #457 closed upstream during this run on 2026-06-19.
- Authority used: issue-read=yes, local-edit=yes, test-build=yes. Remote writes, close, commit, push, and PR creation were treated as not granted.
- Verifier: local tests and build. The running daemon was not restarted, so daemon-backed live `status` still reflects the old deployed handler.

## Route Ledger

| Issue | Route | Confidence | Action artifact | Notes |
|---:|---|---|---|---|
| #475 | FIX | high | local patch | Client-side repo paths now resolve absolute before daemon `repo.resolve`; covers `run prepare` and related daemon-routed commands plus `run drive` handoff. |
| #479 | FIX | high | local patch | Generated collaboration prompt files now include the topic-rich objective plus structured deliverable/falsifiability/output sections; examples updated. |
| #480 | FIX | medium | local patch | Repo-wide bounded status now scopes session and non-accepting verdict detail to the operator frontier; not live-deployed to the running daemon. |
| #477 | ABSTAIN | medium | human handoff | Safe read-surface UX fix, but broader than this batch: add escalation ids/commands to dashboard/run summary/why without changing recovery semantics. |
| #481 | RFC | medium | none | Cross-surface state projection is an API/read-shape decision; docs-only note or additive projection is plausible. |
| #476 | RFC | medium | none | Final-review debounce changes revision-routing semantics and quorum/final-review behavior. |
| #478 | RFC | high | none | Raising `agent_exited_unsealed` retry budget reverses accepted D198 defaults; keep human/RFC gated. |
| #482 | RFC | high | none | Security graduation blocker; must choose daemon-owned attestation rows vs daemon-signed attestations before implementation. |
| #483 | RFC | medium | none | Split needed: self-pin/version doctor diagnostics are small; `verified_stale` and resweep alter claim status semantics. |
| #354 | RFC | medium | none | Blocker text stale after #333/#346/#488, but remaining fan-in P1/P2 live cutover is still design/implementation sequencing. |
| #380 | ABSTAIN | medium | none | Gated on Repro B and multi-sibling witness despite #372 being closed. Needs a state label. |
| #381 | ABSTAIN | high | none | Parked until a real >1-job-per-lane workload exists. Current `bug` + `ready-for-human` labels are acceptable. |
| #387 | RFC | high | none | Partitioning `events`/`audit_log` is schema/policy work; RFC 0136 remains proposed. |
| #421 | RFC | high | none | Supervisor pointer bloat needs RFC 0139 accept/revise/decline before implementation; doc-only PR #468 does not close implementation. |

Counts: FIX 3, RFC 8, DISPOSE 0, ABSTAIN 3.

## Prepared Fix Artifacts

#475:
- `go/pkg/cli/dispatch/dispatch.go`
- `go/pkg/cli/dispatch/dispatch_test.go`
- `go/cmd/striatum/main.go`
- `go/cmd/striatum/main_test.go`
- `go/cmd/striatum/run_start.go`
- `go/cmd/striatum/run_start_test.go`

#480:
- `go/pkg/reads/status.go`
- `go/pkg/reads/status_bounded_runs_test.go`
- `go/pkg/reads/supervision_test.go`

#479:
- `go/pkg/workflowgenerate/generate.go`
- `go/pkg/workflowgenerate/generate_test.go`
- `examples/falsification-gate-flow/prompts/*.md`
- `examples/cross-examination-flow/prompts/*.md`

## Verification Ledger

Passed:

```bash
go test ./pkg/cli/dispatch ./cmd/striatum -run 'TestDispatch.*Repo|TestRunDriveDispatchesThroughRPC|TestRunRunStart.*Repo|TestWorkflowValidateJSON' -count=1
go test ./pkg/reads -run 'TestStatusRunLimitZeroExcludesTerminalNonAcceptingVerdicts' -count=1
go test ./pkg/workflowgenerate -run 'TestCollaborationShapesEmitSubstanceGateV11Graphs' -count=1
go test ./...
make -C go build
git diff --check
```

Live spot checks:

```bash
cd /home/halbritt/git/prompts
/home/halbritt/git/striatum/go/bin/striatum --repo . status --json --run-limit 0
```

The command succeeded from the `prompts` repo, confirming the client-side `--repo .` resolution no longer errors by resolving through the daemon cwd. The status payload itself is still served by the already-running daemon.

Not run:

- `make install`, daemon restart, `make smoke`: not run because that would deploy/restart the dirty local daemon while `operator bootstrap` reported an active run.
- Tracker writes and closes: not authorized.

## Reconcile Summary

Manifest: `STRIATUM_ISSUE_RECONCILE_GPT_5_CODEX_2026-06-19.md`

All remote actions are `blocked` / propose-only because issue-write, close, push, PR, and commit authority were not explicit in this prompt. No close warrants are emitted for #475/#479/#480 until the local patch is committed, reviewed, deployed if needed, and independently verified.

## Residual Risk And Handoffs

- #480 is verified by unit tests and build, but the live daemon was not restarted; a post-merge deploy check should re-run `striatum status --json --run-limit 0 | wc -c`.
- #477 is the highest-value next small fix: improve read surfaces by surfacing escalation ids and literal `escalation resolve <id>` hints.
- #482 and the `verified_stale` part of #483 are security/claim-semantics changes and should not be folded into a routine fix batch.
- The tracker label invariant is currently weak for #475/#476/#477/#478/#380/#354; proposed label actions are in the reconcile manifest.
