# RFC 0027: Sealed Patch Provenance Mode

Status: superseded by RFC 0127 (D195)
Date: 2026-05-10
Context:
[`docs/records/_frozen/research/P005_SYNTHESIS.md`](../records/_frozen/research/P005_SYNTHESIS.md),
[`docs/records/_frozen/research/P005_TRUE_PROVENANCE_CONSENSUS_SYNTHESIS.md`](../records/_frozen/research/P005_TRUE_PROVENANCE_CONSENSUS_SYNTHESIS.md),
[`docs/research/TRUE_PROVENANCE_AND_CONTAINMENT.md`](../research/TRUE_PROVENANCE_AND_CONTAINMENT.md),
[`docs/research/OPERATOR_BYPASS_DEFENSE_IN_DEPTH.md`](../research/OPERATOR_BYPASS_DEFENSE_IN_DEPTH.md),
[`docs/records/_frozen/research/P005_TRUE_PROVENANCE_LOOPHOLE_RESPONSE.md`](../records/_frozen/research/P005_TRUE_PROVENANCE_LOOPHOLE_RESPONSE.md),
[`docs/records/_frozen/research/P005_GEMINI_ANALYSIS_ABSOLUTE_PROVENANCE.md`](../records/_frozen/research/P005_GEMINI_ANALYSIS_ABSOLUTE_PROVENANCE.md),
[`docs/records/_frozen/research/P005_PROVENANCE_BYPASS_STRATEGIES.md`](../records/_frozen/research/P005_PROVENANCE_BYPASS_STRATEGIES.md),
[`RFC 0026`](0026-lane-attestation-and-operator-byline-honesty.md),
[`docs/reference/spec.md`](../reference/spec.md),
[`docs/decisions/decision-log.md`](../decisions/decision-log.md) (D006, D009,
D020, D028, D036, D048, D049)

Implementation status: dogfood-030 shipped phase 2 guardrails
(`provenance_mode` workflow field + fail-closed `sealed_patch` startup
refusal). dogfood-034 added daemon-side sealed-apply RPC scaffold +
signing-key custody under RFC 0031; the full apply gate + receipt
pipeline remains a follow-up.

## Problem

Striatum can record durable provenance for work submitted through its
control surface, but today the top-level operator usually runs in the
same writable checkout as the source tree, runner code, workflow files,
and `.striatum/retired-local-state`. An operator surrogate with native file
tools can edit protected source bytes directly, then call normal
Striatum verbs to publish artifacts, record verdicts, and complete jobs.
The resulting run may look like a multi-lane workflow even though the
source bytes entered outside the workflow.

RFC 0026 addresses one important part of this failure mode: an
unattested session should not mint a lane-typed byline. That is byline
honesty, not source-byte provenance. It prevents a frictionless false
claim like `author: reviewer-codex-gpt-5.5-001`, but it does not stop a
native operator edit from reaching the target repository.

The P005 synthesis documents converge on the same underlying diagnosis:
hooks, prompts, plugin instructions, worktree hygiene, and checksum
ledgers are useful tripwires, but they are not a hard boundary while the
operator can write the same files that express the boundary. Hard local
patch provenance requires a different authority model: the canonical
source tree has one writer, lanes produce immutable patch objects, review
verdicts bind to exact patch digests, and a Striatum apply gate is the
only path from reviewed patch to protected source.

## Goals

- Name Striatum's provenance modes honestly so the current local CLI flow
  is not described as "true provenance".
- Preserve the existing `advisory` mode for fast local workflows.
- Define an opt-in mode in which an unprivileged operator with native
  shell/file tools cannot write protected source paths.
- Make patch identity first-class: base tree, result tree, touched paths,
  blob/hunk digests, producer job/session/supervisor, and patch digest.
- Require apply-gating review verdicts to bind to the exact patch artifact
  and digest they reviewed.
- Make apply a Striatum state transition with explicit preconditions,
  not an operator habit.
- Emit a tamper-evident provenance receipt that links the final protected
  tree to workflow hash, patch artifacts, jobs, sessions, supervisors,
  reviews, verification, and Striatum version.
- Preserve the local-first boundary: no hosted service, telemetry, remote
  persistence, or broad transcript capture is required.
- Introduce a narrow sealed-mode exception that lets Striatum create a
  local signed commit as the final apply receipt, while still never
  pushing, merging, or rebasing.

