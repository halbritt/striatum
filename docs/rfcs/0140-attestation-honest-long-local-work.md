# RFC 0140: Attestation for honest long local work — separate "alive but tool-call-silent" from "wedged/dead" so a lane running tests or scanning a repo keeps its publishable byline, without weakening the dead/hijacked-lane forgery guard

Status: implemented (D240, 2026-06-19 — #457, PR #486)
Date: 2026-06-19
author: proposer-claude-opus-4-8-001
Context:
- GitHub [#457](https://github.com/halbritt/striatum/issues/457) — the finding
  this RFC resolves (failure-mode audit FMA-009). A lane that issued ≥1 tool
  call and then does 10+ minutes of *silent local work* (a test suite, a
  browser-automation step, a large repo scan) is reclassified
  `wedged_no_tool_progress` → its attestation drops → `artifact.publish` refuses
  the role byline **mid-work**. Labeled `rfc-0091` (the lane-health / liveness
  classification family).
- Failure-mode audit, `STRIATUM_FAILURE_MODE_AUDIT_OPUS_4_8_2026-06-19.md`
  §3 (FMA-009, severity **MINOR** — loud, operator-recoverable DX friction on
  honest slow work, not data corruption). The audit's "smallest next step":
  *"treat a fresh heartbeat as sufficient to keep attestation during
  `working_local`, or widen the `wedged_no_tool_progress` deadline for
  confirmed-live PTY activity."* This RFC turns that into a concrete, testable
  design.
- [RFC 0091](0091-lane-health-module.md) / [D153](../decisions/decision-log.md)
  — the lane-health deep module. `go/pkg/lanehealth/lanehealth.go` `Classify`
  is the single seam that derives `Health{Bound, Alive, Attested, Deliverable}`.
  The attestation invariant is `Attested = Bound && Alive && start-token-verified`
  **and no stall class** — step 6 of `Classify` (`lanehealth.go:252-257`) sets
  `h.Reason = ReasonSupervisorStalled` and returns `Attested = false` for **any**
  non-empty stall class, including `wedged_no_tool_progress`, *even when step 5's
  active PID probe already set `h.Alive = true`* (`lanehealth.go:234-250`). That
  is the exact seam this RFC sharpens.
- [RFC 0026](0026-lane-attestation-and-operator-byline-honesty.md) /
  [D080](../decisions/decision-log.md) — the security purpose of attestation.
  Attestation is the **anti-fabrication guardrail** that protects the artifact
  byline: an unattested session publishes `author: operator`, and a lane/model
  byline (`author: <role>-<model>-<ordinal>`) is restored **only when the
  supervised process's pid identity and the launch-command snapshot match**, so a
  dead, hijacked, or closed-session lane cannot publish an artifact under a role
  identity it does not own. [D149](../decisions/decision-log.md) is explicit that
  attestation is **friction, not cryptographic non-repudiation** — which is
  exactly why "the PID is alive and matches" is a legitimate, in-scope basis to
  keep a byline, and why this RFC's fix does not require new crypto.
- [RFC 0131](0131-transport-aware-liveness-confidence.md) /
  [D211](../decisions/decision-log.md) — the `#324` `wedged_no_tool_progress`
  rung (`go/pkg/sessionliveness/liveness.go` `StallToolProgress`,
  `toolProgressWedged`) and the transport-aware `probe_basis`
  (`pty_confirmed_dead` vs `deadline_elapsed_only`). The rung's design intent is
  the **lost-its-MCP-endpoint** wedge — a lane whose daemon endpoint died keeps
  repainting a spinner, so `last_pty_activity_at` stays fresh forever while zero
  tool calls are issued. The rung deliberately consults **only** the tool-call
  timeline, never PTY frames, so spinner chatter cannot mask a dead endpoint.
  This RFC narrows when that *liveness* signal is allowed to also drop
  *attestation*.
- Grounded reads at `origin/main` (`5c904f60`):
  `go/pkg/sessionliveness/liveness.go` (`toolProgressWedged:793-805`,
  `ToolProgressSeconds: 600` default, the `#145` long-foreground-command reprieve
  at `:577-607` that already keeps a *no-tool-history* lease holder
  `working_local`), `go/pkg/lanehealth/lanehealth.go` (`Classify:168-261`, the
  active PID probe `:234-250`, the stall→unattested step `:252-257`, `LegacyMap`,
  `LiveTarget = Attested && Alive`), `go/pkg/mutations/mutations.go`
  (`sessionLaneAttestation:1688-1703`), `go/pkg/mutations/artifact.go`
  (`expectedAuthorLine:627-659` reading `attestation["attested"]` to choose the
  byline, `validateMarkdownAuthorLine:602-625` refusing a byline mismatch with
  `exit 6`), `go/pkg/mutations/claim.go` (`requireLiveLaneBackend` work-session
  gate `:332-368`, which admits an attention-pending-but-alive lane but refuses
  every other stall class).

> **Self-applied discipline.** The single load-bearing claim of this RFC — *"a
> PID-alive, tool-call-silent lane is reclassified `wedged_no_tool_progress`, and
> that reclassification drops attestation and refuses the publish byline, on the
> same path that would refuse a genuinely dead lane"* — was `ASSERTED`, then
> **`VERIFIED` against source.** `lanehealth.Classify` performs the active PID
> probe **first** (`:234-250`, setting `h.Alive` from `f.ProbeResult.Alive`) and
> only **then** runs the stall classification (`:252-257`); a non-empty stall
> class — `wedged_no_tool_progress` included — sets `ReasonSupervisorStalled` and
> returns `Attested = false` **regardless of `h.Alive`**. So the verified
> mechanism is: *the same `Attested = false` is produced for a confirmed-dead PID
> and for a PID-alive-but-tool-silent lane.* The fix is therefore not "trust the
> stall class less" globally — it is "for the one stall class that does **not**
> imply death (`wedged_no_tool_progress` on a PID-alive lane), let a live PID +
> fresh keepalive keep the byline." The forgery guard (dead/hijacked/closed
> session) is untouched because those paths fail the *PID probe* (steps 2–5), not
> the *stall classification* (step 6).

## Problem

### Tool-call silence is not liveness loss

Striatum's liveness classifier treats the **tool-call timeline** as the
forgery-resistant progress signal of last resort (RFC 0131 / `#324`): a lane that
has a recorded tool-call history but has issued no tool call for
`ToolProgressSeconds` (default **600s**) — and is not currently inside a tool
call — is classified `wedged_no_tool_progress`, on the theory that its MCP
endpoint died and only a spinner is keeping the PTY fresh. That theory is correct
for the incident it was built for (a claude lane that lost its daemon endpoint).

But the same condition is produced by **honest long local work**. An agent that
has done some tool calls and then runs a real task — a full test suite, a
browser-acceptance profile, a 13.5M-row repo scan, a long build — issues **zero
tool calls** for the whole duration, by design: it is not talking to the daemon,
it is *working*. After 10 minutes of that, the lane is reclassified
`wedged_no_tool_progress` exactly as a dead-endpoint lane would be. (The classifier
*does* exempt a lease holder with **no tool-call history** — the `#145`
long-foreground-command reprieve, `liveness.go:577-607` — but a single prior tool
call makes the history permanent, so any agent that used a tool *before* starting
its long local work trips the rung.)

### The reclassification drops attestation and refuses the byline mid-work

`wedged_no_tool_progress` is not just an operator-visible status. It flows into
the **attestation** derivation, which gates the artifact byline:

1. `lanehealth.Classify` (step 6, `lanehealth.go:252-257`) sees the non-empty
   stall class and returns `Health{Alive: true, Attested: false,
   Reason: supervisor_stalled}`. **`Alive` is true** (the active PID probe at
   step 5 confirmed the process) — only `Attested` was flipped off by the stall.
2. `sessionLaneAttestation` (`mutations.go:1688-1703`) returns
   `LegacyMap(health)` with `attested: false`.
3. `expectedAuthorLine` (`artifact.go:627-659`) reads `attested == false` and
   derives `author: operator` instead of `author: <role>-<model>-<ordinal>`.
4. `validateMarkdownAuthorLine` (`artifact.go:602-625`) then **refuses**
   `artifact.publish` because the agent's title-block byline (the role byline it
   correctly wrote) no longer matches the now-`operator` expected line — `exit 6`,
   mid-work, on honest output.

