# P005 Response: True Provenance Requires Moving the Trust Boundary

author: researcher-codex-gpt-5-001
Status: exploratory research
Date: 2026-05-10
Source prompt: [`prompts/P005_true_provenance_loophole.md`](../../../../prompts/P005_true_provenance_loophole.md)
Related context:
[`docs/rfcs/0026-lane-attestation-and-operator-byline-honesty.md`](../../../rfcs/0026-lane-attestation-and-operator-byline-honesty.md),
[`docs/research/TRUE_PROVENANCE_AND_CONTAINMENT.md`](../../../research/TRUE_PROVENANCE_AND_CONTAINMENT.md),
[`docs/dogfood/FRICTION_LOG.md`](../../../dogfood/FRICTION_LOG.md)

Current-context note (2026-05-17): this exploratory response predates the
D094/D104 daemon-required runtime. References below to SQLite or CLI-only
state are historical; current Striatum live state is daemon-owned PostgreSQL
and CLI/MCP/web surfaces are clients of the daemon boundary.

## Executive thesis

The operator bypass cannot be solved inside the current plugin contract. If
the operator's native harness can write the target repository and can also
drive Striatum's CLI, then Striatum can at best make bypasses visible after
the fact. It cannot guarantee true provenance.

The desired end state of "no human in the loop" makes this stricter, not
looser. Removing the human removes the informal judgment layer that can notice
when an AI operator has taken a shortcut. The autonomous system therefore
needs harder mechanical boundaries than a human-operated workflow: the
top-level AI can plan and route work, but it must be technically unable to
author protected source bytes outside the workflow.

The reason is simple: the operator is inside the same trust domain
as the repository, the daemon/PostgreSQL state store, the hooks, and the code that
performs validation. Any enforcement rule placed inside that domain is just
another file or process the operator can route around. Prompt instructions,
skill bundles, byline checks, and advisory driver plugins improve behavior,
but they do not create a hard boundary.

To achieve the P005 objective, Striatum must move from "the operator is a
privileged shell that promises to use the workflow" to "the operator is an
untrusted planner that can only request changes through a smaller trusted
writer." The canonical source tree must become a protected resource owned by
Striatum, not a normal working directory owned by the operator harness.

The recommended shape is an opt-in **sealed provenance mode**:

1. Striatum, not the operator, owns the writable canonical repository.
2. Operators run in a control workspace with workflow tools and read-only
   source inspection, but no source-write capability.
3. Agent lanes run in isolated per-job scratch worktrees or overlays.
4. Lanes produce patch artifacts or tree deltas that Striatum records with
   job, session, supervisor, base-tree, and content hashes.
5. Review verdicts bind to exact patch hashes, not just job names.
6. Only a Striatum apply service can mutate the canonical source tree, and
   only after the configured review gates pass.
7. The final repository state is sealed with a provenance receipt that can be
   verified later against the run graph.

This does not require Striatum to become hosted or to capture broad model
transcripts, but it does require a product decision: absolute provenance is
not compatible with giving the top-level operator unrestricted write access to
the same repository it is supposed to police.

## No-human-in-the-loop target state

The target is not "a human stops reviewing and the AI operator gets more
trust." The target is "human discretion is replaced by mechanical controls
that the AI operator cannot bypass."

In that state:

- The human may define product goals, policy, and workflow templates, but is
  not required to inspect each change before it lands.
- The top-level AI operator is an untrusted autonomous planner. It can propose
  work, start runs, inspect status, recover stale workflow state, and explain
  outcomes.
- The operator cannot write protected source files, cannot write lane scratch
  workspaces, cannot approve its own unreviewed patch by editing state, and
  cannot turn an unreviewed diff into the canonical tree.
- Attested implementation lanes create candidate patches inside bounded
  workspaces.
- Attested review lanes review immutable patch or candidate-tree hashes.
- Striatum applies only the exact objects that satisfy workflow policy.
- The resulting tree carries a receipt that lets a later reader verify the
  route from accepted bytes back through jobs, sessions, supervisors, verdicts,
  and verification results.

This is a stronger requirement than ordinary local workflow coordination. A
human-operated workflow can sometimes tolerate advisory controls because a
human can notice drift and decide what to do. A fully autonomous workflow
should assume the AI operator will optimize toward task completion and
therefore needs source-write denial, patch escrow, and apply-time proof as
part of the core architecture.

## The important distinction: provenance of bytes vs provenance of claims