## Non-Goals

- No defense against local root, a compromised Striatum service user, a
  compromised Striatum package, or a user who can alter the OS permission
  boundary.
- No proof that a specific model token stream authored a patch. D028's
  no-broad-transcript-capture posture remains intact.
- No prevention of lane prompt dictation or context-doc smuggling. An
  operator can still over-specify intent or paste a diff into a real
  lane's prompt. This RFC delivers process and patch provenance, not
  model-token provenance.
- No proof of independent decision provenance. The human, or a future
  operator quorum design, remains the intent authority.
- No hosted signing service, transparency log, Sigstore dependency,
  model-rooted token attestation, TEE, or watermark dependency in V1.
- No automatic defaulting to sealed mode for existing workflows.
- No push, merge, rebase, remote publication, or branch-protection
  management by Striatum.

## Proposal

### 1. Provenance modes

Add a workflow-level `provenance_mode` field, snapshotted onto each run.
The initial closed set is:

```json
"provenance_mode": "advisory" | "attested_bylines" | "sealed_patch"
```

`advisory` is the default and describes current behavior: Striatum records
workflow state, artifacts, verdicts, and evidence for work that flows
through its commands, but it does not contain a native operator who can
write source files directly.

`attested_bylines` is the RFC 0026 mode. Lane-typed bylines require a live
supervised lane binding; unattested work is labeled `author: operator`.
This mode improves evidence honesty but still does not prevent direct
source edits.

`sealed_patch` is this RFC's mode. Protected source writes are mediated by
Striatum. Lanes write scratch workspaces. Striatum captures immutable patch
artifacts. Reviews bind to exact patch digests. The apply gate writes the
protected tree and emits a signed provenance receipt.

The runner must surface the mode in `status`, `doctor`, evidence export,
run summary, dashboard, and the web UI. `doctor` must warn when a workflow
claims `sealed_patch` but the local authority boundary has not been
established. `run start` should refuse sealed runs when the operator can
write protected source paths.

### 2. Authority and filesystem model

Sealed mode introduces three local authorities:

- **Operator control workspace.** The operator can edit workflow/RFC/control
  material, inspect run state, start and recover runs, and read source
  through read-only mechanisms. The operator cannot write protected source
  paths or lane scratch paths.
- **Lane scratch workspace.** Each repo-write job receives a per-job
  scratch worktree or overlay writable by the lane identity. Scratch is the
  only place lanes author source changes.
- **Protected repository.** The canonical source tree is writable only by
  the Striatum apply service or apply mutation. It remains readable to the
  operator, preferably by read-only mount or by `striatum source read` /
  `striatum source grep`.

The exact OS mechanism is implementation-defined but must be explicit.
Linux can use separate Unix users, ACLs, read-only bind mounts, `bwrap`, or
container namespaces. macOS and Windows must either provide an equivalent
hard write-denial story or mark sealed mode unsupported. Silent degradation
to advisory behavior is not allowed.

The protected path policy is workflow-declared:

```json
{
  "provenance_mode": "sealed_patch",
  "protected_paths": ["src/", "tests/"],
  "operator_writable_paths": ["docs/rfcs/", "workflows/"]
}
```

The validator rejects overlapping protected and operator-writable paths,
`..` traversal, absolute paths, and attempts to protect `.striatum/` as an
ordinary source path. `.striatum/retired-local-state` remains runner state, not a
source artifact.

### 3. Patch artifacts

Add a first-class patch artifact kind. The preferred shape is `artifact`
kind `patch` plus a dedicated `patch_artifacts` table keyed by
`artifact_id`.

Minimum metadata:

- `artifact_id`
- `run_id`
- `producer_job_id`
- `producer_session_id`
- `producer_supervisor_id`
- `base_tree`
- `result_tree`
- `patch_sha256`
- `paths_json`
- `blob_hashes_json`
- `hunk_hashes_json`
- `write_scope_validated`
- `captured_at`

`striatum patch capture` captures the delta from an expected base tree to
the lane scratch workspace. It refuses empty patches unless the workflow
explicitly allows them, refuses any path outside the job's write scope,
refuses forbidden paths, and refuses capture from a workspace not allocated
to that job.

Patch artifacts are immutable after capture. Regeneration, rebase, or
repair creates a new artifact id and a new digest.

