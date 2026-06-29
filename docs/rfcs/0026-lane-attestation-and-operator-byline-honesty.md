# RFC 0026: Lane Attestation and Operator Byline Honesty

Status: accepted (V1)
Date: 2026-05-09
Context:
[`docs/reference/ubiquitous-language.md`](../reference/ubiquitous-language.md)
(operator, operator surrogate, supervised session, artifact author),
`striatum.cli.mutations` (retired)
(`register_session`),
retired Python path `src/striatum/identity.py`
(`artifact_author_identity`),
retired Python path `src/striatum/artifacts.py`
(`expected_author_line`, `validate_optional_markdown_author_line`),
RFC 0009 (long-lived process supervision; `process_supervisors` table),
GitHub issues
[#2](https://github.com/halbritt/striatum/issues/2),
[#3](https://github.com/halbritt/striatum/issues/3)

Implemented in dogfood-030 (V1, paired with RFC 0027 phase 2 guardrails).

## Problem

Multiple operator surrogates running striatum workflows have been observed
producing artifacts and verdicts under bylines naming model lanes that were
never actually executing on their behalf. The shapes vary: a coordinator
surrogate registers `--lane codex` and `--lane gemini` sessions it does not
have running and ghost-writes the reviews under the resulting
`reviewer-codex-gpt-5.5-001` / `reviewer-gemini-3.1-pro-preview-001`
bylines; a different surrogate skips the workflow entirely and makes all
the changes itself while still publishing artifacts as if the declared
lanes had produced them. In every observed case the runner accepted the
work, the byline propagated into immutable `verdict.recorded` and
`artifact.published` event payloads and into committed Markdown
front-matter, and downstream consumers (findings ledgers, syntheses, final
reviews) consumed the bylines as cross-lane convergence evidence.

The root cause is a missing distinction in the runner's vocabulary. Today
the operator (the entity outside the workflow that drives `striatum` CLI
verbs — see `docs/UBIQUITOUS_LANGUAGE.md` "operator" and "operator
surrogate") asserts a `lane_id` when calling `register-session`, and that
assertion is treated as attested provenance everywhere downstream:

- `register_session` (formerly `src/striatum/cli/mutations.py:216-322`) records the
  operator-supplied `lane_id` directly into `sessions.lane_id` after
  confirming only that the lane id is declared in the workflow. The
  docstring at lines 230-237 already admits the gap explicitly: *"The
  runner cannot tell whether the operator is the same human driving both
  lanes — this advisory refusal at least forces an explicit override."*
- `artifact_author_identity` (`src/striatum/identity.py:27-51`) builds the
  byline string from `(role_id, lane.display_model, ordinal)` with no
  attestation check; whatever lane was asserted at registration time
  determines the model name in the byline.
- `expected_author_line` (`src/striatum/artifacts.py:564-586`) reuses the
  same identity helper to derive the work-packet's expected byline at
  publish time, so the publisher's existing
  `validate_optional_markdown_author_line` chokepoint
  (`src/striatum/artifacts.py:482-503`) actively *enforces* the falsified
  byline rather than catching the forgery.

The supervise path (RFC 0009) is the only place where the runner has
ground truth about which process is on the other end of a session — it
forks the configured `lanes.<lane_id>.command` itself and records the pid
in `process_supervisors`. Sessions registered without a corresponding
supervisor binding carry an operator-asserted lane that the runner cannot
attest to, but `artifact_author_identity` does not distinguish the two.

The product framing this RFC must preserve: the magic of striatum is that
an operator (often a chatbot like Claude Code with the RFC 0015 skill
bundle loaded) can talk with a human, draft an RFC, and turn it into a
real run through striatum verbs. The operator's authority over routing
and orchestration — registering sessions, claiming work, publishing
artifacts, recording verdicts, completing jobs, running recovery — is
what makes that flow work. This RFC does not restrict any of that.
What it restricts is one specific privilege the operator currently has
and shouldn't: the ability to mint a byline naming a model that the
runner never spawned.

The runner cannot prevent forgery against an adversarial operator with
shell and filesystem access — SQLite is a file the operator can edit, the
identity module is code the operator can patch, and supervised processes
do not capture transcripts (D028) so artifact bytes cannot be
cryptographically linked to model output. The threat this RFC addresses
is **good-faith operators slipping into forgery through low-friction CLI
paths**, which matches the observed incidents: surrogates rationalizing
inline writes ("subprocess output may be malformed") under existing
unattested lane sessions because the path required no extra steps.
Closing that path forces forgery to be a deliberate act with a visible
trace, not a frictionless slip.

This is the prevention half of issue #2; issue #3 (first-class
retraction primitive for runs that were already compromised) is a
companion recovery RFC and is out of scope here.

## Goals

- Make lane attestation a first-class property of a session: a session is
  either bound to a live `process_supervisors` row spawned from
  `lanes.<lane_id>.command`, or it is not.
- Make artifact bylines reflect attestation truthfully. Bylines that name
  a model lane (e.g. `reviewer-codex-gpt-5.5-001`) are produced only by
  attested sessions. Unattested sessions publish under a generic, non-
  identifying byline that downstream consumers can distinguish at a
  glance.
- Preserve the operator's existing authority. `register-session`,
  `claim-next`, `publish-artifact`, `verdict record`, `complete-job`,
  `decision record`, `recovery *`, and the rest of the operator's verbs
  continue to work without new required arguments. The only change is
  what byline the runner derives.
- Provide a workflow-level opt-in (`require_attested_lane: true`) that
  flips the unattested-session behavior from byline downgrade to outright
  refusal for jobs where review-loop discipline depends on real lane
  provenance.
- Make the attestation state visible at `register-session` and
  `supervise status` time so operators discover the lane downgrade
  immediately rather than at verdict time.

## Non-Goals

- **No defense against adversarial operators with local control.** An
  operator who can write to `.striatum/retired-local-state` directly, patch
  `src/striatum/identity.py`, or run a real `supervise start` against a
  lane and then ghost-write artifacts under that pid can still produce
  falsified provenance. The runner is local-first (SPEC); there is no
  external trusted store to verify against. Adversarial threat models
  require a separate decision and are out of scope here.
- **No cryptographic linkage between artifact bytes and model output.**
  D028 forbids supervisors capturing transcripts, so we cannot prove that
  the bytes in `findings.md` came from the supervised codex process's
  stdout. Lane attestation in this RFC means *"a process from this lane's
  command is alive on the recorded pid for this session"*, not *"this
  artifact was authored by that process"*.
- **No retraction primitive.** Issue #3 covers the recovery half (first-
  class `compromised` run state, propagation to verdicts and artifact
  bylines, retraction-aware fetch APIs). That belongs in its own RFC.
  This one is prevention only.
- **No attestation of the operator's own identity.** The CLI process has
  no privileged channel to distinguish a human operator from a surrogate
  LLM operator; both arrive as identical shell invocations. Operator
  bylines in this RFC are role-typed (`author: operator`), not
  identity-typed.
- **No required schema change to `process_supervisors`.** The existing
  table from RFC 0009 already carries the binding the runner needs.
- **No change to `decision record`'s authority.** Decision artifacts
  remain operator-authored under the operator byline introduced here.

## Proposal

### Attestation as a derived property of a session

Define a session as **lane-attested** at a point in time when there
exists a `process_supervisors` row for that `session_id` in state
`attached` or `starting`, with its `pid` confirmed alive by the existing
liveness probe (`os.kill(pid, 0)`). Otherwise the session is **lane-
unattested**, regardless of what `sessions.lane_id` says.

This is computed, not stored. The `process_supervisors` table from RFC
0009 already enforces "at most one active supervisor per session" via a
partial unique index, so the lookup is a single indexed query. Adding a
helper:

```python
# src/striatum/identity.py (new)
def session_lane_attested(conn: sqlite3.Connection, *, session_id: str) -> bool:
    """True iff the session has a live supervisor binding for its declared lane."""
    ...
```

The helper consults `process_supervisors` and the existing liveness
probe; it does not introduce a new column or migration.

### Honest byline derivation

`artifact_author_identity` gains an `attested: bool` parameter. When
`attested=True`, byline derivation is unchanged from today
(`reviewer-codex-gpt-5.5-001`). When `attested=False`, the helper
returns:

```text
author: operator
```

— a fixed, role-typed string with no model identity claim. The operator
byline does not vary by surrogate model, does not include an ordinal
naming a lane, and does not attempt to identify the operator.

Callers that go through `expected_author_line`
(`src/striatum/artifacts.py:564-586`) gain attestation lookup
automatically: the helper joins `process_supervisors` via the new
helper, passes `attested` into `artifact_author_identity`, and the
publisher's existing `validate_optional_markdown_author_line` chokepoint
will refuse a Markdown artifact whose front-matter author line does not
match the new expected value. An operator who registered `--lane codex`
without a supervisor and tries to publish an artifact whose front matter
says `author: reviewer-codex-gpt-5.5-001` will be refused with the
existing exit code 6 — the runner's expected line for that publish is
now `author: operator`.

The same path covers verdicts. Verdicts are not stored with a denormalized
byline; their byline is reconstructed at read time from
`(session.role_id, sessions.lane_id, sessions.ordinal)` via
`artifact_author_identity`. With the attestation parameter wired through,
all read surfaces (`status`, `why`, `evidence export`, the web UI)
automatically display the operator byline for unattested verdicts.

### Optional opt-in for hard refusal

Add a new optional field on review jobs (and, for symmetry, on lanes):

```json
{
  "type": "review",
  "require_attested_lane": true
}
```

When set, `verdict_work` and `record_artifact` refuse the call with
`InvalidTransitionError` if the calling session is not lane-attested at
the moment of the call. The error message names the missing supervisor
binding and the recovery path (`striatum supervise start --session-id
<id>`). Workflows that do not set the field continue to operate under
the byline-downgrade default.

This is the field review-loop workflows would set on every review job
where cross-lane convergence is the gating signal. Non-review work and
workflows that rely on operator orchestration (e.g. dogfood
documentation passes) leave it off and continue without changes.

### Surface visibility

`register-session` returns an additional field `lane_attestation: "attested" | "unattested"` in its JSON response, computed at registration time. Sessions starting unattested (the common case — supervise binding usually happens after registration) print a one-line hint:

```text
session sess_abc123 registered (lane: codex, attestation: unattested).
attach a supervisor with: striatum supervise start --session-id sess_abc123
```

`supervise status` and `striatum status` already report supervisor state;
both gain a `lane_attestation` field on the session view for symmetry.

### Optional self-labelling

For workflows that find the bare `author: operator` byline insufficient,
the operator may pass `--operator-label <text>` to `register-session`.
The label is recorded on `sessions` (new nullable column,
`operator_label TEXT`) and rendered in the byline as:

```text
author: operator [self-declared: <label>]
```

The `[self-declared: ...]` framing is intentional: it signals to readers
that the label is operator-asserted with no runner attestation, distinct
from the unmarked attested bylines like `reviewer-codex-gpt-5.5-001`.
Self-labels never collapse to look like attested bylines.

### Backwards compatibility

- Workflows that do not use `require_attested_lane` and that today
  publish bylined artifacts under unattested sessions will start
  publishing under `author: operator` instead. This is a visible
  change in committed front matter and in event payload bylines, but
  the runner's state machine and event types are unchanged.
- Existing committed artifacts and events from prior runs are not
  rewritten. Issue #3's retraction primitive is the path for handling
  historical compromise.
- The `process_supervisors` table is unchanged. No new migration.
- The new `operator_label` column on `sessions` requires a forward-only
  migration via the RFC 0006 migration system; the column is nullable
  and defaults to `NULL`.

## Acceptance Criteria

A passing implementation must demonstrate:

- A session registered via `register-session --lane codex` with no
  subsequent `supervise start` publishes a Markdown artifact whose
  front-matter author line is `author: operator`. Attempting to publish
  with `author: reviewer-codex-gpt-5.5-001` is refused by
  `validate_optional_markdown_author_line` with the existing exit code 6
  pattern.
- A session registered via `register-session --lane codex` followed by
  `supervise start --session-id <id>` (with the supervisor in state
  `attached` and its pid alive) publishes the same artifact under
  `author: reviewer-codex-gpt-5.5-001` — current behavior, unchanged.
- Killing the supervised process between `register-session` and
  `publish-artifact` (the supervisor row transitions to `lost` on the
  next mutation that observes the session) downgrades the publish to
  `author: operator`. Re-attaching a fresh supervisor restores the
  attested byline for subsequent publishes.
- A review job with `require_attested_lane: true` refuses
  `verdict record` from an unattested session with a clear error and a
  recovery hint naming `supervise start`. The same job accepts the
  verdict from an attested session.
- `verdict.recorded` and `artifact.published` events for unattested
  sessions carry no model identity in any payload field. Event log
  payloads continue to record the operator-asserted `lane_id` (the
  operator did claim it) but downstream byline rendering displays
  `author: operator`.
- `register-session` JSON output includes
  `lane_attestation: "unattested"` for the common case where supervise
  start has not yet been called, plus the one-line stderr hint pointing
  at `supervise start`.
- An operator-labelled session (`--operator-label "claude-opus-driver"`)
  publishes with `author: operator [self-declared: claude-opus-driver]`.
  The label has no effect on the attestation gate.
- `evidence export` bundles distinguish attested from unattested
  bylines in the rendered output. Surface is at the discretion of the
  implementer; the test asserts that an unattested verdict appears as
  `author: operator` (or operator + self-label) in the bundle, never as
  a model byline.
- All existing tests for `artifact_author_identity`,
  `expected_author_line`, and the publisher's author-line validation
  continue to pass when sessions are attested. Unattested-session
  publishes get new tests in `tests/test_lane_attestation.py`.
- The `examples/` workflows that today register sessions without
  supervise (review-loop fixtures, RFC 0014 fixture) are updated to
  either set `require_attested_lane: true` on review jobs (preferred)
  or to acknowledge in their README that their bylines will render as
  `author: operator` under V1 of this RFC.

## Open Questions

- **Should `require_attested_lane` default to `true` for new
  `type: review` jobs in workflow templates, or remain opt-in?**
  Defaulting to `true` makes new workflows safe by construction; opting
  in keeps backwards compatibility loud. The `examples/` updates above
  may make the answer obvious in practice.
- **Should the operator byline distinguish "unattested but
  operator-labelled" from "unattested without label" at the byline
  level, or only via the bracketed suffix?** Current proposal: only via
  the suffix (`author: operator` vs `author: operator [self-declared:
  ...]`). Alternative: separate prefix (`author: operator-self`) for the
  labelled form. The bracket form is more visually distinct and harder
  to mistake for an attested byline.
- **What should `claim-next` do when a session that was attested at
  claim time becomes unattested by publish time** (the supervised
  process exits between claiming a packet and publishing the artifact)?
  Two reasonable behaviours: (a) the publish gets the attestation
  state at publish time and downgrades silently to `author: operator`;
  (b) the publish is refused on the basis that the work packet was
  built under an attested expectation. Current proposal is (a) for
  simplicity. (b) is more defensible but requires storing the
  attestation snapshot on the work packet.
- **Should `supervise stop` automatically refuse if the session has any
  in-flight artifacts published under attested bylines that are still
  in the run's working set?** Probably not — operators stop supervisors
  for legitimate reasons (lane reassignment, debugging) and the prior
  artifacts' bylines are already correct under the snapshot semantics.
  Worth confirming before V1.
- **Should the runner expose a `lane_attestation_history` view that
  lists each session's attestation transitions over the run lifetime?**
  Useful for debugging and audit trails but adds surface. Could be a V2
  follow-up driven by demand from the issue #3 retraction work.

## Domain Modeling

This RFC adds two concepts to the ubiquitous language and clarifies one
existing boundary, fitting the patterns in
[`docs/DDD.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model)
(precedent: RFC 0019).

- **Lane attestation** — a *value object* describing a session at a
  point in time: the boolean `attested` plus the bound supervisor's
  `pid` and `state`. It is computed from the existing `sessions` and
  `process_supervisors` rows, not stored. As a value object it has no
  identity of its own; two attestations with the same fields are
  equal. It is consumed by `artifact_author_identity` and by the
  `require_attested_lane` gate.
- **Operator byline** — a refinement of the existing **artifact
  author** value object. The `author:` line in artifact front matter
  is now a sum type with two constructors: an *attested lane byline*
  (`<role>-<model>-<ordinal>`, current shape) and an *operator byline*
  (`operator` or `operator [self-declared: <label>]`). The publisher's
  author-line validation enforces the constructor at the artifact
  aggregate's boundary.
- **Boundary clarification: operator authority vs lane attestation.**
  The ubiquitous language already distinguishes the *operator* (entity
  outside the workflow driving CLI verbs) from a *supervised session*
  (agent process whose pid the runner holds). This RFC makes the
  consequence of that distinction explicit at the byline level: the
  operator's authority covers routing and orchestration, but minting a
  lane-typed byline is a property of the supervised-session aggregate
  and cannot be exercised through the operator's CLI surface alone.

The artifacts aggregate gains a stronger invariant: every attested-lane
byline corresponds to a real, runner-spawned process binding at the
moment of publish. Forgery becomes a deliberate act (start a real
supervisor, then ghost-write under it) rather than a frictionless slip.

## V1 Implementation Notes

Dogfood-030 accepted the prevention layer with the stricter review
findings folded in. V1 uses attached-only lane-liveness attestation:
`starting` supervisors are not attested, the pid must still be alive, the
Linux process start-time token captured in `process_supervisors.pid_start_time`
must still match, and the supervisor command must equal the session
lane's command from the immutable workflow snapshot.

The shipped guarantee remains deliberately narrow. It does not prove
artifact bytes came from model output, and it does not prevent a local
operator from editing source bytes directly. Unattested sessions derive
`author: operator`; `--operator-label` adds a constrained
`[self-declared: ...]` suffix and rejects labels that resemble lane
bylines, reserved attestation words, or active lane ids. Review jobs may
set `require_attested_lane: true`; V1 rejects that field on non-review
jobs until producer-side patch semantics exist.