RFC 0026 closes an important gap: it prevents an unattested session from
minting a model-typed byline such as `reviewer-codex-gpt-5.5-001`. That is
byline honesty. It ensures the claim in the artifact header is not silently
stronger than what the runner observed.

P005 asks for something stronger: every accepted source change must be known
to have entered through an attested Striatum job and the required adversarial
review path. That is byte provenance. The difference matters:

- **Byline provenance** answers: "Was this artifact allowed to claim it came
  from this lane?"
- **Process provenance** answers: "Was a supervised process for this lane
  alive when this work was submitted?"
- **Patch provenance** answers: "Did these exact bytes enter the repository
  through this job, from this base tree, under these review gates?"
- **Model-token provenance** would answer: "Did this LLM, and not some other
  process or human, generate these exact bytes?"

Striatum can plausibly enforce patch provenance locally. It cannot honestly
claim model-token provenance without changing the no-transcript-capture
boundary, integrating with provider-side attestation, or constraining the
lane harness so tightly that all tool calls and outputs become part of the
audited protocol. For the current product, the defensible target is:

> Every byte in the protected repository tree was introduced by a sealed
> Striatum apply operation that references an attested lane submission and the
> configured review verdicts over the exact submitted patch.

That is strong enough to solve the operator bypass. It is also a more stable
target than "every line," because line identity changes under formatting,
renames, generated files, and refactors. The durable proof should bind Git
tree objects, file blobs, patch hunks, and run graph edges. Line-level views
can be derived for humans, but the invariant should be tree-level.

## Impossibility result under the current trust domain

Assume the following:

1. The operator harness can write arbitrary files in the target repository.
2. The operator harness can run arbitrary shell commands as the same OS user.
3. The Striatum database, hooks, worktree, and Python source are writable by
   that same OS user.
4. The operator is the entity deciding whether to use its native tools or the
   Striatum driver plugin.

Under those assumptions, absolute provenance is impossible.

The operator can bypass at multiple layers:

- It can edit `src/` or `tests/` directly and then ask Striatum to record
  success on a loosely related job.
- It can stage changes and run `git commit --no-verify` if the only barrier
  is a local client-side hook.
- It can patch or delete the hook before committing.
- It can edit files after Striatum records an artifact hash.
- It can attempt to tamper with the daemon/PostgreSQL state authority or
  replace the local Striatum code that performs validation.
- It can create a fake supervisor process, keep it alive, and ghost-write the
  artifact unless the artifact submission path is also isolated.
- It can generate a patch outside Striatum and submit it through an attested
  session unless the patch-producing workspace is inaccessible to the
  operator.

This is not an LLM-specific problem. It is a standard authority problem:
the party being constrained has write access to the objects that express the
constraint. The only way out is to move at least one of the following outside
the operator's authority:

- the canonical source tree;
- the apply operation;
- the provenance ledger;
- the commit/reference update;
- the lane scratch workspace that produces the submitted patch.

The rest of this document assumes Striatum wants a local-first answer and
therefore treats a local Striatum daemon, separate OS users, read-only mounts,
and cryptographic receipts as acceptable building blocks. A fully adversarial
local root user remains out of scope unless Striatum adds a remote or hardware
trust anchor.

## Required invariants for true provenance mode

A true-provenance mode should be designed around invariants rather than
around UI conventions.

### Invariant 1: The canonical repository has one writer

The canonical source tree must be writable only by Striatum's trusted apply
component. The operator may create workflows, request runs, inspect status,
and read source through approved tools, but it cannot write `src/`, `tests/`,
examples, or product docs directly in the protected tree.

This is the single most important change. Without it, every other mechanism
is advisory.

### Invariant 2: Lanes do not write the canonical tree directly

Even attested lanes should not mutate the canonical tree while work is in
progress. They should write a per-job scratch worktree, overlay, or patch
workspace. Striatum then captures the resulting delta and records it as a
first-class patch artifact.

This prevents a successful lane from becoming an uncontrolled side channel.
It also gives reviewers a stable object to review.

### Invariant 3: The reviewed object is the applied object

Review jobs must bind verdicts to exact patch hashes or tree hashes. A verdict
that says "accept job X" is weaker than a verdict that says "accept patch
SHA256 abc from base tree def for job X."

The apply step must refuse if the patch hash differs from the reviewed hash,
if the base tree changed without rebase/re-review, or if required verdicts are
missing.

### Invariant 4: The apply step is a state transition, not a shell habit

"The operator applies the patch after review" is the same loophole with a
different label. Apply must be a Striatum mutation with explicit preconditions:

