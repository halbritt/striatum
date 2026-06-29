---
status: synthesis
date: 2026-05-10
author: synthesist-codex-gpt-5-001
inputs:
  - docs/records/_frozen/research/OPERATOR_BYPASS_DEFENSE_IN_DEPTH.md
  - docs/records/_frozen/research/P005_GEMINI_ANALYSIS_ABSOLUTE_PROVENANCE.md
  - docs/records/_frozen/research/P005_PROVENANCE_BYPASS_STRATEGIES.md
  - docs/records/_frozen/research/P005_TRUE_PROVENANCE_LOOPHOLE_RESPONSE.md
  - docs/records/_frozen/research/TRUE_PROVENANCE_AND_CONTAINMENT.md
---

# P005 True Provenance Consensus Synthesis

## Executive finding

The prompt says "four approaches" but names five source files. This
synthesis treats all five as inputs.

The inputs converge on a clear consensus at the patch-provenance layer:
Striatum cannot claim true provenance while the top-level operator has
same-user write authority over the protected source tree, lane scratch
workspaces, and provenance records. Plugins, prompts, skills, byline checks,
client-side hooks, and ordinary worktree isolation can improve workflow
hygiene, but they do not create a hard boundary when the operator can write
the same files that express the boundary.

The consensus approach is an opt-in sealed provenance mode:

1. The operator is treated as an untrusted planner.
2. The canonical protected repository has one writer: a Striatum apply
   service or apply mutation.
3. Lanes write bounded scratch workspaces, not the canonical tree.
4. Striatum captures lane output as immutable patch artifacts with base-tree,
   result-tree, path, blob, and patch digests.
5. Review verdicts bind to exact patch or candidate-tree digests.
6. The apply gate writes only reviewed, verified, in-scope patches.
7. The resulting tree update emits a tamper-evident provenance receipt.

That approach is sufficient to draft an RFC for local, opt-in sealed patch
provenance. It is not sufficient to claim absolute model-token provenance,
and it does not solve no-human intent provenance by itself. Those remain
research or product-policy questions.

## Source positions

`TRUE_PROVENANCE_AND_CONTAINMENT.md` frames four containment strategies:
git-hook ledger, OS write-scope isolation, blind/shadow worktrees, and an
inverted Striatum-native chat control plane. It recommends hooks first, chat
second, and OS isolation later.

`P005_PROVENANCE_BYPASS_STRATEGIES.md` emphasizes local mechanical controls:
make worktree isolation default for repo-write lanes, add post-job
checksum/diff verification, consider runner-mediated artifact publishing,
signed work packets, and git-level attestation. It recommends worktree
isolation plus post-job verification.

`P005_GEMINI_ANALYSIS_ABSOLUTE_PROVENANCE.md` sharpens the trust-boundary
failure as routing authority plus authoring capability in the same actor. It
proposes deterministic apply, model-rooted token attestation, and semantic
entropy analysis, then bundles them as a sealed-run protocol.

`P005_TRUE_PROVENANCE_LOOPHOLE_RESPONSE.md` provides the clearest
implementation shape: protected source ownership, operator control workspace,
attested lane scratch workspaces, patch escrow, hash-bound review, protected
apply, and provenance receipts. It argues that current plugin-driven
operation cannot honestly be called true provenance.

`OPERATOR_BYPASS_DEFENSE_IN_DEPTH.md` extends the problem from byte
provenance to intent and decision provenance. It adds cryptographic lane
signatures, threshold co-signing, capability discipline, workflow snapshot
signing, channel firewalls, replay audit, stylometry, watermarking, TEEs, and
operator quorum. It concludes that byte provenance is tractable, intent
provenance can only be made costly to subvert, and decision provenance needs
either human intent authority or operator quorum.

## Where the inputs agree

### 1. The bypass is an authority problem

All inputs reject the idea that better instructions alone can solve the
bypass. The operator bypass exists because the entity that is supposed to
route work can also author protected bytes with native harness tools. If that
operator can write source and then call Striatum verbs, the runner sees a
plausible workflow record but cannot know that the source bytes followed the
workflow.

### 2. RFC 0026-style byline honesty is necessary but insufficient

The inputs assume or reference the lane-attestation and byline-honesty track.
That work prevents unattested sessions from claiming lane-typed bylines. It
does not stop a top-level operator from editing source directly, nor does it
prove that a supervised lane produced a specific patch. A future sealed RFC
should depend on byline honesty for claims, but not confuse it with source
provenance.

As of this repository snapshot, `docs/rfcs/0026-lane-attestation-and-operator-byline-honesty.md`
is still marked `Status: proposed`, so an RFC that relies on it should either
make RFC 0026 a prerequisite or restate the minimal attested-lane requirement.

