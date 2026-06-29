---
status: synthesis
date: 2026-05-10
author: synthesis-claude-opus-4-7-001
source_prompt: prompts/P005_true_provenance_loophole.md
sources:
  - docs/records/_frozen/research/TRUE_PROVENANCE_AND_CONTAINMENT.md
  - docs/records/_frozen/research/OPERATOR_BYPASS_DEFENSE_IN_DEPTH.md
  - docs/records/_frozen/research/P005_TRUE_PROVENANCE_LOOPHOLE_RESPONSE.md
  - docs/records/_frozen/research/P005_GEMINI_ANALYSIS_ABSOLUTE_PROVENANCE.md
  - docs/records/_frozen/research/P005_PROVENANCE_BYPASS_STRATEGIES.md
---

# P005 Synthesis: Sealed Provenance Mode

Current-context note (2026-05-17): this synthesis predates the D094/D104
daemon-required runtime. References below to `.striatum/retired-local-state` are
historical substrate examples; current Striatum live state is daemon-owned
PostgreSQL.

## 0. TL;DR

Five independent analyses of the P005 operator-bypass loophole converge on a
single architecture pattern under different names:

- TRUE_PROVENANCE_AND_CONTAINMENT (anon) — 4 strategies, no umbrella name
- OPERATOR_BYPASS_DEFENSE_IN_DEPTH (Claude Opus 4.7) — "layered defense-in-depth"
- P005_TRUE_PROVENANCE_LOOPHOLE_RESPONSE (Codex GPT-5) — "sealed provenance mode"
- P005_GEMINI_ANALYSIS_ABSOLUTE_PROVENANCE (Gemini 1.5 Pro) — "Sealed-Run Protocol"
- P005_PROVENANCE_BYPASS_STRATEGIES (iklllama Qwen3.6) — "structural enforcement + post-hoc verification"

The shared shape is: **the canonical repository becomes a protected resource
owned by Striatum, not a working directory owned by the operator harness.
Lanes write to scratch workspaces; Striatum captures patches as immutable
artifacts; reviewers accept exact patch hashes; an apply gate is the only
writer of the canonical tree; a signed receipt binds the final tree to the
run graph.**

There is enough consensus to write **RFC 0027: Sealed Provenance Mode** and
begin spec work. Three issues are genuinely open and belong in follow-on
research. The one product-decision blocker (B1: does sealed mode own
commits?) was **resolved on 2026-05-10 in favor of Option A** — Striatum
creates a local signed commit as the final apply step in sealed mode; no
push/merge/rebase. RFC 0027 is unblocked.

Section 9 below is the recommended RFC outline.

---

## 1. The Five Documents at a Glance

| Doc | Author role | Frame | Core proposal | Length |
|---|---|---|---|---|
| TRUE_PROVENANCE | anon | seed prompt | 4 numbered strategies (ledger / sandbox / shadow / inverted) | ~7K |
| OPERATOR_BYPASS_DEFENSE_IN_DEPTH | Claude Opus 4.7 | critique-and-extend | 10 additional strategies, 3-layer model, 5 proposed RFCs | ~53K |
| P005_RESPONSE | Codex GPT-5 | impossibility-result + architecture | Sealed Provenance Mode w/ 6 invariants, mode taxonomy, 5-phase roadmap | ~42K |
| P005_GEMINI | Gemini 1.5 Pro | shortest, three named techniques | MRTA + D-Apply + SEA → Sealed-Run Protocol | ~7K |
| P005_QWEN | Qwen3.6 | most conservative, local-first | 5 mechanical strategies; recommends worktree + checksum | ~11K |

The Codex response is the most architecturally complete. The Claude Opus
response is the most analytically sharp (introduces the byte/intent/decision
layer model and exposes the lane-dictation loophole that the seed prompt
missed). The Gemini response coins the cleanest taxonomy of the three named
techniques (MRTA/D-Apply/SEA). The Qwen response is the strongest at
identifying which pieces already exist in striatum today (D048 worktree
isolation, D028 transcript boundary). The original anon doc is the seed.

---

## 2. Shared Diagnosis (Five-Way Consensus)

All five agree on the following:

1. **The bypass is real and structural, not behavioral.** Prompt
   instructions, skill bundles, and a "good driver plugin" cannot close
   it. The operator's native harness writes the same file system as the
   runner's state and source tree.

2. **The current four-strategy doc (TRUE_PROVENANCE) is necessary but not
   sufficient.** Claude, Codex, and Gemini each say so explicitly. Qwen
   implicitly agrees by proposing different mechanisms.