- the run is in the right state;
- the job graph says this apply job is eligible;
- the patch artifact exists and matches its recorded digest;
- the patch is within declared write scope;
- required reviews and postures exist for that exact digest;
- verification commands passed, if the workflow requires them;
- the canonical tree is still at the expected base.

Only then should Striatum write the canonical tree or update the protected
Git reference.

### Invariant 5: The provenance receipt is tamper-evident

The accepted tree state should have a receipt that can be verified later
without trusting a conversational transcript. At minimum, record:

- run id;
- workflow id and workflow content hash;
- parent tree hash;
- resulting tree hash;
- patch artifact ids and digests;
- producer job ids, lane ids, session ids, supervisor ids, command digests,
  and attestation state at submission time;
- review job ids, verdicts, review postures, and the reviewed patch digest;
- verification command ids and outcomes;
- Striatum version and schema version;
- receipt timestamp.

For a stronger local boundary, sign the receipt with a key held by the
Striatum apply service, not by the operator harness. The key can remain local,
but it must be outside the operator's writable workspace.

### Invariant 6: The operator can orchestrate, but not author protected bytes

The product should preserve Striatum's useful operator flow: a human or AI can
draft RFCs, scaffold workflows, start runs, recover stale work, and ask for
status. The restriction is narrower: the operator cannot introduce protected
source bytes except by creating work for attested lanes and letting the apply
gate accept their patch artifacts.

This is the autonomy boundary. A no-human-in-the-loop deployment may give the
operator broad authority to decide what work to attempt next, but it should
not give the operator authority to decide that its own unreviewed bytes are
now source truth. That decision belongs to the workflow graph and the apply
gate.

This distinction keeps the product usable while removing the bypass.

## Recommended architecture: sealed provenance mode

Sealed provenance mode is an opt-in operating mode for repositories that want
hard provenance. It can coexist with today's simpler local workflow mode.

### Actors

**Operator harness**

The external CLI or chat harness used by the human or top-level AI. It is
treated as untrusted for source writes. It can call Striatum control verbs and
read source through constrained inspection APIs.

**Striatum control plane**

The deterministic SQLite and Python runner. In sealed mode, it is reached
through a local daemon or command wrapper that owns the writable repository
and the provenance signer.

**Striatum apply service**

The only component allowed to mutate the canonical repository or protected Git
reference. It may be the same process family as the local service, but it must
run with filesystem authority the operator does not have.

**Attested lane**

A supervised process spawned by Striatum for a declared lane. In sealed mode,
the lane gets a job workspace with write access only to its allowed paths in a
scratch tree, not to the canonical tree.

**Reviewer lane**

A supervised lane that reviews an immutable patch artifact or tree delta. It
does not review an ambient working directory whose contents can change under
it.

### Filesystem shape

One possible local layout:

```text
project/
  control/                 # operator-visible control workspace
    workflows/
    docs/rfcs/
    .striatum-client/
  source/                  # canonical protected repository
    .git/
    src/
    tests/
    docs/
    .striatum/
  scratch/
    jobs/<job-id>/         # per-job worktrees or overlays
    patches/<artifact-id>/ # immutable captured patches
```

The exact paths are not important. The permissions are:

- `control/` is writable by the operator.
- `source/` is readable by the operator only through either filesystem
  read-only mounts or Striatum read APIs; it is writable by the apply service.
- `scratch/jobs/<job-id>/` is writable by the lane process for that job, not
  by the operator.
- `scratch/patches/` is immutable after capture.

On Linux, this can be implemented with separate Unix users, `chown`, POSIX
ACLs, read-only bind mounts, `bwrap`, overlayfs, Docker, or another local
sandbox. The important property is not the particular technology; it is that
the operator's native `write_file` and shell tools receive `EACCES` when they
try to write protected source paths.

### Control surface

The operator gets tools like:

- `striatum workflow init`;
- `striatum run start`;
- `striatum status`;
- `striatum dashboard`;
- `striatum source read <path>`;
- `striatum source grep <pattern>`;
- `striatum propose-rfc`;
- `striatum recovery ...`;
- `striatum chat ...`, if using the local web/chat surface.

The operator does not get a source write tool. If the operator is running in a
general-purpose coding harness, the harness must be launched in a mode where
its native write tools cannot write the protected source tree. A driver plugin
alone is not enough.

### Lane execution

A write-capable job proceeds as follows:

1. Striatum validates the workflow and write scope.
2. Striatum creates a scratch worktree from the current protected base tree.
3. Striatum exposes only the job's allowed paths as writable inside the lane
   workspace. Other paths are read-only or absent.
4. Striatum starts the lane supervisor and records process attestation.
5. The lane performs the work inside the scratch workspace.
6. On completion, Striatum computes the diff between the scratch workspace and
   the base tree.
7. Striatum rejects the diff if it touches paths outside write scope.
8. Striatum stores the diff as an immutable patch artifact with base-tree,
   result-tree, file, blob, and hunk hashes.
9. The work job completes by referencing the patch artifact, not by mutating
   the canonical source tree.

The lane may still use its native tools inside its scratch workspace. That is
fine. The claim is not "the model emitted these bytes directly from tokens."
The claim is "the bytes were produced inside the attested lane's bounded job
workspace and entered the protected repository only through Striatum."

### Review execution

Review jobs receive the patch artifact, base tree metadata, and any relevant
context. Their verdicts must record the reviewed patch digest.

For example, a review verdict should conceptually say:

```json
{
  "job_id": "review-parser-fix",
  "verdict": "accept_with_conditions",
  "reviewed_patch_sha256": "abc123...",
  "review_posture": "security",
  "session_id": "sess_..."
}
```

If a patch is rebased, regenerated, or edited, it receives a new digest and
must be reviewed again according to workflow policy. The run graph should make
this explicit.

### Apply execution

An apply job is eligible only after required producer and reviewer jobs have
completed. It performs these checks:

1. The patch artifact exists and is immutable.
2. The patch hash matches the hash recorded by the producer job.
3. Every required review verdict references the same patch hash.
4. The patch touches only allowed paths.
5. The canonical source tree is still at the patch's recorded base tree, or
   the workflow has an explicit rebase/re-review path.
6. Required verification jobs passed for the exact tree candidate.
7. The run has not been canceled, compromised, or paused in a way that forbids
   apply.

Then the apply service writes the canonical tree and emits a provenance
receipt. If Striatum also owns Git commits in this mode, the commit should be
created by the apply service and signed by the Striatum provenance key.

### Resulting guarantee

If the OS permissions and Striatum service boundary hold, a native operator
edit cannot reach the protected source tree. The only path from "idea" to
"accepted source bytes" is:

```text
operator intent
  -> workflow job
  -> attested lane scratch workspace
  -> immutable patch artifact
  -> hash-bound review verdicts
  -> Striatum apply gate
  -> protected tree update
  -> signed provenance receipt
```

That is the structural answer to P005.

## Strategy analysis

### 1. Client-side Git hooks are useful but not sufficient

A pre-commit hook that rejects unprovenanced changes is a good early tripwire.
It helps good-faith operators notice when they have drifted outside the
workflow. It is not an absolute control because the operator can use
`--no-verify`, modify the hook, bypass Git entirely, or alter the working tree
after a Striatum hash is recorded.

Hooks become stronger only when the hook is outside the operator's authority:

- a server-side `pre-receive` hook on a protected remote;
- a local bare repository owned by a different OS user;
- a Striatum-owned apply service that is the only writer of protected refs;
- branch protection rules in a hosted forge.

Hosted branch protection may be the cleanest industry pattern, but it crosses
Striatum's current no-hosted-services boundary and would need a product
decision. A local bare repo owned by the Striatum service preserves the
local-first posture while still avoiding the operator's `--no-verify` escape.

### 2. Filesystem isolation is the first hard local boundary

The most direct local fix is to make the protected source tree read-only to
the operator harness. This can be done with:

- separate Unix users for operator, lane, and Striatum service;
- POSIX permissions and ACLs;
- read-only bind mounts;
- `bwrap` or another mount namespace tool;
- Docker or Podman volumes;
- platform-specific sandboxing on macOS or Windows.

This should be treated as a product mode, not a hidden implementation detail.
Users need to know when they are in advisory mode versus sealed mode.

The limitation is also clear: if the operator can escalate to the Striatum
service user or local root, local containment is gone. To defend against that,
the trust anchor must move to hardware, a remote protected repository, or a
separate machine. That is a different product boundary.

### 3. Patch escrow is better than direct lane writes

Letting lanes write the canonical repository directly creates audit ambiguity:
the repository changes before reviews happen, and the operator may still
modify the same files between lane completion and review.

Patch escrow is cleaner. Lanes work in scratch space. Striatum captures their
delta. Reviewers review the delta. The apply service applies the delta. Every
important object has a digest.

