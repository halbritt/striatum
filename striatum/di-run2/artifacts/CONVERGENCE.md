---
schema_version: striatum.findings_ledger.v1
artifact_kind: findings_ledger
summary_count: 24
author: convergence-critic-gpt-5.5-001
---

# Convergence Ledger

author: convergence-critic-gpt-5.5-001

## Result

Rank the graduation path around deterministic structural evidence, not model
content:

1. **Branch 3 idea 6, Last-mile delivery receipts** - convergence and final
   synthesis must consume daemon-recorded artifact receipts from all upstream
   jobs, not ambient repository files. This is the strongest fit because it
   proves fan-in through the declared run graph while ignoring branch prose.
2. **Branch 3 idea 2, Exception-lane cross-dock** - inject lane death,
   transport churn, and reviewer replacement as deterministic exceptions, then
   require each exception to reach replacement or loud escalation before the
   lease budget closes.

Near-neighbor support: Branch 3 idea 4 (deterministic warehouse scan) is the
acceptance scanner for pick 1, and Branch 3 idea 5 plus Branch 1 ideas 3-4 are
the returns/loudness mechanics for pick 2.

## Scoring Method

Weighted score = novelty 0.35 + viability 0.40 + fit 0.25. Viability is scored
against the fixed constraints: no new daemon method, no model call in state
transition, deterministic unattended CI, structural assertions only, fault
matrix coverage, and no hosted/external persistence.

## Scored Ideas

| Rank | Idea | Cluster | Novelty | Viability | Fit | Weighted | Trap |
| ---: | --- | --- | ---: | ---: | ---: | ---: | --- |
| 1 | B3.6 Last-mile delivery receipts: downstream jobs consume daemon artifact receipts from every upstream stop | receipt-backed fan-in | 9 | 9 | 10 | 9.25 | no |
| 2 | B3.2 Exception-lane cross-dock: route injected lane death, churn, and reviewer replacement to replacement or escalation before lease close | bounded fault routing | 9 | 9 | 10 | 9.25 | no |
| 3 | B3.4 Deterministic warehouse scan: scan graph, artifacts, kinds, author lines, leases, terminal states, and timestamps | structural scanner | 7 | 10 | 10 | 8.95 | no |
| 4 | B3.1 Proof-of-delivery manifests: structural bill of lading per job matched to fan-out/fan-in graph | receipt-backed fan-in | 8 | 9 | 10 | 8.90 | no |
| 5 | B4.3 Adversarial deterministic lane stub: emit worst-legal structural outputs such as empty, duplicate, or oversized artifacts | adversarial structural stubs | 9 | 8 | 9 | 8.60 | no |
| 6 | B1.6 Marble run terminal outcome: under lane death, land in FINISHED or STUCK, never silent ledge | terminal outcome invariant | 7 | 9 | 10 | 8.55 | no |
| 7 | B1.2 Seeded fault script: preselect the exact fault schedule for replayable lane death/churn/replacement | deterministic fault schedule | 7 | 9 | 9 | 8.30 | no |
| 8 | B3.3 Cold-chain freshness seals: fresh-session branches prove no warm session or branch artifact reuse | fresh-session isolation | 7 | 9 | 9 | 8.30 | no |
| 9 | B3.5 Returns depot for stalled packets: reissue missed packets or emit one named escalation with original reason | bounded fault routing | 8 | 8 | 9 | 8.25 | no |
| 10 | B1.3 Mandatory stuck signal: every non-progressing part must produce a loud stuck signal | loudness invariant | 6 | 9 | 10 | 8.20 | no |
| 11 | B1.4 Per-helper timers: every helper must be done or honking before its timer expires | budgeted terminal invariant | 6 | 9 | 10 | 8.20 | no |
| 12 | B1.1 Deterministic chooser rule: replace nondeterministic critic behavior with a fixed rule for fixture runs | deterministic stub | 7 | 9 | 8 | 8.05 | no |
| 13 | B2.5 Supervisor loudness budget: heartbeat/lease overrun must create a failure-to-report escalation | loudness invariant | 7 | 8 | 9 | 7.95 | no |
| 14 | B2.1 Lease ownership attestation: name lease UUID, process ID, and supervisor ID on transitions/failure | chain of custody | 7 | 8 | 8 | 7.65 | no |
| 15 | B2.4 Schema handoff guard: name writer vs critic at the validation liability transfer point | validation boundary | 8 | 7 | 8 | 7.65 | no |
| 16 | B1.5 Telephone transport test: churn delivers once or asks retry, never vanishes or wedges | transport churn invariant | 6 | 8 | 9 | 7.55 | no |
| 17 | B4.5 Reviewer disagreement replacement: replacement reviewer returns a different structural verdict and run still terminates | reviewer replacement semantics | 8 | 7 | 8 | 7.45 | no |
| 18 | B4.2 Budget spread/price discovery: tighten budget until controlled escalation proves the bound is real | budget pricing | 9 | 6 | 8 | 7.35 | yes, as stated |
| 19 | B2.3 Reviewer chain of custody: maintain sequential reviewer IDs and timestamps | chain of custody | 6 | 7 | 8 | 6.90 | no |
| 20 | B4.1 Escalation premium: count escalations per completed run as an explicit charge | hidden-cost accounting | 8 | 6 | 7 | 6.85 | watch |
| 21 | B4.6 CI margin/flakes: state flake tolerance as an explicit spread and fail when variance crosses it | hidden-cost accounting | 8 | 6 | 7 | 6.85 | yes, if tolerant |
| 22 | B4.4 Cluster-count invariant: check cluster count/balance so critic collapse is shape-valid | semantic collapse guard | 8 | 5 | 5 | 6.05 | yes |
| 23 | B2.6 Transport proxy digest: proxy MCP traffic and pin losses to client or daemon socket handler | transport instrumentation | 6 | 5 | 6 | 5.60 | yes |
| 24 | B2.2 Cryptographic mock signing: mock agents sign metadata packets for every artifact | mock accountability | 6 | 5 | 5 | 5.35 | yes |