3. **RFC 0026 (lane attestation) is byline hygiene, not byte provenance.**
   It prevents an unattested session from minting a model-typed byline; it
   does not prevent the operator from editing source files. Three of five
   docs say this in essentially identical words.

4. **Client-side git pre-commit hooks alone are insufficient.** `--no-verify`,
   hook deletion, and direct file edits after hash-recording all defeat them.
   Hooks are useful tripwires; they are not boundaries. (Codex states most
   explicitly; Claude implicit in §6 RFC 0027; Qwen treats hooks as one of
   five tools, not the keystone; the seed doc proposes hooks as Phase 1 but
   does not claim they are sufficient.)

5. **The right invariant is tree-level, not line-level.** The seed prompt
   asks for "every line of code." Codex explicitly argues the durable
   invariant is *the tree that landed*, not *the line that landed*, because
   line identity changes under formatting, renames, and refactors. Claude
   and Gemini both bind their guarantees to file/patch SHAs, not line spans.

6. **Local-first is preservable.** No proposal mandates cloud services,
   transcript capture, or hardware enclaves for V1. Hardware (TEE, MRTA)
   and transcripts are explicitly research-track in every doc that
   mentions them. This honors D020 and D028.

7. **The four strategies in the seed doc map onto a small set of "necessary
   ingredients" that the longer responses elaborate.** The four-strategy
   set is a partial enumeration; the longer responses make the underlying
   architecture explicit.

---

## 3. The Consensus Core (4-of-5 Agreement)

The following invariants appear in at least four of the five documents and
have no opposing position in the fifth:

### C1. The canonical repository has exactly one writer, and it is not the operator

- Codex: Invariant 1
- Gemini: Strategy B step 1 (read-only operator via bwrap/mount namespaces)
- Claude: Strategies 2 + 3 + 9 composed
- Anon seed: Strategies 2 + 4
- Qwen: not stated explicitly, but worktree-isolation-as-default (Strategy 1)
  produces the same effect at the level of "main branch is never directly
  editable by the agent"

### C2. Lanes do not write the canonical tree directly; they write to a scratch workspace

- Codex: Invariant 2 (patch escrow)
- Gemini: Strategy B step 2
- Claude: Strategy 3 (shadow worktrees) refined into RFC 0029 proposal
- Anon seed: Strategy 3 (Blind Operator) is the operator-side mirror; lanes
  see the full tree, but write into the mapped overlay
- Qwen: Strategy 1 (worktree isolation per repo-write lane)

### C3. The reviewed object is identified by an immutable hash, and the apply gate refuses if any required reviewer's hash differs

- Codex: Invariant 3 (hash-bound verdicts)
- Gemini: Strategy B step 4 ("accept verdicts for the exact hash of that patch")
- Claude: Strategy 8 (multi-lane threshold co-signing) ties signature
  verification to the file hash
- Anon seed: Strategy 1 records SHA256 in the attestation_ledger
- Qwen: Strategy 2 (post-job checksum gating) is the same mechanism in a
  pre/post-snapshot form

### C4. "Apply" is a Striatum state transition with explicit preconditions, not an operator habit

- Codex: Invariant 4 (apply service)
- Gemini: Strategy B step 3 ("The Apply Job is deterministic")
- Claude: pre-commit hook + signed-commit verification chain (RFC 0027 sketch)
- Anon seed: Strategy 1 hook is the apply choke point in spirit
- Qwen: implicit in Strategy 3 (atomic artifact publishing via the runner)

### C5. The apply produces a tamper-evident receipt linking final tree → patch hashes → producer jobs → review verdicts

- Codex: Invariant 5 (provenance receipt) — most fully specified
- Gemini: "autonomous apply ... using a runner-specific GPG key"
- Claude: Strategy 5 + 13 (cryptographic signatures on artifacts + workflow
  snapshots)
- Anon seed: implicit in Strategy 1
- Qwen: not stated explicitly, but consistent with the SQLite append-only
  event log already required by the product

### C6. The operator retains orchestration authority but loses source-write authority

- Codex: Invariant 6
- Gemini: "Operator is a high-level manager, delegating authorship to the lanes"
- Claude: Strategy 9 (capability discipline) — operator token vs lane token
- Anon seed: Strategy 4 (Inverted Control Plane) — operator gets workflow
  tools only
- Qwen: implicit; mechanical controls catch the bypass, but the operator
  surface is not redesigned. This is the weakest point of Qwen overlap and
  the strongest case for *combining* Qwen's mechanical detection with the
  other docs' capability discipline.

### C7. The product should expose mode names honestly; current behavior should not be called "true provenance"