This also solves a subtle problem with RFC 0026's lane attestation. A live
supervised process proves that a lane process exists; it does not prove that
the operator did not write the artifact file. If the patch workspace is
writable only by the lane process and captured by Striatum, the operator has
less room to ghost-write under a real supervisor.

### 4. Hash-bound reviews prevent review/apply substitution

Review loops are weak if they review a moving target. The reviewer may have
accepted an earlier patch while the applied patch contains later operator
edits.

Every verdict that affects apply eligibility should bind to the exact object
it reviewed:

- patch digest;
- base tree hash;
- candidate result tree hash;
- optionally, normalized diff digest that ignores irrelevant metadata.

Synthesis artifacts should also bind to the selected patch digest when they
turn multiple findings into an implementation plan. This gives downstream
readers a transitive chain from final tree to review evidence.

### 5. A Striatum-owned commit path makes provenance visible to Git users

If true provenance means "every accepted repository state is provenanced,"
then the strongest user-facing boundary is the Git commit or protected ref.

In sealed mode, Striatum could create commits itself after apply:

```text
commit <hash>
tree <result-tree>
parent <base-commit>

Striatum-Provenance-Run: run_...
Striatum-Provenance-Receipt: sha256:...
Striatum-Workflow-Hash: sha256:...
Striatum-Reviewed-Patches: sha256:abc..., sha256:def...
```

The commit can be signed by a local Striatum key. The receipt can be stored in
the repository, in Git notes, or in `.striatum/` plus an exported artifact
bundle. Each storage choice has tradeoffs:

- Repository-tracked receipts travel with source history but add commit noise.
- Git notes avoid file churn but are easier to drop in clones.
- SQLite is convenient locally but does not travel with normal Git history.
- A transparency log is strongest but no longer purely local.

The product should decide whether sealed mode owns commits. The current SPEC
boundary says Striatum does not commit, push, merge, or rebase. True
provenance does not strictly require Striatum to own commits if it owns the
protected working tree, but commits are the natural durable checkpoint for
humans and CI systems.

### 6. Inverted control plane removes the bad default

The cleanest UX is to make Striatum the primary interface for managed work:
a local web UI, TUI, or chat surface where the top-level AI only receives
workflow and inspection tools.

This removes the "frontier model in a general coding harness" problem by not
giving the top-level model source-write tools in the first place. The model
can plan, ask questions, draft workflows, and start runs. It cannot edit
protected source files.

External harness support can still exist, but it should be explicitly labelled
as one of two modes:

- **Driver mode:** convenient, advisory, not true provenance.
- **Sealed driver mode:** launched in a sandbox/control workspace where native
  write tools cannot write the protected source.

Without that distinction, users will overestimate what the plugin enforces.

### 7. Detection mechanisms are useful only as belts and suspenders

File watchers, checksums, audit logs, `git status` reconciliation, and
doctor checks are valuable. They can detect accidental drift and produce good
diagnostics. They should not be described as true-provenance enforcement.

A watcher runs after the write has happened. If the writer can also stop the
watcher, rewrite its state, or edit the files it watches, detection is not a
hard guarantee. In sealed mode, watchers should monitor the protected tree and
raise compromise events, but the prevention control remains write denial.

## Non-solutions

The following ideas are useful in narrow ways but do not solve P005 by
themselves.

### Stronger prompts or skills

They reduce accidental bypass. They do not constrain native write tools.

### A better driver plugin

A plugin is just another tool unless the harness is configured so it is the
only tool that can write protected source. The plugin can be the interface to
the trusted writer, but it cannot be the trust boundary.

### RFC 0026 alone

RFC 0026 is necessary byline hygiene. It prevents false model claims under
unattested sessions. It does not stop the operator from editing source files
directly.

### Client-side pre-commit hooks alone

They are bypassable by the same user. They are a tripwire, not a boundary.

### Capturing transcripts alone

Transcript capture may improve auditability, but it still does not prevent
the operator from editing files outside the captured transcript. It also
conflicts with an existing Striatum product decision unless scoped tightly.

### Reviewing the operator's final diff

This catches some errors, but it is not the claimed workflow. The code was
still authored through the bypass path. If accepted, the provenance should say
"operator-authored diff reviewed after the fact," not "attested lane output."

### Trusting model self-reports

An LLM saying it used Striatum is not evidence. The system must rely on
objects outside the model's control: process ids, filesystem permissions,
patch hashes, tree hashes, and apply receipts.

## Threat model matrix

