# RFC 0111: In-band failure legibility — carry error code, message, and remediation through the MCP boundary so agents self-heal

Status: accepted (D165)
Date: 2026-06-03
author: proposer-claude-opus-4-8-001
Context: `STRIATUM_VS_CC_FLEET_CLAUDE_OPUS_4_8_2026-06-03.md` §4.3/§6 (senior peer comparison vs `github.com/ethanhq/cc-fleet`); the recurring opaque-MCP-error friction (state-changers surface a contentless `<error>method</error>`, forcing a CLI re-run to read the real cause); RFC 0030 (daemon RPC envelope), RFC 0020/0029 (autonomous recovery + operator recovery vocabulary), RFC 0103 (the `interrogation_unavailable` non-wedging signal — legibility done right).

## Problem

striatum **already has** a typed error envelope and a real stable-code vocabulary — the gap is that the legible signal does not reach the channel an agent actually reads, carries no remediation, and is not a closed contract.

What exists today:

- A typed RPC error: `rpc.Error{Code, Message, Details, ExitCode}` (`go/pkg/rpc/envelope.go:15`), and `ErrorResponse` already serializes `{code, message, details}` into the RPC `Response.Data` (`envelope.go:123-139`).
- A substantial **stable code vocabulary** in live use across `pkg/mutations` / `pkg/rpc`: `invalid_transition`, `fresh_session_required`, `interrogation_unavailable`, `branch_confirmation_required`, `worktree_required`, `already_completed`, `not_found`, `capability_missing`, `token_invalid`, `method_unknown`, … — dozens of them.
- The MCP boundary even extracts them: `ToolsCall` pulls `code`/`message` out of `response.Data` on `!ok` (`go/pkg/mcp/tools.go:66-76`).

The defect is one line in the rendering:

- `toolResult` (`go/pkg/mcp/tools.go:86-110`) builds the MCP result so the structured signal goes to `structuredContent` + `isError`, but the human-readable **`content` text block** is set to `fmt.Sprint(name)` — the bare **method name** — on success *and* failure (`tools.go:106`). The `content` text is the channel an LLM agent reads as the tool's result. So on a failed `work.complete` an agent sees `<error>work.complete</error>` and must re-run the equivalent CLI verb to learn *why* — the exact friction recorded in the comparison's §4.3.

Two further gaps separate this from a contract an agent can self-heal against:

1. **No remediation on the envelope.** `rpc.Error` has `Code`/`Message`/`Details` but no first-class `suggestion`/`recovery` field. striatum *has* a recovery vocabulary (RFC 0020 `recovery auto`, RFC 0029 `recovery_commands` on the process-adapter diagnostic envelope), but it is not attached to the error an agent receives in-band — so the agent cannot turn a failure into the next action without out-of-band knowledge.
2. **The codes are not a closed, documented catalog.** They are ad-hoc string literals scattered across handlers, with no central registry and no guard test, so they drift and no client can exhaustively dispatch on them.

**Why it matters now (the yolo / multi-repo-server envelope).** Under minimal-human-intervention there is no operator to translate an opaque failure; an agent lane that can't read a failure in-band stalls or burns a turn re-deriving the cause through the CLI fallback. cc-fleet's entire delegation loop turns on a stable `error_code` the skill dispatches on (`internal/spawn/types.go:113`) and self-heals from (`FINGERPRINT_MISSING` → re-probe; `SKILL.md:101`), with vendor prose canonicalized so the code is authoritative (`internal/subagent/classify.go:111`). striatum has the **harder** half cc-fleet lacks — a real lease/recovery state machine — but not the **legible boundary** that lets an agent trigger it. This RFC closes that boundary; it adds no persistence and no new transport.

## Proposal

Three slices, smallest-viable first; each lands alone.

**P1 — Make the MCP `content` legible (the bug; a few lines).** In `toolResult`
(`go/pkg/mcp/tools.go`), on failure render the `content` text from the **code + message**
(+ suggestion once P2 lands), not `fmt.Sprint(name)`; keep a terse one-line summary on
success. The model reads `content` — put the dispatchable signal there. `structuredContent.error`
keeps carrying the code unchanged (back-compat). This alone retires the opaque-`<error>method</error>`
friction.

**P2 — Add remediation to the envelope.** Extend `rpc.Error` with a `Suggestion string`
(and an optional machine `recovery` hint reusing the RFC 0029 `recovery_commands` shape), thread
it through `ErrorResponse` → `Response.Data` → `toolResult`, and populate it for the high-traffic
families: lifecycle (`invalid_transition`, `already_*`), lease/session (`fresh_session_required`,
`*_unavailable`), capability (`capability_missing`, `token_*`), and the confirmation gates
(`branch_confirmation_required`, `worktree_required`). This is the field that makes self-heal
possible, mirroring cc-fleet's `Suggestion` (`internal/spawn/types.go:108`).