The same demotion also makes `work.*` itself refusable: the `requireLiveLaneBackend`
gate (`claim.go:332-368`) admits an alive-but-attention-pending lane but refuses
every *other* non-empty stall class — so a `wedged_no_tool_progress` lane can be
told it "cannot complete work without a live attested lane backend" while its PID
is demonstrably alive.

### Why the existing workaround is real but insufficient

The standing operator guidance (and Striatum's own memory) is: **heartbeat during
local work** — call `work.heartbeat` periodically so the lane stays fresh, and
`supervise.rebridge` to recover if it has already decayed. This works because a
`work.heartbeat` stamps `last_work_heartbeat_at`, which keeps the *lease*
heartbeat rung satisfied. But it does **not** address `wedged_no_tool_progress`:
that rung consults the **tool-call timeline only** (`toolProgressBase` =
`last_tool_call_started_at` / `last_tool_call_finished_at`), and a `work.heartbeat`
is **not** a tool call — so heartbeating keeps the lease alive yet still leaves the
lane tool-silent past 600s, still `wedged_no_tool_progress`, still unattested. The
workaround that *does* work today is the operator manually overriding or the agent
issuing a no-op tool call every few minutes to refresh the tool timeline — fragile,
client-cooperative, and undocumented as a contract.

### What must NOT change: the security invariant

Attestation exists to stop **byline forgery**. A genuinely **dead**, **hijacked**
(pid identity / start-token mismatch), or **closed-session** lane must remain
unattested, must keep publishing `author: operator`, and must remain reapable by
the dead-agent recovery path (its lease requeued, its slot transferred). Any fix
here must keep producing `Attested = false` for those cases — the change must be
**narrowly scoped to the one condition that does not imply death**: a lane whose
PID probe says **alive**, whose pid identity / start token **match**, and whose
session is **open**, but which is merely **tool-call-silent** because it is doing
real local work.

## Goals

- A lane doing ≥10 minutes of silent local work with a **live, identity-matched
  PID** keeps a **publishable role byline** and is not refused at `work.*` or
  `artifact.publish`.
- A genuinely dead, hijacked, or closed-session lane **still** loses attestation,
  still publishes `author: operator` (or is refused), and is **still** reaped and
  its lease requeued by the dead-agent recovery path — no regression to RFC 0026 /
  D080 / RFC 0131.
- The `wedged_no_tool_progress` *liveness* signal (RFC 0131 / `#324`) stays exactly
  as load-bearing for the lost-endpoint-spinner case it was built for; this RFC
  changes only whether/when that signal is allowed to also drop *attestation*.

## Non-Goals

- No cryptographic non-repudiation. D149 fixes attestation as friction, not a
  signature; "alive + identity-matched PID + a recent keepalive" is a legitimate
  in-scope attestation basis and we lean on it deliberately.
- No change to the lost-endpoint recovery outcome: a spinner-chattering,
  endpoint-dead lane must still be transferable in bounded time (RFC 0131's
  escape-valve cap is preserved).
- No new durable transcript capture, no hosted services, no change to what the
  byline *means*.

## The seam, precisely

The fix lives at the junction of two facts the classifier **already computes** but
does not currently combine:

- `lanehealth.Health.Alive` — set from the **active PID probe** (`lanehealth.go:236`,
  `f.ProbeResult.Alive`), an out-of-band oracle that is **true only if the
  supervised process is running with the matching pid identity**. This is the
  forgery-resistant fact attestation actually cares about.
- `sessionliveness` `wedged_no_tool_progress` — a **tool-timeline-only** signal
  that, by RFC 0131's own design, says nothing about whether the PID is alive
  (a spinner keeps the PTY fresh; the rung ignores the PTY precisely so it cannot
  be fooled). It is evidence of *no MCP progress*, **not** evidence of *death*.

Today step 6 lets the second fact override attestation unconditionally. The
correct invariant is: **a `wedged_no_tool_progress` stall may drop attestation
only when it is *not* contradicted by a live, identity-matched PID plus a recent
liveness keepalive.** When the PID probe is positive and a keepalive is fresh, the
lane is "alive but tool-call-silent," which is honest long local work — attest it.
When the PID probe is negative *or* no keepalive is fresh, it is "wedged/dead" —
the existing behavior holds.

## Proposal — options and trade-offs

Three options, increasing in where the change lands. They are not mutually
exclusive; the recommendation combines the cheapest two.

### Option A — client-side auto-heartbeat (keepalive that the tool timeline can see)

The agent loop emits a **periodic keepalive** during long local work so the lane
never crosses `ToolProgressSeconds`. Because `wedged_no_tool_progress` reads the
**tool-call timeline only**, the keepalive must stamp a signal that rung honors —
either a dedicated `work.heartbeat` variant that *also* advances the tool-progress
base, or a lightweight no-op MCP call the agent loop issues on a timer
(e.g. every `ToolProgressSeconds/3 ≈ 200s`) while a foreground command runs.

- **Pros.** Minimal; touches **no** gate, **no** classifier, **no** attestation
  derivation — the lane simply never enters the wedged state, so attestation,
  `work.*`, and the byline are all unaffected by construction. Aligned with the
  existing, understood "heartbeat during local work" operator model and with the
  audit's first suggestion. Lowest blast radius.
- **Cons.** **Relies on cooperative clients.** An agent loop that does not
  implement the keepalive (a third-party adapter, an older CLI) still decays. It
  also blurs the `#324` signal at the margin: a keepalive that is *too* generous
  could keep a genuinely lost-endpoint lane out of `wedged_no_tool_progress`
  longer than intended — so the keepalive must stamp a signal that is still
  **forgery-resistant** (tied to the supervised process actually executing), not a
  bare timer the helper can fire even after the agent died. The honest framing:
  Option A alone moves the problem to "did the client implement the keepalive,"
  which is acceptable for first-party lanes (claude/codex/agy) but not a complete
  guarantee.

#### Interaction with dead-agent recovery

A keepalive that the *helper* can fire regardless of agent liveness would defeat
recovery — so under Option A the keepalive must be **agent-issued** (it requires
the supervised process to run an MCP call / tool step), exactly like every other
tool-timeline stamp. A dead agent cannot issue it, so its tool timeline still ages
out, it still trips `wedged_no_tool_progress`, and recovery still reaps it. This
is the same property the lease heartbeat already relies on (D080: a dead process
cannot forge a heartbeat).

### Option B — server-side liveness-truthful classifier (decay attestation only on TRUE liveness loss)

Make the **classifier** distinguish "alive but tool-call-silent" from
"wedged/dead." Concretely, in `lanehealth.Classify` (or in the `sessionliveness`
verdict it consumes), a `wedged_no_tool_progress` stall on a lane whose **active
PID probe is positive** (`h.Alive == true`, identity-matched) becomes a new,
**attestation-preserving** protocol state — call it `alive_but_silent` — rather
than a stall that sets `ReasonSupervisorStalled`. Attestation decays only on a
stall that reflects **true liveness loss**: PID gone / pid-identity or start-token
mismatch / session closed / no recent process-level heartbeat. The
`wedged_no_tool_progress` *status* is still surfaced to the operator (it is real
information: "this lane has made no MCP progress for N minutes"), but it no longer,
on its own, flips `Attested` off for a PID-alive lane.

- **Pros.** Fixes the problem for **all** clients, not just cooperative ones —
  the truth of "the PID is alive and matches" is computed server-side and is not
  forgeable by the agent. Makes the classifier honest: it stops conflating "no
  tool progress" with "dead." Directly implements the audit's "widen the deadline
  for confirmed-live PTY activity" suggestion, but more precisely — keyed on the
  **PID probe**, not on raw PTY frames (so a spinner alone does **not** rescue
  it; only a positively-probed live process does).
- **Cons.** Touches the most security-sensitive seam (`lanehealth.Classify` step
  6). It must be written so the **PID probe remains authoritative**: the
  `alive_but_silent` exemption fires **only** when `f.ProbeResult.Alive` is true
  *and* identity matches — never on PTY freshness alone, or `#324`'s whole point
  (spinner chatter is not progress) is undone. It also has to interact correctly
  with the pipe transport (TransportPipe), which has **no** PID oracle: for a pipe
  lane `f.ProbeResult.Alive` is not positively established, so a pipe lane in
  `wedged_no_tool_progress` does **not** get the exemption — it keeps today's
  behavior (degrade-safe; RFC 0131 "default to the lower-confidence classification
  on ambiguity"). That asymmetry is correct (a pipe lane genuinely lacks the
  forgery-resistant alive signal) but must be explicit.

#### Interaction with dead-agent recovery

This is the option most entangled with recovery, and the entanglement is **safe**
by construction: the exemption keys on the **same active PID probe** the recovery
decision tree uses to confirm death (`supervisedAgentConfirmedDead` /
`pty_confirmed_dead`, RFC 0131). A lane the recovery tree would reap has a
**failed** PID probe — so it never qualifies for `alive_but_silent`, stays
unattested, and is requeued/transferred exactly as today. A lane that qualifies
for `alive_but_silent` has a **passing** PID probe — so the recovery tree would
**not** reap it either (it is, in fact, alive). The two paths read the same oracle
and therefore cannot disagree: a lane is either confirmed-alive (attest, do not
reap) or not-confirmed-alive (do not attest, eligible for reap). Option B must
keep the RFC 0131 escape-valve cap intact so a pipe lane (no oracle) is still
escalatable in bounded time.

### Option C — publish-time grace (a distinct, recorded attestation tier)

Leave the classifier and recovery alone; relax only the **publish/`work.*` gate**.
When `artifact.publish` (or `requireLiveLaneBackend`) is refused *solely* because
of `wedged_no_tool_progress`, re-probe: if the session is **active** and the PID is
**alive and identity-matched**, allow the publish under a **distinct attestation
tier** recorded *in the artifact's front matter / event* — e.g.
`attestation: alive_silent` (vs `attested` / `operator`) — so the provenance
record is honest that the byline was admitted on a live-PID-but-tool-silent basis
rather than a fully-fresh-protocol basis.

- **Pros.** Smallest behavioral surface on the classifier and recovery (they are
  untouched). The new tier makes the *provenance* explicit — a reader can see this
  artifact was published while the lane was tool-silent, which is strictly more
  honest than either today's refusal or a silent re-attest. Useful even alongside
  A/B as the **record** of why a silent lane was allowed to publish.
- **Cons.** Adds a third byline/attestation tier (schema + byline-validation +
  front-matter-schema surface), which is more concept than the problem strictly
  needs if B already makes the classifier truthful. If shipped **alone** it leaves
  the classifier still calling the lane "wedged" and the operator status still
  misleading, fixing only the publish symptom. The new tier must validate against
  the artifact V1 schemas (the publisher refuses invalid front matter with
  `exit 6`), so it is not free.

#### Interaction with dead-agent recovery

Option C re-probes the **same PID oracle** at publish time, so a dead lane's
publish attempt re-probes negative and is still refused — no forgery. But because
C leaves the *classifier* unchanged, a lane it admits at publish is still
`wedged_no_tool_progress` to the recovery sweep; recovery would still consider it
for transfer based on the tool timeline. That is a latent inconsistency (publish
says "fine," recovery says "wedged") — which is exactly why C should **not** ship
alone; it should ride on B so the classifier and the gate agree.

## Recommendation

**Ship A + B. A is the minimal, immediately-deployable safe keepalive for
first-party lanes; B makes the classifier liveness-truthful so the fix holds for
*all* clients and so the operator status stops lying.** C's distinct
`alive_silent` provenance tier is **recommended as the record layer on top of B**
(record *why* a tool-silent lane kept its byline) but is optional and can be a
fast-follow; it must **not** ship alone.

Rationale: A alone is fragile (cooperative clients) but trivially safe and useful
today. B alone fully fixes the problem and is the right structural change, but is
the more delicate edit. Together, A removes the common case before it ever reaches
the classifier (the lane simply keeps making tool-timeline progress), and B is the
backstop that keeps an *uncooperative* or *keepalive-missed* lane attested **iff
its PID is provably alive** — closing the gap A leaves while never weakening the
forgery guard, because B keys on the **same PID oracle recovery uses to confirm
death**. The combined invariant: *attestation decays only on true liveness loss
(PID gone / identity mismatch / session closed), never on tool-call silence
alone.*

## Acceptance Criteria

Each is a behavior testable with the existing `pgtest` + pure-classifier surfaces
(RFC 0091 keeps `Classify(Facts, now)` a pure, DB-free test surface).

1. **Honest long local work stays attested + publishable (the fix).** A lane with
   a recorded tool-call history, an **active + identity-matched PID** (active probe
   `Alive == true`), and **no tool call for ≥ `ToolProgressSeconds` (600s)** is
   classified `alive_but_silent` (not a `ReasonSupervisorStalled` attestation
   drop): `lanehealth.Classify` returns `Health{Alive: true, Attested: true}`,
   `sessionLaneAttestation` returns `attested: true`, `expectedAuthorLine` derives
   `author: <role>-<model>-<ordinal>` (not `operator`), and a matching-byline
   `artifact.publish` **succeeds** (no `exit 6`). (Option B.)

2. **A keepalive keeps the lane out of the wedged state (the cheap path).** A lane
   that issues the agent-side keepalive every `< ToolProgressSeconds` never reaches
   `wedged_no_tool_progress`: its `toolProgressBase` stays fresh, `Classify`
   reports `working_*`, attestation never drops, and no publish is ever refused for
   tool-call silence. (Option A.)

3. **A truly dead lane still loses attestation and is recovered (forgery guard
   holds).** A lane whose **active PID probe fails** (PID gone / pid-identity
   mismatch / start-token mismatch) is `Attested == false` regardless of any
   tool-timeline or PTY state, `expectedAuthorLine` derives `author: operator`,
   `artifact.publish` of a role byline is **refused**, the work-session gate
   refuses `work.*`, and the dead-agent recovery path reaps it and requeues its
   lease — **identical to today** (no regression to RFC 0026 / D080 / RFC 0131).

4. **A hijacked / closed-session lane is refused.** A session that is **not
   active** (closed) or whose pid identity / start token **does not match** is
   `Attested == false` and cannot publish a role byline — unchanged.

5. **A lost-endpoint spinner lane is still escalatable.** A lane that emits PTY
   frames (a spinner) but whose **PID probe is not positively alive** (or a pipe
   lane with no PID oracle) and has made no tool-call progress past the
   `ToolProgressSeconds` / RFC 0131 escape-valve cap is **not** rescued by the
   `alive_but_silent` exemption (PTY freshness alone never qualifies — only a
   positive PID probe does), stays `wedged_no_tool_progress`, and is escalated /
   transferred in bounded time. The `#324` spinner-cannot-forge-progress property
   is preserved.

6. **Pipe transport stays degrade-safe.** A `TransportPipe` lane (no PID oracle,
   `Alive` not positively established) in `wedged_no_tool_progress` does **not**
   get the `alive_but_silent` exemption and keeps today's behavior (RFC 0131
   "default to the lower-confidence classification on ambiguity").