### 3. Client-side hooks and checksum ledgers are tripwires, not roots

The inputs agree that hooks and ledgers are useful. They disagree about how
strong they are. The consensus resolution is:

- A hook owned by the same operator user is an early warning, not a hard
  guarantee.
- A checksum ledger inside operator-writable SQLite is evidence only while
  SQLite remains untampered.
- Hooks become enforcement only when the checked ref, hook, or signer lives
  outside the operator's authority, such as a protected local bare repository,
  a Striatum apply service, server-side hook, branch protection, or signed
  receipt verifier.

### 4. Filesystem containment is the first hard local boundary

Every serious hard-provenance approach eventually removes normal source-write
permission from the operator. The implementation mechanism can vary by
platform: separate Unix users, POSIX ACLs, read-only bind mounts, `bwrap`,
containers, platform sandboxes, or a Striatum-owned service. The exact
technology is less important than the invariant: native operator file tools
and shell commands must fail to write protected source paths.

### 5. Patch artifacts are the right provenance object

The strongest inputs converge on patch escrow rather than direct lane writes.
Lanes should work in bounded scratch workspaces. Striatum should capture the
delta from an expected base tree, validate path scope, record digests, and
store the delta as an immutable patch artifact. Reviewers should review that
artifact or a candidate tree derived from it.

This is a better unit than "every line" because generated files, formatting,
renames, and refactors make line identity unstable. The durable invariant
should bind tree objects, file blobs, patch hunks, job/session/supervisor
identity, and review verdicts.

### 6. Reviews must bind to exact objects

A review verdict over "job X" is too weak. A future RFC should require review
verdicts that affect apply eligibility to record the patch artifact id and
digest, plus the base tree and candidate result tree where relevant. If the
patch is regenerated, rebased, or modified, it must receive a new digest and
must be reviewed again according to workflow policy.

### 7. Apply must be a Striatum state transition

The operator applying a patch after review recreates the loophole. Apply must
be a checked Striatum mutation or local service operation with preconditions:
eligible run/job state, required reviewed patch digests, write-scope
validation, verification status, expected base tree, and no incompatible
pause/cancel/compromise state.

### 8. Local-first is compatible with sealed mode

None of the consensus architecture requires hosted services, telemetry,
remote persistence, or broad transcript capture. A local machine can contain
multiple local authorities: an operator user, a Striatum service/apply user,
lane users or sandboxes, scratch workspaces, protected source, and local
signing keys. This preserves the product boundary while making "local" mean
more than "same process and same writable directory."

### 9. Model-token provenance is not a near-term product claim

Provider signatures, watermarks, semantic entropy analysis, TEEs, and replay
audits are useful research directions. They should not block a sealed patch
provenance RFC. Striatum can plausibly enforce that accepted bytes entered
through attested jobs and hash-bound review gates. It cannot currently prove
that a particular frontier model token stream generated those bytes without
provider cooperation, transcript capture, or a much narrower harness
protocol.

### 10. Decision provenance is a separate problem

Hard byte provenance does not answer who chose the goal, acceptance criteria,
workflow shape, or review postures. If the human is fully removed, the
top-level AI operator becomes a singleton intent authority. The inputs do not
reach consensus that this is acceptable. The realistic product position is:
sealed mode can make execution faithful to declared workflow policy, but
intent authority must remain explicit. Either a human accepts intent, or a
future operator-quorum design replaces the human at higher cost.

## Where the inputs disagree

### Git hooks as phase one

One input recommends git hooks and a cryptographic ledger as the immediate
first phase. Two others explicitly say client-side hooks alone are bypassable
by `--no-verify`, hook edits, direct working-tree changes, or SQLite tamper.

Consensus: hooks are valuable diagnostics and can become a verifier for signed
receipts, but they should not be the root control for true provenance unless
the hook or checked ref is outside operator authority.

### Worktree isolation as sufficient closure

One input treats existing per-job worktree isolation as the most impactful
existing mechanism and suggests making it default or mandatory for repo-write
lanes. The stronger sealed-mode inputs argue that ordinary worktrees are not
enough if the operator can still write the main tree, lane scratch, or final
apply path.

Consensus: use worktrees or overlays as lane scratch, but pair them with
operator write denial and Striatum-only apply. Current opt-in worktree
isolation is a useful building block, not a complete answer.

### Atomic artifact publishing

Runner-mediated artifact writing closes "write then claim" for Markdown
artifacts, but source changes are not just artifact files. The stronger
approach is to introduce a patch artifact kind and apply gate. Atomic artifact
publishing may still be useful for research/findings outputs, but source
provenance should center on patch capture and reviewed apply.