**P3 — Enumerate the codes as a closed contract.** A single Go const block in `pkg/rpc`
enumerating the error codes, each with a one-line meaning + default suggestion, and a catalog
section in `docs/reference/command-authority-matrix.md` (the existing oracle for the daemon
surface). Add a guard test — in the spirit of the existing authority-guardrail tests — that fails
if a handler returns a code absent from the registry. Mirrors cc-fleet's enumerated `ErrCode*`
(`internal/spawn/types.go:113`), adapted to striatum's `rpc.Error` rather than copied.

## Acceptance

- A test driving a tools/call that fails (e.g. a stale-lease `work.complete`) asserts the returned
  **`content` text carries the code and message** (not just the method name), and
  `structuredContent.error` still carries the code (back-compat preserved).
- For the chosen high-traffic codes, the error carries a non-empty `suggestion` end to end (RPC
  `Response.Data` and the MCP result).
- A guard test enumerates every error code a per-run / per-handler mutation can return and fails on
  an uncataloged one; `command-authority-matrix.md` gains the catalog section.
- Existing `pkg/mcp` + `pkg/rpc` suites stay green; `go vet` + `golangci-lint` clean.

## Non-goals (and one explicitly declined idea)

- **Not** a new error framework — reuse `rpc.Error`; no new transport, no new persistence.
- **Not** changing capability *denial* reasons (`go/pkg/rpc/capability.go` `DenialReason`) — they
  are already structured; P1 only ensures they reach the `content` block.
- **Not** changing exit codes (`rpc.Error.ExitCode`, default 10) or the front-matter exit-6 publish
  contract.
- **Explicitly declined: a "daemon-not-required" verb tier** (the comparison's §6.3 — run config /
  scaffold / read verbs with the daemon down). That idea was written from the generic
  single-operator-laptop framing in the comparison prompt. striatum's real deployment is a
  self-hosted multi-repo daemon, and RFC 0043 / D094 (daemon required for every verb) plus the
  just-accepted RFC 0110 (daemon→PostgreSQL auth + DB-enforced write boundary, D164) make a
  daemon-down write/scaffold path a **regression of the security boundary**, not a convenience. If
  ever revisited it must be its own RFC arguing against D094/0110 on the merits — not folded in
  here.

## Companion work items (recorded, not RFC-gated)

The comparison's other cc-fleet→striatum lessons need *doing*, not *deciding*; recorded here so
they are not lost — both are now tracked as issues:

- **Distribution (comparison §6.2) — [#160](https://github.com/halbritt/striatum/issues/160):** a
  tagged GoReleaser pipeline + one-line installer + npm, modeled on cc-fleet's `.goreleaser.yaml` /
  `install.sh` / `npm/`. The concrete fix for operator version-staleness and the "`make install`
  does not restart the running daemon" trap. Chore-sized.
- **One navigable architecture map (comparison §6.4) — [#161](https://github.com/halbritt/striatum/issues/161):**
  a single `CLAUDE.md`-style map of the daemon / PostgreSQL / RPC / MCP / lane substrate,
  *alongside* (not replacing) the 110 RFCs, to cut cold-start cost for the agents that drive
  striatum autonomously. Docs-sized.

## Relationship to prior RFCs / source

- **Source:** `STRIATUM_VS_CC_FLEET_CLAUDE_OPUS_4_8_2026-06-03.md` §6.1 (the headline), §6.2/§6.4
  (companions), §6.3 (the declined daemon-tier idea).
- **Builds on RFC 0030** (the daemon RPC envelope already provides `rpc.Error`) and the **RFC
  0020/0029** recovery vocabulary (the `suggestion`/`recovery` hint reuses `recovery_commands`).
- **Complements RFC 0103:** its `interrogation_unavailable` non-wedging signal is exactly this
  pattern done right — a legible, dispatchable code an agent acts on instead of wedging; P1–P3
  generalize that legibility to the whole RPC/MCP boundary.
- **Reference design:** cc-fleet's classified `Result` envelope (`internal/spawn/types.go:113`
  `ErrCode*` + `Suggestion`; `internal/subagent/classify.go:111` canonical messages;
  `skills/cc-fleet/SKILL.md:101` skill dispatch) — adapted to striatum's existing `rpc.Error`, not
  copied.

## Domain Modeling

This is a **boundary clarification** (per `docs/reference/domain-driven-design.md § "Adding to
the model"`): the daemon's *failure* surface becomes a first-class, enumerated part of the RPC/MCP
ubiquitous language — a value object (`rpc.Error` with `Code` + `Suggestion`) that crosses the
write boundary intact, rather than prose that degrades to a method name at the MCP edge. No new
aggregate, no new domain event.