- Codex: Phase 0 of roadmap, "Name the modes honestly"; proposes mode names
  `advisory`, `attested-bylines`, `sealed`
- Qwen: Recommended approach explicitly lists the question "is this default,
  opt-in, or a workflow-level provenance_mode setting?"
- Claude: §6 "honest categorical observation" — byte and intent provenance
  tractable, decision provenance is values
- Gemini: §4 frames as "Guidance → Governance"
- Anon seed: does not address mode naming

### C8. Phased rollout with sane sequencing

All five docs (where they propose a roadmap) order the phases the same way:

1. Be honest about what current mode delivers (mode taxonomy / mode flag).
2. Finish byline honesty (RFC 0026) and add retraction primitives.
3. Make patches first-class artifacts; bind verdicts to patch hashes.
4. Introduce the apply gate (initially in advisory enforcement; tightens later).
5. Add filesystem containment (the OS-level boundary).
6. Optional: signed commits, hardware anchor, external verification.

Claude's RFC 0027–0031, Codex's Phases 0–5, Gemini's "Sealed-Run Protocol",
and Qwen's "1 + 2" recommendation all collapse into this same order.

---

## 4. Cryptographic Primitives: Where the Five Diverge (Resolvable)

Different docs propose different cryptographic anchors. These are resolvable
and the consensus is straightforward.

| Doc | Anchor | Strength | Local-first? |
|---|---|---|---|
| Anon S1 | SHA256 ledger in SQLite | weak (operator can rewrite SQLite) | yes |
| Qwen S4 | HMAC-signed work packets | medium (shared secret) | yes |
| Claude S5 | Ed25519 runner master + ephemeral lane keys + nonce | strong | yes |
| Claude S10 | TPM/TEE-rooted master key | very strong | yes (with HW) |
| Codex | "Striatum signer" key outside operator workspace | strong | yes |
| Gemini RD-01 | Sigstore for lane-signed attestations | strong (with external root) | no |

**Resolved recommendation.** Use Ed25519 (or whatever the Sigstore-compatible
primitive ends up being) with:

- a **runner master key** generated at `striatum keys init`, public key
  committed to the repo, private key stored under a Unix user the operator
  cannot read;
- **ephemeral lane keypairs** generated per supervised session, signed by
  the master key, scoped to the lane's session;
- **per-session nonces** to prevent replay;
- **HMAC-signed work packets** as a *cheap secondary* layer that proves
  protocol adherence (operator must echo back to call `complete`).

This combines Claude S5 + Qwen S4 and is portable to Sigstore later (RD-01)
without rework. SHA256-only (anon S1) is rejected as the sole anchor because
it cannot survive SQLite tamper. Hardware (TEE) is deferred to research.

---

## 5. The Genuinely Open Questions

The five docs disagree, or do not address, the following. None are resolved
by the consensus.

### Open Q1. The intent loophole (lane dictation)

Even with C1–C8 fully implemented, an operator can still write the patch in
its head and feed it to a real attested lane as a literal diff in the lane's
task prompt. The lane transcribes; every gate passes; the multi-lane review
reviews the operator's pre-decided code. This is the **idea-laundering**
attack and is named in the Claude doc as "Lane-Dictation Bypass."

Positions across docs:

- **Claude S6: Channel Firewall.** Prevent dictation: enforce a grammar on
  operator → lane prompts that admits intent but rejects literal code blocks
  above a token budget. Lexical filter + LLM-based semantic firewall.
  Acknowledges this is an arms race and not bypass-proof.
- **Gemini S-C: Semantic Entropy Analysis.** Detect dictation post-hoc:
  measure log-probability of patch given prompt. Low information gain →
  flag as collusive. Acknowledges adversarial operators can over-specify
  prompts to defeat the metric.
- **Codex: Out of scope for current product.** Explicitly says
  "model-token provenance" is not achievable without changing the
  no-transcript-capture boundary (D028). Recommends documenting honestly
  rather than trying to enforce.
- **Qwen: Not addressed.**
- **Anon seed: Not addressed.**

**Synthesis.** The intent loophole is a *real and distinct* attack class
from the byte-level bypass. The consensus core (sealed provenance mode) does
NOT close it. Claude is the only doc that proposes a prevention mechanism;
Gemini proposes detection; Codex argues honest scoping.