7. **(If C ships) the provenance tier is honest.** An artifact published while the
   lane was `alive_but_silent` records the distinct attestation basis
   (e.g. `attestation: alive_silent`) in its event/front matter, validates against
   the artifact V1 schema, and is distinguishable in provenance from a
   fully-fresh-protocol `attested` publish.

## Open Questions

1. **Keepalive cadence and signal (Option A).** Should the keepalive be a new
   `work.heartbeat` variant that *also* advances the tool-progress base, or a
   first-class "local work in progress" MCP call? And what cadence — fixed
   (`~ToolProgressSeconds/3`) or adaptive? It must stamp a **forgery-resistant**
   (agent-executed) signal, never a helper-side timer.
2. **`alive_but_silent` as a stall class vs a protocol state (Option B).** The new
   classification should be a non-attestation-dropping **protocol state** (like
   `working_local`), not a persisted `liveness_stall_class` enum value — to avoid
   widening the migration-0012 CHECK constraint (the same care RFC 0131 / `#117`
   took). Confirm it lands as a projection-only protocol state.
3. **Does Option C's `alive_silent` tier warrant its own byline form, or just an
   event/front-matter annotation on the normal role byline?** Recommendation: keep
   the role byline unchanged and record the basis as an annotation, so the byline
   contract (RFC 0026) is untouched and only the *provenance record* gains detail.
4. **Should the work-session gate (`requireLiveLaneBackend`, `claim.go`) admit
   `alive_but_silent` the same way it already admits attention-pending-but-alive
   lanes (`claim.go:341`)?** Almost certainly yes (it is the same "alive lane,
   non-death stall" shape) — confirm the gate change rides with B.

## Domain Modeling

This is a **boundary clarification** on the existing lane-health aggregate (RFC
0091), not a new aggregate. It sharpens one invariant: *attestation* (the
anti-fabrication byline guard) decays on **true liveness loss** — a confirmed-dead
or identity-mismatched PID, or a closed session — and **not** on the *liveness
signal* `wedged_no_tool_progress` alone, which is a statement about *MCP progress*,
not *process death*. The new value distinction is the protocol state
`alive_but_silent`: "the supervised process is provably alive and identity-matched,
but has made no tool-call progress" — a first-class, attestation-preserving state
between `working_local` and a death-implying stall. Cites
[`docs/DDD.md § "Adding to the model"`](../explanation/domain-driven-design.md#adding-to-the-model);
RFC 0091 (the lane-health module) and RFC 0131 (transport-aware liveness
confidence) are the precedents this refines.