### Signed work packets and lane signatures

Cryptographic signatures improve tamper-evidence and offline verification,
but they do not by themselves stop direct operator edits or lane dictation.

Consensus: defer broad signature infrastructure until patch artifacts and
hash-bound reviews exist. Then sign receipts, workflow snapshots, or lane
submissions where the threat model justifies it.

### Inverted control plane

The Striatum-native chat/control-plane approach is clean because the planner
never receives source-write tools. But external coding harnesses will remain
useful, and Striatum's product boundary includes terminal-agent portability.

Consensus: name the modes. Striatum-native chat can be the preferred sealed
UX, while external harnesses can participate only when launched in a sealed
driver mode whose native file tools cannot write protected source.

### Channel firewalls, semantic entropy, replay, and stylometry

Defense-in-depth work highlights lane-dictation and idea-laundering attacks.
These are real, but prevention is not crisp: natural language can encode code,
and model-based detectors are probabilistic.

Consensus: do not block sealed patch provenance on these controls. Track them
as V2 or research for high-assurance workflows. V1 should prevent direct
operator source writes and review/apply substitution. It should not claim to
prove independent creative intent.

### Commit ownership

Some inputs want Striatum to create signed commits. Current Striatum SPEC says
V1 does not commit, push, merge, or rebase. Sealed provenance can begin with a
protected working tree and receipts, but durable Git history is the natural
place for long-lived provenance.

Consensus: the RFC should make commit ownership an explicit product decision.
Do not smuggle it in. A narrow future rule could be: in sealed mode, Striatum
may create a local signed commit only as the final apply step for a reviewed
candidate tree.

### Zero-human operation

Some inputs frame the goal as fully autonomous, zero-human-in-the-loop
provenance. The defense-in-depth input argues that removing the human leaves a
single AI operator as intent authority unless operator quorum is added.

Consensus: sealed mode can reduce the human role to intent acceptance or
policy selection. Full replacement of human intent authority is not solved by
the proposed patch-provenance RFC.

## Consensus RFC shape: sealed patch provenance

### Proposed title

RFC 0027: Sealed Patch Provenance Mode

The number is illustrative. Use the next available RFC number if this moves
forward.

### Problem

Striatum currently records durable provenance for work submitted through its
control surface, but a top-level operator with native write tools can modify
the target repository directly and then record plausible workflow success.
This creates a false provenance chain: SQLite, artifacts, bylines, and
verdicts may imply multi-lane execution even though protected bytes entered
outside the workflow.

### Product position

The RFC should define provenance modes explicitly:

- `advisory`: current local CLI behavior. Useful workflow discipline and
  durable provenance for submitted artifacts, but no hard containment of an
  external operator with source-write access.
- `attested_bylines`: RFC 0026 or equivalent. Lane-typed bylines require a
  live supervised lane binding; unattested work is labelled `author:
  operator`.
- `sealed_patch`: protected source writes are mediated by Striatum. Lanes
  produce patch artifacts. Reviews bind to patch digests. Apply is a Striatum
  gate. Receipts bind the final tree to the reviewed objects.
- `sealed_commit`: optional future extension where Striatum also creates or
  verifies signed Git commits/refs.

The RFC should avoid using "true provenance" for `advisory` mode. It may say
`sealed_patch` provides local hard patch provenance against an unprivileged
operator, assuming OS permissions and Striatum's service boundary hold.

### Goals

- Prevent a native operator source edit from reaching the protected tree in
  sealed mode.
- Make patch identity first-class and reviewable.
- Ensure the reviewed object is the applied object.
- Preserve Striatum's no-broad-transcript-capture posture.
- Preserve local-first operation.
- Keep advisory mode available for fast, low-assurance local work.
- Produce evidence exports or receipts sufficient to verify the final tree
  against run graph, patch artifacts, and verdicts.

### Non-goals

- No defense against local root or a user who can become the Striatum service
  user.
- No proof that a specific model token stream authored a patch.
- No proof that the top-level operator did not shape or dictate the lane's
  implementation through prompts.
- No hosted service, telemetry, or external persistence requirement.
- No automatic commit/push/merge/rebase unless a separate product decision
  explicitly adds `sealed_commit`.
- No replacement for human or quorum-based intent authority.

### Required invariants

1. The canonical protected repository has one writer.
2. The operator can plan, read through approved APIs, start workflows, inspect
   state, and recover runs, but cannot write protected source paths.
3. Lanes write per-job scratch workspaces only.
4. Patch capture refuses empty patches unless allowed, out-of-scope paths,
   missing base-tree metadata, and workspaces not provisioned for the job.