**Recommendation.** Do NOT include the intent loophole in RFC 0027. Document
it explicitly as a known residual risk in the threat-model section
(following Codex's table). Open a separate research track to evaluate
Channel Firewall (prevention) vs. SEA (detection). Both are arms-race
strategies; neither is bypass-proof. The honest claim of sealed mode is
**process and patch provenance**, not **model-token provenance**.

### Open Q2. Decision provenance (operator-as-singleton intent authority)

Even with C1–C8 AND a perfect intent firewall, the operator is still the
single AI deciding which RFCs to write, which workflows to scaffold, which
review postures to require, and which lanes to spawn. Multi-lane execution
is faithful execution of one AI's plan.

Positions:

- **Claude §7 + Strategy 14: Quorum of operators on intent.** The only way
  to genuinely "remove the human" while not making the operator a singleton
  authority. Explicitly speculative. Claude itself recommends keeping the
  human at the intent layer (Mode B) as the realistic balance.
- **Codex: Implicit.** Codex's "no human in the loop" framing puts policy
  authority on the human and execution on the AI. Same as Claude Mode B.
- **Gemini: Aspirational "Zero-Human."** Does not propose how to escape
  the singleton problem.
- **Qwen, anon seed: Not addressed.**

**Synthesis.** This is a values question, not a containment problem. There
is no consensus on whether the singleton-operator problem must be solved.
The mechanical strategies in C1–C8 are valuable regardless of which answer
is chosen.

**Recommendation.** RFC 0027 should explicitly scope itself to *execution-
layer* provenance. Intent-layer provenance (Open Q2) is a separate product
decision and should not block RFC 0027.

### Open Q3. Hardware / provider anchoring (TEE, Sigstore, model attestation)

All five docs treat these as research-track:

- TEE (Claude S10): expensive, depends on CVM availability, fundamentally at
  odds with "small Python CLI on local repo" ethos.
- Sigstore (Gemini RD-01): introduces external root of trust; conflicts with
  "no cloud services" (D020) unless scoped narrowly.
- Watermark/MRTA (Claude S11, Gemini Strategy A): blocked on Anthropic/
  Google/OpenAI shipping public detection APIs.

**Recommendation.** Track as research; revisit when the relevant
infrastructure exists. Do not block RFC 0027 on any of these.

---

## 6. The One Product-Decision Blocker — RESOLVED (Option A)

**B1. Does sealed mode own commits?** — **Resolved 2026-05-10: Option A.**

Striatum's current SPEC boundary (`docs/SPEC.md`) says Striatum does not
commit, push, merge, or rebase. The five docs split on whether sealed mode
must change this:

- **Codex** flags it explicitly: "In sealed mode, Striatum may create a
  local signed commit only as the final apply step for a reviewed candidate
  tree. That should be an explicit product decision, not a side effect of
  this research note."
- **Gemini** assumes Striatum commits autonomously with a runner GPG key
  (Strategy B step 4: "the runner ... applies the patch and commits it
  using a runner-specific GPG key"). It does not flag this as a product
  change.
- **Claude** uses pre-commit hooks (RFC 0027 sketch): the operator/lane
  invokes git commit, the hook verifies signatures, and rejects if missing.
  This preserves "the operator commits" semantics but adds a signature
  gate.
- **Anon seed** Strategy 1 uses a pre-commit hook: same as Claude.
- **Qwen** does not directly address commits; it operates on file-system
  hashes pre- and post-job.

**Why this is a blocker.** The two paths (Striatum-as-committer vs.
hook-as-gate) produce different threat models:

| Path | Pros | Cons |
|---|---|---|
| Striatum-as-committer | Cannot be bypassed by `--no-verify`. Receipt is naturally embedded in commit metadata. Provenance survives `git clone`. | Crosses the current SPEC "no commits" boundary. Requires `core.hooksPath` lockdown or it can still be bypassed. Striatum needs a signing key. |
| Hook-as-gate | Keeps the current SPEC. Simpler. Operator workflow unchanged. | `--no-verify` defeats it. Hook can be deleted or modified by the operator unless filesystem isolation also covers `.git/hooks/`. |

**The honest synthesis.** The hook-as-gate path is *strictly weaker* unless
combined with filesystem isolation (separate Unix user, read-only mount of
`.git/hooks/`) — at which point it's nearly as complex as
Striatum-as-committer. If the project values the SPEC boundary, the only
coherent answer is filesystem-isolated hooks. If the project values
simplicity and is willing to grant a narrow exception, Striatum-as-committer
is cleaner.

**Decision (2026-05-10): Option A — Striatum-as-committer in sealed mode.**

Sealed mode introduces a narrow SPEC exception: Striatum may create a local
signed commit as the final apply step for a reviewed candidate tree.
Striatum still does not push, merge, or rebase under any mode.

Considered and rejected:

- **Option B** (hook-as-gate, "no commits" preserved). Rejected because
  `--no-verify` defeats it unless `.git/hooks/` is also covered by
  filesystem ACLs, and at that point Option B is roughly as complex as
  Option A while delivering a strictly weaker guarantee (the receipt does
  not survive `git clone`).
- **Option C** (support both, user chooses). Rejected as over-engineering
  for V1. May revisit after dogfood if a real use case appears.

Rationale for Option A:

1. The receipt survives `git clone` — downstream readers (humans, CI,
   later runs) can verify provenance from commit metadata alone, without
   access to Striatum's live daemon state.
2. The boundary is enforced by signature verification, not by filesystem
   ACLs on `.git/hooks/` that are easy to misconfigure across platforms.
3. Codex's apply-service invariants (I1–I4) already assume an entity
   outside the operator's authority performs the commit; Option A is the
   honest naming of that entity.
4. The SPEC exception is genuinely narrow: only `apply` may commit, only
   for an artifact that already satisfies every other gate, and only on
   the protected canonical tree. No autonomous push/merge/rebase.

**Action items for RFC 0027:**

- The "Apply execution" section assumes Striatum creates the commit and
  signs it with the apply-service key.
- A companion D-prefix decision lands alongside the RFC (text proposed in
  §10 below).
- The SPEC document gets a narrow carve-out in the "no commits" rule.

This unblocks RFC 0027.

---

## 7. Where the Documents Materially Disagree

Beyond the genuinely open questions, two smaller disagreements deserve
explicit resolution in the RFC.

### Disagreement 1. Default vs. opt-in for sealed mode

- **Qwen** suggests making worktree isolation default for repo-write lanes,
  noting D048 currently makes it opt-in.
- **Codex** keeps sealed mode strictly opt-in: "an opt-in operating mode
  for repositories that want hard provenance."
- **Claude** treats it as opt-in (RFC 0029 has an `--operator-isolated` flag).
- **Gemini, anon seed** do not address default-vs-opt-in.

**Resolution.** V1: opt-in. Existing workflows continue in advisory mode.
Sealed mode is selected at `striatum init --provenance-mode sealed` or via
a `provenance_mode: sealed` field in `workflow.json`. Revisit defaulting
after at least one dogfood cycle in sealed mode.

### Disagreement 2. Read-source surface for the operator

- **Anon seed Strategy 3** (Blind Operator) gives the operator
  `striatum_read_source` / `striatum_grep_source` tools and removes
  filesystem visibility of source entirely.
- **Codex** mounts source read-only into the operator's control workspace,
  so normal read tools still work. Plus `striatum source read|grep` for
  scripted access.
- **Claude RFC 0029** proposes read-source via CLI with a budget consumption
  (raw content requires an attestation that the read is necessary).
- **Gemini** does not specify.
- **Qwen** does not specify.

**Resolution.** V1: read-only mount of source into operator workspace (the
Codex shape). This is the lowest-friction and most ergonomic option, works
with any operator harness, and does not require a budget API. Claude's
budget mechanism is a possible refinement once usage data exists.

---

## 8. Coverage Matrix

For the RFC threat model, this matrix summarizes which docs cover which
attack class:

| Attack | Anon | Claude | Codex | Gemini | Qwen | Closed by consensus core? |
|---|---|---|---|---|---|---|
| Direct file edit | S1, S2 | S5, S2 | Inv 1, 4 | S-B | S1, S2 | ✅ yes |
| Shell-based edit (sed, here-doc) | S2 | S2, S5 | Inv 1 | S-B | S2 | ✅ yes |
| `git commit --no-verify` | — | partial | Inv 4 | S-B | — | ✅ yes (Option A: Striatum owns commit; operator's `git commit` is irrelevant) |
| Pre-commit hook patching | — | partial | Inv 4 | S-B | — | ✅ yes (Option A: apply gate, not hooks, is the boundary) |
| SQLite tamper | — | S5 | Inv 5 | — | — | ✅ yes (signed receipts) |
| Workflow.json mid-run mutation | — | S13 | partial | — | — | ✅ yes (signed snapshot) |
| Fake lane PID / supervisor | — | S5, S7 | partial | — | — | ✅ yes (ephemeral lane key) |
| **Lane prompt dictation** | — | S6 | scoped out | S-C | — | ❌ **Open Q1** |
| Context-doc smuggling | — | S6 | scoped out | — | — | ❌ Open Q1 |
| Plugin / skill-bundle tamper | — | partial | partial | — | — | ⚠️ partial; needs FS isolation |
| Heartbeat replay | — | partial | partial | — | — | ✅ existing lease lazy-expiry |
| Decision-tree capture | — | S14 | scoped out | — | — | ❌ **Open Q2** |
| TOCTOU race | — | partial | Inv 4 | — | — | ✅ yes (apply gate atomic) |
| OOB evidence transfer | — | partial | — | — | — | ⚠️ partial |

The consensus core closes 9 of 14 named attacks. The remaining 5 split
into: (a) Open Q1 / lane dictation, addressed only by Claude S6 and Gemini
S-C with no clear winner; (b) Open Q2 / decision capture, only addressed
speculatively by Claude S14; (c) edge cases (skill tamper, OOB transfer)
that filesystem isolation handles but the RFC should call out.

---

## 9. Recommended RFC 0027 Outline

Given the consensus, the following structure should produce a draftable RFC
once Blocker B1 is resolved. The outline directly maps to Codex's six
invariants plus Claude's mode taxonomy and Qwen's mechanical-floor
strategies.

### RFC 0027: Sealed Provenance Mode

**Status:** proposed (pending D-prefix decision on commit ownership)

**Summary**

Introduce a `provenance_mode` setting on workflows and runs. Three modes:

- `advisory` (current behavior, default for existing workflows)
- `attested-bylines` (RFC 0026 already lands this)
- `sealed` (this RFC)

In sealed mode, the canonical source tree is owned by Striatum; the
operator runs in a control workspace with read-only source visibility;
lanes write to scratch workspaces; Striatum captures diffs as immutable
patch artifacts; reviewers bind verdicts to patch hashes; an apply gate is
the only writer of the canonical tree; the apply produces a signed
provenance receipt.

**Scope**

In scope:
- Patch artifact as a first-class kind
- Hash-bound review verdicts
- Apply gate with explicit preconditions
- Provenance receipt (signed)
- Capability tokens for operator vs. lane CLI verbs
- Filesystem layout for sealed mode (control / source / scratch)
- Mode field on workflow + run
- Threat model and explicit residual risks

Out of scope (deferred):
- Channel Firewall / SEA (Open Q1 — separate research RFC)
- Operator quorum (Open Q2 — separate product decision)
- TEE / hardware anchoring (research)
- Sigstore / MRTA / watermarking (blocked on provider APIs)
- Defaulting sealed mode (revisit after dogfood)

**Invariants (numbered for tests)**

I1. Canonical repo writer is the Striatum apply service. Operator's UID
    receives `EACCES` on `write()` to protected paths.

I2. Lanes write only to `scratch/jobs/<job-id>/` under the lane process's
    UID. Operator's UID cannot write that directory.

I3. Verdicts that contribute to apply eligibility carry the reviewed patch
    SHA256. A verdict without it is recorded but cannot satisfy an apply
    gate.

I4. Apply is a CLI mutation with preconditions:
    - patch artifact exists and is immutable;
    - patch hash matches producer's recorded hash;
    - every required review verdict references that hash;
    - patch touches only allowed paths;
    - canonical tree is at the patch's recorded base tree;
    - required verification jobs passed for the candidate tree.

I5. Apply emits a signed receipt: `(run_id, workflow_hash, base_tree,
    result_tree, patch_artifacts, jobs, sessions, supervisors, verdicts,
    striatum_version, schema_version, timestamp)`.

I6. Operator capability tokens exclude source-write CLI verbs; lane tokens
    open them. Tokens are HMAC-signed by the runner master key, scoped per
    session.

**Cryptographic primitives**

- Ed25519 runner master keypair, generated by `striatum keys init`. Public
  key committed to `.striatum/keys/runner.pub`. Private key stored under
  the Striatum service UID, mode `0600`.
- Ephemeral Ed25519 lane keypair per supervised session, signed by the
  master key. Stored under the lane process's UID in `scratch/jobs/`.
- HMAC-signed work packets as a cheap protocol-adherence layer.
- Per-session nonces (random 16-byte tokens) bound to SQLite session rows.
- Signed receipt is the apply output; can later be verified offline.

**Filesystem layout in sealed mode**

```
project/
  control/                 # operator-visible, writable
    workflows/
    docs/rfcs/
    .striatum-client/
  source/                  # canonical protected repository (read-only mount in operator NS)
    .git/
    src/ tests/ docs/
    .striatum/
  scratch/
    jobs/<job-id>/         # writable by lane UID only
    patches/<artifact-id>/ # immutable after capture
```

Operator harness is launched with `bwrap` (Linux) or equivalent macOS/
Windows sandbox so that native `write_file`/`Bash` tools see `source/` as
read-only. Striatum service UID is distinct from operator UID; signing key
lives under service UID. (Cross-platform sandboxing is the largest
remaining engineering risk; macOS may require Apple's app sandbox or a
deliberate weaker fallback.)

**Schema migrations**

- `attestation_keys` table: master key fingerprint, lane key per session.
- `attestation_signatures` table: per-artifact signatures.
- `patch_artifacts` table: artifact id, base tree, result tree, patch
  SHA256, paths.
- `verdict_patch_bindings` column or table: verdict id ↔ patch SHA256.
- `provenance_receipts` table: run id, signed receipt blob.
- `workflow_snapshots.signature` column (Claude S13).
- `provenance_mode` column on `runs` and per-workflow setting.

**New CLI verbs**

- `striatum keys init | rotate | export-public`
- `striatum patch capture --run-id --job-id --workspace`
- `striatum verdict record --artifact-id <patch> --verdict ...` (extends existing)
- `striatum apply reviewed-patch --run-id --artifact-id`
- `striatum receipt show --run-id` / `striatum receipt verify --receipt-file`
- `striatum source read | grep` (operator-visible read API)
- `striatum hooks install | verify` (still useful for non-sealed users)

**Threat model section**

Reuse the Codex 7-row matrix and the Claude 17-row attack catalog. The RFC
must explicitly list the residual risks:

- **R1.** Local root or service-UID compromise defeats sealed mode.
- **R2.** Lane prompt dictation (Open Q1) is not prevented by sealed mode.
  Sealed mode delivers process and patch provenance, not model-token
  provenance.
- **R3.** Decision-layer capture (Open Q2) is not addressed.
- **R4.** Cross-platform sandboxing is uneven; macOS may degrade to
  POSIX-permissions-only.
- **R5.** Lane harness can run arbitrary commands within scratch
  workspaces; workflows wanting tighter assurance must constrain lane
  tools, network, and dependencies.

**Acceptance tests**

Codex's adversarial test list is suitable verbatim:

1. Operator write denial: native shell/file tools cannot write `source/`.
2. Operator cannot stage an unreviewed source change.
3. Lane scratch is inaccessible to operator UID.
4. Patch hash substitution fails: reviewers accept hash A, applying hash
   B is refused.
5. Base tree drift fails: rebase requires re-review.
6. Write-scope escape fails.
7. Supervisor death changes attestation.
8. Direct SQLite tamper is detected via receipt verification.
9. Review evidence export is self-contained.

Add Claude's adversarial cases for crypto:

10. Replay attack: harvested signature from prior job cannot satisfy a new
    job's apply gate (nonce mismatch).
11. Forged lane key: a lane public key not signed by the runner master is
    refused at apply.

**Phased delivery**

Phase 0 (no RFC needed): rename current claims; expose mode in `doctor`.

Phase 1 (this RFC): Patch artifacts + hash-bound verdicts + apply gate +
    signed receipts. Initially the apply gate runs as the same UID as the
    operator; sealed mode is documented but not yet bound by filesystem
    isolation. This is "advisory sealed mode" — it produces real receipts
    but does not prevent direct edits. Useful as evidence quality
    immediately.

Phase 2 (companion RFC 0028): Capability tokens for CLI verbs. Operator
    skills emit only operator-token verbs. Lane skills emit lane-token
    verbs.

Phase 3 (companion RFC 0029): Filesystem containment. Separate Unix user
    for Striatum service. Operator launched in `bwrap` / sandbox. Source
    mounted read-only. Scratch under lane UID. This is when sealed mode
    becomes a hard boundary.

Phase 4 (research): Channel Firewall or SEA for Open Q1.

Phase 5 (research / blocked): TEE, MRTA, Sigstore, watermarking.

---

## 10. Recommended Decision Log Entries (D-prefix, to land alongside RFC)

DECISION_LOG.md entries to write before or during RFC 0027:

- **D??? — Sealed mode commit ownership** (resolves B1; decision recorded
  2026-05-10):
  > Striatum may create a local signed commit only as the final apply step
  > in sealed provenance mode. The commit is created by the apply service
  > using the runner signing key. Striatum does not push, merge, or rebase
  > under any mode. This is a narrow exception to the SPEC "no commits"
  > rule and applies only to `provenance_mode: sealed`.
- **D??? — Provenance mode taxonomy**:
  > Workflows and runs carry a `provenance_mode` field. Valid values:
  > `advisory` (default; current behavior), `attested-bylines` (RFC 0026),
  > `sealed` (RFC 0027). The mode is exposed in `striatum status`,
  > `striatum doctor`, and the web UI.
- **D??? — Sealed mode opt-in**:
  > Sealed provenance mode is opt-in per workflow / per run. The default
  > `provenance_mode` is `advisory` to preserve existing behavior. Revisit
  > defaulting after at least one dogfood cycle.
- **D??? — Receipts are local-first**:
  > Provenance receipts are local-first by default. Optional external
  > verification (Sigstore, transparency log) is a future RFC and is not
  > part of sealed-mode V1.
- **D??? — Honest residual-risk naming**:
  > Lane prompt dictation (Open Q1) and decision-layer capture (Open Q2)
  > are known residual risks of sealed mode. The honest claim of sealed
  > mode is process and patch provenance, not model-token provenance and
  > not decision provenance. Separate research tracks address Channel
  > Firewall vs. Semantic Entropy Analysis (Open Q1) and operator-quorum
  > schemes (Open Q2).

---

## 11. Block / Proceed Recommendation

**Recommendation: proceed to RFC 0027 draft. No blockers remain.**

B1 is resolved (Option A — Striatum-as-committer in sealed mode; see §6).
The consensus is strong enough that the RFC's invariants, schema, CLI, and
acceptance tests can be drafted directly from §9.

The two genuinely open questions (Q1 lane dictation, Q2 decision capture)
do NOT block RFC 0027 because:

- They are correctly out of scope for execution-layer provenance.
- Sealed mode is honest about what it does and does not deliver
  (process + patch provenance, not model-token or intent provenance).
- They deserve separate research RFCs once V1 is in dogfood.

**Status: unblocked. Ready to draft RFC 0027.**

---

## 12. Where Each Document Made the Distinctive Contribution

For credit and traceability:

- **Codex GPT-5** (`P005_TRUE_PROVENANCE_LOOPHOLE_RESPONSE.md`): the six
  invariants, the threat-model matrix, the mode taxonomy
  (advisory/attested-bylines/sealed), the phased roadmap, the
  acceptance-test list. This is the architectural backbone of the
  synthesis.

- **Claude Opus 4.7** (`OPERATOR_BYPASS_DEFENSE_IN_DEPTH.md`): the
  byte/intent/decision three-layer model that names Open Q1 and Open Q2;
  the cryptographic anchor recommendation (Ed25519 + ephemeral lane keys
  + nonce); the Channel Firewall as the one proposed prevention mechanism
  for the intent loophole; the attack catalog of 17 rows.

- **Gemini 1.5 Pro** (`P005_GEMINI_ANALYSIS_ABSOLUTE_PROVENANCE.md`): the
  cleanest naming of three primitives (MRTA, D-Apply, SEA); the
  information-theoretic framing of dictation detection; the "from Guidance
  to Governance" tagline that the RFC can borrow.

- **Qwen3.6** (`P005_PROVENANCE_BYPASS_STRATEGIES.md`): the strongest
  grounding in current striatum state (D020, D028, D048 cited correctly);
  the recognition that worktree isolation already partially solves C2; the
  mechanical post-job checksum strategy as a useful Phase-1-minus
  deliverable that does not require sealed mode.

- **Anonymous seed** (`TRUE_PROVENANCE_AND_CONTAINMENT.md`): the framing
  that triggered the rest; Strategy 4 (Inverted Control Plane) as the
  cleanest operator-side UX answer; Strategy 3 (Shadow Worktrees) as the
  contextual containment that informs the read-source design.

---

## 13. Final Honest Statement

The five-document consensus answers the seed-prompt question — *"How can a
system with a local control plane ensure True Provenance when the
cloud-based AI responsible for driving the orchestration possesses
harness-native tools that allow it to bypass the local orchestration?"* —
with the following honest formulation:

> Striatum can guarantee **patch and process provenance** (every byte in
> the canonical tree entered through a Striatum apply gate that referenced
> an attested lane's immutable patch and the configured review verdicts
> over the exact submitted patch). This is mechanical and local-first.
>
> Striatum cannot guarantee **model-token provenance** (the LLM, and not
> some other process or human, generated these exact bytes) without
> changing the no-transcript-capture boundary or depending on external
> provider attestation infrastructure that does not yet exist at the
> required maturity.
>
> Striatum cannot guarantee **decision provenance** (the orchestrating AI
> made these design decisions independently rather than under a single
> entity's influence) by any mechanical means. This is a values question
> resolved by either (a) keeping the human in the loop at the intent layer
> only, or (b) operator-quorum schemes that are speculative.

The seed-prompt's stated goal — "safely remove the human from the loop,
relying entirely on the multi-lane model hierarchy" — is partly achievable
and partly a category error. The realistic target is: *the human's
remaining surface in the loop is intent acceptance, and execution is
mechanically faithful to that intent.* That target is exactly what sealed
provenance mode delivers.

RFC 0027 should make this scope explicit. Anything more is overpromising.