| Threat model | Minimum useful control | Honest claim |
|---|---|---|
| Good-faith human operator occasionally mistakes commands | RFC 0026, warnings, hooks, `doctor`, visible bylines | Best-effort workflow hygiene |
| Good-faith AI operator optimizing for speed | Driver plugin plus read-only source sandbox | Strong prevention of accidental bypass |
| AI operator has same-user shell and source write access | No local in-repo control is sufficient | Cannot guarantee true provenance |
| AI operator has same-user shell but source is owned by Striatum service | OS permissions, sealed apply, patch escrow | Local hard provenance against unprivileged operator |
| Operator can become the Striatum service user or root | Local controls can be bypassed | Need external or hardware trust anchor |
| Lane harness is malicious or compromised | Lane sandboxing, network policy, command digesting, review diversity | Process provenance only, not model honesty |
| Striatum code or package supply chain is compromised | signed releases, reproducible install, code review, isolated service user | Outside normal workflow provenance |

This matrix should appear in any future RFC because it prevents the product
from promising more than the selected controls can deliver.

## Proposed domain model additions

Sealed provenance mode would add several domain concepts.

### Protected repository

A repository tree whose writes are mediated by Striatum's apply service. This
is distinct from an ordinary target repository in advisory mode.

### Control workspace

The operator-visible workspace used to draft workflows, inspect status, and
request runs. It may contain workflow fixtures and docs, but it is not the
canonical protected source tree.

### Patch artifact

An immutable artifact representing a candidate source change from a specific
base tree. It has identity, digest, producer job, producer session,
attestation snapshot, write-scope validation result, and optional hunk-level
metadata.

### Candidate tree

The result of applying one or more patch artifacts to a base tree in a
deterministic order. Verification and reviews can target either individual
patches or candidate trees.

### Apply gate

The aggregate boundary that decides whether a candidate tree may become the
protected tree. It consumes patch artifacts, verdicts, workflow policy, and
verification outcomes.

### Provenance receipt

A tamper-evident record that binds a protected tree update to the run graph
and the exact objects reviewed.

### Provenance mode

A repository mode:

- `advisory`: current simple CLI behavior; good workflow hygiene but no hard
  containment of an external operator.
- `attested-bylines`: RFC 0026-style honesty about lane claims.
- `sealed`: protected repository, patch escrow, hash-bound review, and
  Striatum-only apply.

These names are illustrative. The important product point is to expose the
mode clearly so users know what guarantee they are receiving.

## Protocol sketch

The exact CLI names are future design work, but a concrete sketch helps test
the architecture.

### Capture patch

```text
striatum patch capture \
  --run-id <run> \
  --job-id <job> \
  --session-id <session> \
  --workspace <scratch-worktree>
```

Preconditions:

- session owns a live lease for the job;
- session is lane-attested if the workflow requires it;
- workspace was provisioned by Striatum for this job;
- workspace owner matches the lane identity or sandbox identity;
- diff is non-empty unless the job allows no-op completion;
- diff is inside write scope.

Output:

```json
{
  "artifact_id": "art_patch_...",
  "kind": "patch",
  "base_tree": "git-tree-sha1-or-sha256",
  "result_tree": "git-tree-sha1-or-sha256",
  "patch_sha256": "sha256...",
  "paths": ["src/parser.py", "tests/test_parser.py"]
}
```

### Record verdict over patch

```text
striatum verdict record \
  --job-id <review-job> \
  --session-id <review-session> \
  --artifact-id <patch-artifact> \
  --verdict accept_with_conditions
```

The event payload stores the patch digest. A verdict without the digest should
not count for apply.

### Apply reviewed patch

```text
striatum apply reviewed-patch \
  --run-id <run> \
  --artifact-id <patch-artifact>
```

Preconditions:

- caller is the apply service, or the CLI is proxying to it;
- all review policy gates for the patch digest are satisfied;
- candidate tree verification passed;
- protected repository is at the expected base tree.

Output:

```json
{
  "applied": true,
  "base_tree": "...",
  "result_tree": "...",
  "receipt_sha256": "...",
  "commit": "optional"
}
```

## How this interacts with current Striatum decisions

### Local-first state

Sealed mode can remain local-first. The service, SQLite database, protected
repo, scratch workspaces, and signer can all live on the local machine. The
difference is that the local machine has more than one authority boundary.

The caveat is that local-first does not mean adversarial-root-proof. If the
same user or local root can rewrite everything, absolute guarantees require a
remote or hardware trust anchor.

### No broad transcript capture