5. Patch artifacts are immutable after capture.
6. Review verdicts that gate apply name the exact patch artifact and digest.
7. Apply refuses if reviewed digest, current patch digest, base tree, write
   scope, verification state, or run state do not match policy.
8. Apply emits a provenance receipt with enough data to verify the transition
   without a transcript.
9. Evidence export exposes the patch and receipt chain.

### Domain model additions

- **Provenance mode**: repository or run mode describing which guarantee is
  active.
- **Protected repository**: a canonical source tree whose writes are mediated
  by Striatum.
- **Control workspace**: operator-visible workspace for workflows, RFCs,
  status, and read-only source inspection.
- **Lane scratch workspace**: per-job worktree or overlay writable by the
  attested lane process, not the operator.
- **Patch artifact**: immutable source-change artifact with base tree, result
  tree, path list, blob hashes, hunk hashes, producer job/session/supervisor,
  write-scope validation result, and patch digest.
- **Candidate tree**: deterministic result of applying one or more patch
  artifacts to a base tree.
- **Apply gate**: aggregate boundary that decides whether a candidate tree may
  become protected source.
- **Provenance receipt**: tamper-evident record binding a protected tree
  update to workflow hash, patch digests, jobs, sessions, supervisors,
  verdicts, verification commands, and Striatum version.

### Workflow configuration sketch

Exact JSON belongs in the RFC, but the shape should be explicit:

```json
{
  "provenance_mode": "sealed_patch",
  "protected_paths": ["src/", "tests/"],
  "operator_writable_paths": ["docs/rfcs/", "workflows/"],
  "apply_policy": {
    "require_hash_bound_reviews": true,
    "require_attested_lane": true,
    "require_verification": true,
    "allow_striatum_signed_commit": false
  }
}
```

Open design question: decide whether this is workflow-level only, lane-level,
or both. The consensus recommendation is workflow-level mode with lane-level
overrides only in a later RFC.

### Schema/API sketch

The RFC should specify one of two storage designs:

- Add `patch` to `ALLOWED_ARTIFACT_KINDS` and store patch metadata in a
  dedicated `patch_artifacts` table keyed by `artifact_id`.
- Or add a generic artifact metadata table keyed by `artifact_id` and use it
  for patch-specific fields.

Minimum fields for patch metadata:

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
- `write_scope_validated`
- `captured_at`

Verdicts need an object-binding field:

- `reviewed_artifact_id`
- `reviewed_digest`
- `reviewed_base_tree`
- `reviewed_result_tree`

Apply operations need a durable receipt:

- `apply_id`
- `run_id`
- `patch_artifact_ids_json`
- `base_tree`
- `result_tree`
- `receipt_sha256`
- `receipt_path` or artifact id
- `commit_hash` nullable
- `applied_at`

### CLI sketch

Names are illustrative:

```text
striatum source read --path <path>
striatum source grep --pattern <pattern>
striatum patch capture --run-id <run> --job-id <job> --workspace <path>
striatum verdict --job-id <job> --artifact-id <patch_artifact> --verdict <v>
striatum apply reviewed-patch --run-id <run> --artifact-id <patch_artifact>
striatum provenance verify --run-id <run>
striatum provenance status --run-id <run>
```

The RFC should decide whether `patch capture` is a new command or a mode of
`publish-artifact`. The consensus recommendation is a new command or command
group because patch capture has stronger preconditions than ordinary artifact
publishing.

### Protocol sketch

1. Human or operator selects `sealed_patch` mode.
2. Striatum verifies the protected source tree is not writable by the operator
   user.
3. Operator drafts or selects workflow policy in the control workspace.
4. Run starts; repo-write jobs provision lane scratch workspaces.
5. Attested lane writes only inside scratch.
6. Striatum captures scratch delta as a patch artifact.
7. Review lanes receive immutable patch metadata and record verdicts over its
   digest.
8. Verification jobs run against the candidate tree, if configured.
9. Apply gate checks policy and writes the protected source.
10. Striatum emits a provenance receipt and exposes it through evidence
    export, status, web UI, and optional commit metadata.

### Acceptance tests

A future RFC should require adversarial tests, not only happy paths:

- In sealed mode, native operator file tools and shell commands fail to write
  protected source paths.
- The operator cannot write or modify an active lane scratch workspace.
- A lane patch touching a forbidden path is refused in full. Silent partial
  capture is not allowed.
- A verdict over patch digest `A` does not satisfy apply for patch digest `B`.
- A patch captured against base tree `T1` cannot apply to protected tree `T2`
  without an explicit rebase and re-review path.