## Clusters

### Receipt-backed fan-in and structural scanning

Core ideas: B3.6, B3.1, B3.4, B1.6, B2.4, B4.3.

The best cluster says the fixture should treat model prose as opaque cargo and
prove the workflow by daemon receipts, graph shape, artifact kind/path/byline,
lease/heartbeat behavior, and terminal states. This is the cleanest
determinism boundary and the best graduation fit.

### Bounded fault routing and loud terminal states

Core ideas: B3.2, B3.5, B1.3, B1.4, B2.5.

This cluster supplies the "completes-or-escalates-loud" half of the gate. The
fixture should script each matrix fault and assert that every affected packet
is either reissued to a valid session or produces a named escalation within the
lease/budget window.

### Deterministic fault schedule and transport churn

Core ideas: B1.2, B1.5, B2.6.

Replayable fault schedules are useful. The transport-proxy variant is weaker:
the fixture needs to exercise churn, but should avoid making a custom proxy the
thing being certified.

### Accountability and chain of custody

Core ideas: B2.1, B2.3, B3.1, B3.6.

These ideas make failure debuggable by preserving leaseholders, reviewers,
receipt chains, and artifact provenance. They are valuable acceptance details,
but not sufficient alone because accountability without recovery can still
leave a loud but unsupported shape.

### Freshness and branch isolation

Core ideas: B3.3, B1.1.

Fresh-session isolation deserves an explicit structural assertion: each diverge
branch should prove it claimed a fresh session and did not read sibling branch
artifacts. This protects the workflow's intended parallel diversity without
asserting on generated content.

### Hidden subsidy and CI cost accounting

Core ideas: B4.1, B4.2, B4.3, B4.5, B4.6.

This branch correctly warns that deterministic stubs and generous budgets can
make the shape look cheaper than it is. The actionable part is adversarial
legal stub output and structural reviewer-disagreement tests. The risky part is
normalizing occasional CI trips or treating flake tolerance as reliability.

## Cross-model Signals

- **Opaque structural proof recurs across model families.** Claude branch 1
  says to watch terminal cups rather than squiggles; Gemini branch 2 proposes a
  schema handoff guard; GPT branch 3 proposes warehouse scans and delivery
  receipts; Claude branch 4 proposes adversarial structural stubs. This
  convergence is strong.
- **Bounded loudness recurs across model families.** Claude branch 1 has the
  red button and timer ideas; Gemini branch 2 has supervisor loudness budget;
  GPT branch 3 has exception cross-dock and returns depot. This is the other
  core fixture invariant.
- **Replayable fault injection recurs across model families.** Claude branch 1
  proposes pre-rolled fault dice, GPT branch 3 proposes exception routing for
  all matrix faults, and Gemini branch 2 independently names lane death,
  transport churn, and reviewer replacement accountability.
- **Receipt/custody evidence recurs across Gemini and GPT.** Gemini branch 2
  names lease/reviewer chain of custody; GPT branch 3 names proof-of-delivery
  manifests and last-mile artifact receipts. Use both: receipts for pass/fail,
  custody for failure diagnosis.
- **Reviewer replacement must be more than reattachment.** Gemini branch 2
  tracks reviewer custody; Claude branch 4 asks for structural disagreement;
  GPT branch 3 routes replacement as an exception. The combined signal is to
  assert bounded terminal behavior after reviewer churn, including disagreement.

## Trap List

- **B2.2 cryptographic mock signing:** signatures identify mocks but do not
  prove workflow reliability; they add crypto ceremony to a structural fixture.
- **B2.6 transport proxy digest:** an interceptor proxy risks certifying proxy
  behavior and adding a test-only transport dependency outside the product
  surface.
- **B4.2 budget price discovery, as stated:** "occasionally trips" is a CI
  flake if not tied to deterministic fault cases. Budget tests should trip only
  when the scripted case says they should.
- **B4.4 cluster-count invariant:** cluster balance is semantic quality
  pressure disguised as structure; it violates the graduation gate's content
  boundary.
- **B4.6 flake tolerance:** a tolerance threshold can warehouse regressions.
  Prefer deterministic structural variance cases that fail loud immediately.
- **B4.1 escalation premium, if used as the gate:** escalation frequency is a
  useful operating metric, but the graduation fixture must first prove
  complete-or-loud behavior. Do not penalize the shape for correctly escalating
  controlled fault cases.

## Top Picks for Deepening

### 1. Last-mile delivery receipts

Make daemon artifact receipts the only fan-in input accepted by convergence and
final synthesis in the fixture. CI checks that each upstream workflow node has
one receipt with the expected logical name, kind, path, author line, placement,
attempt, and terminal transition, then checks that downstream jobs consumed the
declared receipt set. The test never reads generated idea text.

Load-bearing reason: this directly proves the fan-out/fan-in shape and prevents
the fixture from passing because files happened to be present in a worktree.

### 2. Exception-lane cross-dock

Script the fault matrix as deterministic exceptions: kill a lane, interrupt
transport, and replace a reviewer. For each exception, assert the packet moves
to exactly one allowed terminal path before the budget closes: replacement lane
completes with valid receipts, or the run emits a named escalation with the
original failed lease/session context.

Load-bearing reason: this covers the RFC 0105/0106 gate's hard part without
requiring content-exact assertions or a new daemon method.