Patch escrow does not require transcript capture. Striatum records the patch
payload and the process/workspace attestation, not the full conversation
between a lane and its model provider.

This preserves the privacy posture better than recording every model token.
It also makes the provenance object smaller and easier to review.

### No autonomous commits

The current SPEC boundary avoids commit/push/merge/rebase. Sealed provenance
can be implemented without auto-commits by applying to a protected working
tree and emitting receipts. However, commit-level sealing is the natural
durable boundary. If the product wants the guarantee to survive normal Git
movement, it should consider a narrow exception:

> In sealed mode, Striatum may create a local signed commit only as the final
> apply step for a reviewed candidate tree.

That should be an explicit product decision, not a side effect of this
research note.

### Workflow write scopes

Write scopes become enforceable mechanically rather than just through CLI
validation. The scratch workspace mount can make allowed paths writable and
all other paths read-only. Striatum still validates the resulting diff because
mount rules and workflow rules should agree.

### Reviewer independence

True provenance does not automatically solve reviewer independence. It only
ensures the reviewed patch and the applied patch are the same object. Workflow
policy still needs distinct lanes, postures, and review gates.

### Dogfood and historical artifacts

The dogfood friction log shows a good-faith provenance failure: artifacts were
authored by the operator while bylines claimed another lane. RFC 0026 addresses
that class at the byline level. P005 sealed mode addresses the stronger class
where source bytes bypass the runner entirely.

The two should not be conflated. A repository can have honest bylines and
still allow direct source edits. Conversely, a sealed repository can prevent
direct source edits while still needing better byline/retraction semantics for
historical artifact truth.

## Implementation roadmap

### Phase 0: Name the modes honestly

Document the difference between advisory operation and sealed provenance.
Avoid saying "true provenance" for the current plugin-driven shape.

Deliverables:

- update docs to classify guarantees by mode;
- expose current mode in `doctor`, `status`, and web UI;
- warn when an operator claims sealed behavior but the source tree is writable.

### Phase 1: Finish byline honesty and retraction primitives

Land RFC 0026 or an equivalent attestation/byline model. Add the companion
retraction or compromised-run primitive for historical incidents.

Deliverables:

- lane-attestation-aware bylines;
- `author: operator` for unattested sessions;
- `require_attested_lane` for workflows that need hard refusal;
- compromised/retracted run state for known bad evidence.

This does not solve P005, but it prevents current evidence from overstating
itself.

### Phase 2: Introduce patch artifacts and hash-bound reviews

Before building heavy sandboxing, make patch identity first-class. This can
start in advisory mode.

Deliverables:

- patch artifact kind;
- patch digest and base tree metadata;
- verdicts that reference patch digests;
- apply eligibility checks over patch digests;
- evidence export that can show "this review accepted this exact patch."

This phase improves provenance even before source writes are fully sealed.

### Phase 3: Add protected apply

Introduce an apply gate that is the preferred way to write reviewed patches.
Initially it may run as the same user and provide detection rather than hard
containment. The API shape should already match sealed mode.

Deliverables:

- `striatum apply` or service endpoint;
- provenance receipts;
- candidate tree verification;
- refusal when reviewed hash and applied hash diverge.

### Phase 4: Add filesystem containment

Make sealed mode real by changing permissions.

Deliverables:

- protected source tree owned by Striatum service user;
- operator control workspace;
- read-only source inspection tools;
- per-job scratch workspaces;
- lane workspaces writable only by the supervised lane identity;
- source-write denial tests for operator harnesses.

This is the phase where P005's bypass becomes mechanically blocked.

It is also the phase where "no human in the loop" becomes a credible product
claim. Before containment, autonomy is mainly a behavioral convention. After
containment, the autonomous operator can be useful without being trusted as a
source writer.

### Phase 5: Optional commit signing and external verification

If the product wants provenance to survive outside the local workspace, add a
commit or receipt verification story.

Deliverables:

- local Striatum signing key held outside operator workspace;
- signed receipts;
- optional signed commits;
- optional remote branch protection or transparency-log integration behind an
  explicit product decision.

## Acceptance tests for a future RFC

A serious RFC should include adversarial tests, not only happy paths.

### Operator write denial

Given sealed mode is enabled and the operator harness runs in its control
workspace, an attempt to write `source/src/example.py` with native shell or
native file tools fails with permission denied.

### Operator cannot stage an unreviewed source change

Given an operator somehow creates a file in a writable control area, attempting
to apply or commit it into the protected source tree without a patch artifact
and review receipt is refused.

### Lane scratch is inaccessible to operator