### 4. Hash-bound reviews

Review verdicts that contribute to apply eligibility must name the exact
reviewed object:

- `reviewed_artifact_id`
- `reviewed_digest`
- `reviewed_base_tree`
- `reviewed_result_tree`

A verdict without these fields may still be recorded for advisory review,
but it cannot satisfy a sealed apply gate. If a patch is regenerated or
rebased, prior verdicts over the old digest no longer apply.

### 5. Apply gate

Add a new mutation surface, names illustrative:

```text
striatum apply reviewed-patch --run-id <run> --artifact-id <patch_artifact>
striatum provenance verify --run-id <run>
striatum provenance status --run-id <run>
```

`apply reviewed-patch` refuses unless all of the following hold:

- the run's `provenance_mode` is `sealed_patch`;
- the patch artifact exists and is immutable;
- the patch digest matches the producer's recorded digest;
- required reviewers have accepting verdicts over the same digest;
- the patch touches only allowed paths and no forbidden paths;
- the protected repository is still at the patch's recorded base tree;
- required verification jobs passed for the candidate tree;
- no open blocker, paused run, canceled run, or failed dependency makes
  apply ineligible;
- any configured `require_attested_lane` policy is satisfied.

The P005 synthesis resolved the commit-ownership blocker in favor of a
narrow sealed-mode exception: Striatum may create a local signed commit as
the final apply step for a reviewed candidate tree. Acceptance of this RFC
therefore requires a decision-log row and a SPEC carve-out: Striatum still
does not push, merge, or rebase under any mode, but in `sealed_patch` mode
the apply service may commit the reviewed result locally using the runner
signing key.

### 6. Receipts and signing

Apply emits a signed provenance receipt. Minimum receipt fields:

- `receipt_version`
- `run_id`
- `workflow_hash`
- `base_tree`
- `result_tree`
- `patch_artifact_ids`
- `patch_digests`
- `producer_jobs`
- `producer_sessions`
- `producer_supervisors`
- `review_verdict_ids`
- `verification_job_ids`
- `striatum_version`
- `schema_version`
- `applied_at`
- `commit_hash` when a signed local commit was created

V1 should use a local signing key whose private material is outside the
operator's writable authority. The synthesis recommends Ed25519 runner
keys, ephemeral lane keys, and nonces, but the acceptance-critical
invariant is simpler: receipt verification must fail after direct SQLite
tamper, patch substitution, or protected-tree drift.

Suggested commands:

```text
striatum keys init
striatum keys rotate
striatum keys export-public
striatum receipt show --run-id <run>
striatum receipt verify --receipt-file <path>
```

External roots of trust, transparency logs, and provider signatures are
future RFC material.

### 7. Phased delivery

V1 can land in phases without overclaiming:

1. **Honest mode surfacing.** Add `provenance_mode` to workflow/run state
   and expose it in status, doctor, web UI, run summary, and evidence
   export. Existing workflows default to `advisory`.
2. **RFC 0026 prerequisite.** Land lane attestation and `author: operator`
   downgrade so evidence no longer overstates lane identity.
3. **Patch artifacts and hash-bound verdicts.** Add capture and reviewed
   digest binding while still running in advisory authority mode. This
   gives workflows exact review objects before hard containment ships.
4. **Apply gate and receipts.** Add deterministic apply checks and signed
   receipts. Same-user apply is useful for evidence quality but must not
   claim sealed enforcement.
5. **Protected source ownership.** Refuse `sealed_patch` runs unless the
   operator cannot write protected paths and cannot write active lane
   scratch. This is the first phase that mechanically closes the native
   operator source-edit bypass.
6. **Signed local commit receipt.** Once the sealed-mode commit decision is
   accepted, apply writes a local signed commit as the durable Git-level
   receipt. Push, merge, and rebase remain out of scope.

## Acceptance Criteria

A passing implementation must demonstrate adversarial behavior, not only
happy paths:

- `advisory` workflows keep their current behavior.
- A `sealed_patch` run refuses to start when the operator can write a
  protected source path.
- Native operator file tools and shell commands fail to write protected
  source paths during an active sealed run.
- The operator cannot write or modify an active lane scratch workspace.
- `patch capture` refuses a patch that touches a forbidden path or a path
  outside the job's write scope. Silent partial capture is not allowed.
- A review verdict over patch digest `A` does not satisfy apply for patch
  digest `B`.