- An unreviewed patch cannot be applied.
- A review verdict without a reviewed digest does not satisfy the apply gate.
- If RFC 0026 or equivalent is required, unattested sessions publish under
  `author: operator` and cannot satisfy `require_attested_lane`.
- If a supervisor dies before patch capture, capture is refused for jobs that
  require attested lanes.
- Editing SQLite rows after apply causes receipt verification to fail when
  receipts/signatures are outside the operator's write authority.
- Advisory-mode workflows keep their current behavior.
- Evidence export contains enough patch and receipt metadata to verify the
  final protected tree from a fresh checkout or exported bundle.

## Implementation roadmap

### Phase 0: name the modes honestly

Update docs, `status`, `doctor`, and web UI to distinguish advisory mode,
attested bylines, and sealed patch provenance. Warn when a workflow claims
sealed behavior but the source tree is writable by the operator.

This phase can land before hard containment and helps prevent overclaiming.

### Phase 1: resolve byline honesty prerequisite

Land RFC 0026 or an equivalent minimal attested-lane model:

- lane-attested sessions;
- `author: operator` for unattested work;
- optional `require_attested_lane`;
- evidence surfaces that distinguish attested from unattested claims.

This still does not solve source bypass, but it prevents evidence from
overstating the source of artifacts.

### Phase 2: patch artifacts and hash-bound reviews

Add patch artifact capture in ordinary advisory mode first. Add verdict
binding to patch digest. Add evidence export support. This gives the workflow
an exact object to review before the OS containment work lands.

### Phase 3: apply gate and provenance receipts

Add `striatum apply` over reviewed patch artifacts and candidate trees. At
first it may run as the same user, which provides deterministic checks and
receipts but not hard containment. The API should already match sealed mode.

### Phase 4: protected source ownership

Make sealed mode real by moving protected source writes outside operator
authority:

- protected source tree owned by Striatum service/apply user;
- operator control workspace;
- read-only source inspection APIs;
- scratch workspaces owned by lane identity or sandbox;
- source-write denial tests for supported harnesses.

This is the first phase where the operator bypass is mechanically blocked for
an unprivileged operator.

### Phase 5: optional commit or external verification

If provenance must survive normal Git movement, add signed commits, protected
refs, Git notes, or receipt verification. This needs a specific product
decision because current Striatum deliberately avoids committing, pushing,
merging, or rebasing.

### Research track

Keep the following outside the V1 sealed patch RFC:

- channel firewall for operator-to-lane dictation;
- semantic entropy analysis;
- replay audit;
- stylometric operator-handprint audit;
- watermark-bound acceptance;
- TEE/confidential-VM execution;
- operator quorum for no-human intent provenance;
- provider model-rooted token attestation.

These may become future RFCs once sealed patch provenance exists and the
product has evidence about real bypass pressure.

## Blocks and required product decisions

The synthesis is sufficient to draft an RFC for sealed patch provenance. It is
not sufficient to start implementation without resolving the following:

1. **Mode boundary**: choose exact mode names and guarantee text. The product
   must stop implying true provenance for advisory plugin-driven operation.
2. **RFC 0026 dependency**: either land RFC 0026 first or include a minimal
   attested-lane prerequisite in the sealed provenance RFC.
3. **Local authority model**: choose the first supported containment
   mechanism. Linux `bwrap`/Unix users are easiest to reason about; macOS and
   Windows need an explicit story or an initial unsupported status.
4. **Protected path policy**: decide whether sealed mode protects only source
   paths (`src/`, `tests/`) or all non-control repository paths.
5. **Commit authority**: decide whether sealed mode may create local signed
   commits. Without this, sealed patch provenance can still work, but the
   guarantee lives in receipts and protected working-tree state rather than
   Git history.
6. **Intent authority**: decide whether the human remains the intent
   authority, whether operator autonomy is accepted as a singleton authority,
   or whether a future operator quorum is required for high-stakes no-human
   workflows.
7. **Threat model statement**: explicitly say sealed mode protects against an
   unprivileged operator with native harness tools, not local root or a
   compromised Striatum service/package.

## Consensus verdict

There is consensus for a practical, RFC-ready architecture at the patch
provenance layer: sealed patch provenance with protected source ownership,
lane scratch workspaces, patch artifacts, hash-bound review, Striatum-only
apply, and receipts.

There is not consensus for calling this absolute provenance over model tokens,
nor for claiming that it removes the need for an intent authority. The honest
claim is narrower and stronger: in sealed mode, accepted protected source
bytes entered through Striatum-reviewed patch objects, not through the
operator's native file tools.