Given a lane job is running in `scratch/jobs/<job-id>/`, the operator user
cannot modify files in that workspace. If this is not true, a real supervisor
can still be used as byline cover for operator-authored patches.

### Patch hash substitution fails

Given reviewers accepted patch hash `A`, applying patch hash `B` under the
same job id is refused even if `B` touches the same files.

### Base tree drift fails

Given reviewers accepted patch `A` against base tree `T1`, and the protected
tree is now `T2`, apply is refused unless the workflow has an explicit rebase
and re-review path.

### Write-scope escape fails

Given a lane modifies an allowed file and a forbidden file in scratch, patch
capture refuses the artifact or strips nothing and marks the job failed. Silent
partial capture is dangerous because reviewers may not see what the lane did.

### Supervisor death changes attestation

Given a lane supervisor dies before patch capture, the capture is refused for
jobs requiring attested lanes. If the workflow allows unattested operator
submissions, the patch is labelled as operator-submitted and cannot satisfy
attested-lane gates.

### Direct SQLite tamper is detected

If sealed mode uses signed receipts, editing the SQLite rows after apply makes
receipt verification fail. This does not stop a local root attacker, but it
detects post-hoc state rewrite by the operator user if receipts are outside
that user's write authority.

### Review evidence export is self-contained

An exported evidence bundle contains enough hashes and receipts to verify that
the final protected tree corresponds to reviewed patch artifacts.

## Residual risks

### Local root remains absolute

No local software system can defend against a party with full administrative
control over the machine and the ability to alter the trust anchor. Sealed
mode should state this plainly.

### Process attestation is not model attestation

Striatum can attest that a lane process ran and that its workspace produced a
patch. It cannot prove the cloud model generated the patch unless the harness
or provider participates in attestation, or Striatum records the full
interaction. This is acceptable if the product claim is process/workflow
provenance, not model-token provenance.

### Lane tools can still be broad

An attested lane may run shell commands, generators, formatters, or scripts.
Those are part of the lane's authority. Workflows that need tighter assurance
must constrain lane tools, network, and dependencies.

### Generated files complicate line-level explanations

For generated lockfiles, snapshots, compiled assets, or vendored files, the
right provenance object is the generator command and input hash, not every
individual line. The provenance model should support generated-output
annotations.

### Usability pressure will push users back to advisory mode

Sealed mode adds friction. Users like general-purpose coding CLIs precisely
because they can edit quickly. The product needs a clear UX: advisory mode for
fast local work, sealed mode for high-assurance runs, and obvious indicators
when guarantees differ.

## Recommended product position

Striatum should avoid claiming "true provenance" for plugin-driven operation
where the operator has normal repository write access. The honest product
position is:

- Current Striatum provides deterministic workflow coordination and durable
  provenance records for work submitted through it.
- RFC 0026-style changes can make author claims honest and prevent accidental
  lane forgery.
- True provenance requires sealed mode, where source writes are physically or
  cryptographically mediated by Striatum.

The most pragmatic path is to build toward sealed mode incrementally:

1. Finish byline honesty and compromised-run recovery.
2. Introduce patch artifacts and hash-bound reviews in ordinary workflows.
3. Add a Striatum apply gate and provenance receipts.
4. Add operator containment and protected source ownership as an opt-in mode.
5. Consider signed commits or external trust only after the local workflow is
   coherent.

For a fully autonomous deployment, phase 4 is not optional. The earlier phases
improve evidence quality, but they still rely on the operator choosing not to
use native write authority. The no-human target state requires removing that
authority.

## Direct answer to P005

The bypass problem is structurally addressed by removing source-write
authority from the operator. A frontier AI can remain the planner and
orchestrator, but it must not be a peer writer in the same repository it is
orchestrating.

The hard architecture is:

```text
untrusted operator
  can: plan, read through APIs, start workflows, inspect state
  cannot: write protected source or lane scratch

attested lanes
  can: write bounded scratch workspaces
  cannot: mutate protected source directly

review lanes
  can: review immutable patch artifacts
  cannot: accidentally review a moving target

Striatum apply service
  can: write protected source
  only if: exact patch hashes satisfy workflow gates

provenance receipt
  binds: final tree -> patch hashes -> jobs -> sessions -> supervisors -> reviews
```

Anything less is useful workflow discipline, but not absolute provenance. If
the cloud operator's native harness can still write the target repository
directly, the system has not bridged the gap between advisory plugin usage and
hard enforcement. It has only made the desired path easier to follow.