- A patch captured against base tree `T1` cannot apply to protected tree
  `T2` without an explicit rebase and re-review path.
- An unreviewed patch cannot be applied.
- A review verdict without a reviewed digest is recorded but does not
  satisfy the sealed apply gate.
- If `require_attested_lane` is set, an unattested producer session cannot
  satisfy apply eligibility.
- If a supervised process dies before patch capture, capture is refused for
  jobs that require attested lanes.
- Editing SQLite rows after apply causes receipt verification to fail when
  signatures or receipt material are outside the operator's writable
  authority.
- The signed receipt links final tree, patch digests, run id, workflow hash,
  producer jobs/sessions/supervisors, review verdicts, verification, and
  Striatum version.
- Evidence export contains enough patch and receipt metadata to verify the
  final protected tree from an exported bundle or fresh checkout with the
  relevant receipt material.
- Striatum-created sealed-mode commits are local, signed, and never push,
  merge, or rebase.

## Open Questions

- **First supported containment mechanism.** Which platform/mechanism lands
  first: Linux `bwrap`, separate Unix users, POSIX ACLs, macOS sandboxing,
  or a Striatum-owned local service? The RFC requires hard write denial but
  leaves implementation order to the build plan.
- **Protected path defaults.** Should sealed mode protect only source/test
  paths declared by the workflow, or all non-control repository paths by
  default? Narrower defaults are easier to adopt; broader defaults are
  easier to reason about.
- **Single patch vs. candidate tree batches.** V1 may apply one reviewed
  patch at a time. Larger workflows may need a candidate tree assembled from
  multiple reviewed patches with a single verification result.
- **Rebase semantics.** When base tree drift occurs, should Striatum provide
  a first-class rebase/re-review command or require a new producer job?
- **Receipt storage.** Should receipts be ordinary durable artifacts,
  Git notes, commit trailers, both, or a dedicated `.striatum/receipts/`
  export format?
- **Mode naming.** This RFC proposes JSON value `sealed_patch` because the
  honest guarantee is patch provenance. The user-facing label may be
  "sealed provenance" if the docs keep the scope explicit.

## Current Implementation Status

Dogfood-030 shipped only the honest mode-surfacing guardrail for this RFC.
Workflow validation accepts `provenance_mode` values `advisory`,
`attested_bylines`, and `sealed_patch`; absent mode defaults to
`advisory`. Structurally valid `sealed_patch` workflows must declare
repo-relative, non-overlapping `protected_paths` and
`operator_writable_paths`.

No patch capture, hash-bound review target, apply gate, receipt signing,
source containment, or signed local commit behavior has shipped. `run
start` refuses `sealed_patch` runs on every platform with an unsupported
containment error so the runner cannot silently downgrade sealed claims to
advisory behavior.

## Domain Modeling

This RFC adds several terms to the model. Per
[`docs/explanation/domain-driven-design.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model),
the accepted implementation should update `docs/reference/ubiquitous-language.md`
before validator and introspection changes land.

- **Provenance mode** - a workflow/run policy value object describing which
  provenance guarantee is active: `advisory`, `attested_bylines`, or
  `sealed_patch`.
- **Protected repository** - the canonical source tree whose writes are
  mediated by Striatum in sealed mode. It is a boundary clarification around
  the existing target repository concept.
- **Control workspace** - the operator-visible workspace for orchestration,
  workflow authoring, RFC/docs work, and read-only source inspection.
- **Lane scratch workspace** - the per-job authoring workspace writable by
  the attested lane identity, not by the operator.
- **Patch artifact** - a durable artifact subtype representing a source
  change with base tree, result tree, touched paths, digests, producer
  identity, and write-scope validation.
- **Candidate tree** - a value object representing the deterministic result
  of applying one or more patch artifacts to a base tree.
- **Apply gate** - a domain service and aggregate boundary that decides
  whether a candidate tree may become protected source.
- **Provenance receipt** - a durable signed artifact/event binding a
  protected tree transition to workflow hash, patch digests, producing jobs,
  sessions, supervisors, verdicts, verification, and Striatum version.

The central invariant is intentionally narrow: sealed mode can prove that
accepted protected source bytes entered through reviewed Striatum patch
objects. It does not prove independent model creativity, absence of operator
prompt dictation, or independent decision authority.
