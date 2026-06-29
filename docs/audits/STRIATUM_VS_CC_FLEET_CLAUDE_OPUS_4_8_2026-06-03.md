---
type: record
status: frozen
owner: OPUS
expires: null
---

# striatum vs cc-fleet — a senior peer comparison

*Reviewer: Claude Opus 4.8 · 2026-06-03 · ~5,000 words*

Two single-developer, local-first projects in the same domain — run several terminal LLM
coding agents at once, one operator, homelab/laptop, no Kubernetes, no managed cloud — that
made opposite top-level bets. This is a lessons-extraction review, not a scorecard. Where one
side is plainly better on an axis I say so; where the difference is a real trade-off I name
what's being traded. The four voices stay separate throughout: **stated-a** (striatum's
docs), **stated-b** (cc-fleet's docs), **actual** (what the code does, project named), and
**mine** (my judgment).

Note up front: these are *different authors'* repos (striatum's remote is the local
maintainer's; cc-fleet's is `github.com/ethanhq/cc-fleet`, Ethan Guo). So this is genuinely a
comparison of independent designs, not a planned migration.

---

## 0. Files reviewed

**striatum** (rooted at cwd):
- `CLAUDE.md`, `AGENTS.md`
- `docs/reference/spec.md` (product boundary)
- `docs/reference/command-authority-matrix.md` (existence/size only)
- `docs/rfcs/` index — read titles of `0033`, `0043`, `0103`, `0104`, `0105`, `0106`, `0107`, `0109`
- `CHANGELOG.md` (version headers)
- `Makefile` (targets)
- `go/` layout: `cmd/{striatum,striatumd,striatum-supervisor-helper}`, `pkg/*`, `web/`
- `go/cmd/striatumd/` file listing (`main.go`, `web_service.go`, `web_mux_test.go`, `web_routes_test.go`, `pidfile.go`, …)
- `go/pkg/` listing (`mcp`, `recovery`, `mutations`, `reads`, `webservice`, `adapterconformance`, `sessionliveness`, …)
- `go/pkg/recovery/` (`scheduler.go`, `sweep.go`)
- `go/pkg/mutations/` references (`supervision.go`, `recovery_decision_tree.go`, `run_lock_deadlock_test.go`) via grep
- `go/pkg/webservice/` (`service.go`, `live_stream.go`, `identity.go`)
- LOC counts: ~58k non-test, ~36k test

**cc-fleet** (`../cc-fleet`):
- `README.md`, `CLAUDE.md`, `AGENTS.md`, `CONTRIBUTING.md` (referenced), `docs/cli.md` (head)
- `internal/` full package listing (29 packages)
- `cmd/cc-fleet/` file listing
- `internal/spawn/spawn.go`, `internal/spawn/types.go`
- `internal/config/lock.go`
- `internal/fingerprint/capture.go`, `internal/fingerprint/apply.go`, package listing (`bundled.go`, `resolve.go`, `validate.go`, `cache.go`, `default_fingerprint.json`)
- `internal/secrets/dispatch.go`, package listing (`keyset.go`)
- `internal/subagent/classify.go`, `internal/subagent/types.go` (via grep), package listing
- `internal/ids/` (`validate.go`, `typed.go`, `vendor.go` via grep)
- `skills/cc-fleet/SKILL.md`, `references/` listing
- `.goreleaser.yaml`, `.claude-plugin/{marketplace.json,plugin.json}`, `npm/package.json`, `hooks/`, `commands/`
- `git log` (35 commits, tags `v0.1.0`–`v0.1.2`)
- LOC counts: ~17k non-test, ~19k test

---

## 1. Executive summary

- **The single most important difference is build-vs-borrow.** striatum builds its own
  orchestration plane — a long-lived daemon owning a PostgreSQL instance as authoritative
  state, driving agents as adapters over RPC/MCP. cc-fleet declines to build one: it makes
  vendor LLMs into *real Claude Code agent-team teammates* the host already knows how to
  drive (`TeamCreate`/`SendMessage`), and only adds backend-swap + key-safety + pane
  management. Almost everything else downstream follows from this one bet.
- **That bet is a true trade-off, not a maturity gap.** striatum buys durability, provenance,
  multi-model adjudication, and survival across daemon restarts — at the cost of ~58k LOC, a
  Postgres dependency, 110 RFCs of governance, and a deadlock surface it had to author
  RFC 0104 to remove. cc-fleet buys native UX, zero infra, and a 17k-LOC footprint — at the
  cost of being welded to Claude Code's private spawn flags and an experimental flag.
- **cc-fleet has the cleaner failure contract and striatum should steal the discipline.**
  `spawn.Spawn`/`subagent.Run` *never return a Go error* — every failure is a stable
  `error_code` the skill dispatches on (`internal/spawn/types.go:113`). striatum's equivalent
  is scattered across lease/recovery state, and its MCP state-changers return contentless
  `<error>method</error>` that force a CLI re-run to read the real message.
- **cc-fleet's distribution is in a different league and is cheaply copyable.** goreleaser +
  npm OIDC + one-line curl + Claude Code plugin marketplace, all wired (`.goreleaser.yaml`,
  `npm/`, `install.sh`, `.claude-plugin/`). striatum ships `make install` + make-based
  release archives only. One-sided.
- **striatum's anti-hallucination machinery has no cc-fleet analogue, and that's a framing
  difference, not negligence.** Durable artifacts, provenance, and interrogating multi-model
  review panels are striatum's reason to exist; cc-fleet is a delegation tool, not a
  verification system.
- **Both independently re-derived the same five disciplines:** one static Go binary, `flock`
  with an explicitly documented cycle-free lock order, classified results over raw errors,
  name-validation as a security boundary, key-never-in-argv. When two solo builds in one
  domain converge on the same five, they're load-bearing — make them defaults for attempt
  three.
- **The fingerprint recipe is cc-fleet's most interesting idea and its largest liability in a
  single object.** Reverse-engineering CC's Agent spawn argv from a live process
  (`internal/fingerprint/capture.go`) is genuinely clever *and* a hard dependency on
  undocumented internals. The mitigations are exactly right; the coupling is still the thing
  most likely to break.
- **Walk away believing:** striatum is not "overbuilt cc-fleet" and cc-fleet is not
  "unfinished striatum." They are two defensible points on a frontier defined by how much
  orchestration you *own* vs *borrow*. The synthesis is to own state + provenance while
  borrowing the host's spawn + UX — explicitly, not by reimplementing either.

---

## 2. Shared problem statement

Both solve: *coordinate multiple terminal-based LLM coding agents, for a single operator,
on local hardware, with no hosted control plane.*

**stated-a** (`docs/reference/spec.md`): striatum is "a standalone, local-first workflow
runner for terminal-based AI coding agents [that] coordinates registered target repositories
through a local daemon, daemon RPC methods, and capability-gated client surfaces (CLI, MCP,
and local web UI)." It is emphatic about what it is *not*: "does not provide hosted services,
external persistence, telemetry, … durable transcript capture … or automatic commits." Per
D094/RFC 0043, "the daemon is a hard prerequisite for every Striatum verb."

**stated-b** (cc-fleet `README.md` / `CLAUDE.md`): "Spawn any vendor LLM … as real Claude
Code teammates or one-shot subagents." The core trick, stated plainly: "a vendor worker is a
genuine `claude` process whose LLM backend is swapped by launching it with `--settings
<vendor-profile>.json` … and `--model <vendor-model-id>`. The main session's own auth … is
never touched."

**The framing divergence — the part that usually gets buried.** striatum frames the problem
as *workflow*: phases, lanes, reviews, artifacts, provenance; agents are interchangeable
adapters moved through a state machine the daemon owns. cc-fleet frames it as *delegation*:
the unit is a teammate or subagent you hand a task to; the host's agent-teams already supplies
the coordination, so cc-fleet only swaps the backend and guards the key. striatum is asking
"how do I run a *trustworthy multi-step process* across models?" cc-fleet is asking "how do I
get a *cheaper or different model* to do this chunk, with native ergonomics?"

**mine:** these are orthogonal questions that happen to share a substrate (local + multiple
`claude` processes). A feature matrix would flatten exactly the interesting thing — the two
barely overlap in the middle. striatum's overlap with cc-fleet is its *lane adapter* layer;
cc-fleet's overlap with striatum is its *subagent fan-out*. Everything else is each project
answering its own question. The most common misread of this pair would be "cc-fleet is a
lighter striatum"; it isn't — it's a different program.

---

## 3. Architecture side-by-side

| Axis | striatum | cc-fleet |
|---|---|---|
| Language / binaries | Go; 3 binaries (`striatum`, `striatumd`, `striatum-supervisor-helper`) | Go; 1 binary (`cc-fleet`, `ccf` alias) |
| LOC (non-test / test) | ~58k / ~36k | ~17k / ~19k |
| Authoritative state | daemon-owned PostgreSQL, per `repository_id` (`spec.md`; RFC 0033/0043) | none of its own — borrows `~/.claude/teams/<team>/config.json` + tmux + `vendors.toml` |
| Process model | long-lived daemon owns everything; CLI/MCP are clients | many short-lived external procs, serialized by `flock` |
| Coordination surface | daemon RPC + MCP tools + capability tokens | native CC `TeamCreate`/`SendMessage`; cc-fleet only spawns/tears down |
| Concurrency control | per-run lock (RFC 0104), leases, lane attestation | 3 disjoint `flock` scopes w/ written order (`internal/config/lock.go`) |
| Operator surface | `dashboard --once`; mounted, route-tested web service (`go/cmd/striatumd/web_service.go`, `web_routes_test.go`) | Bubbletea TUI (`internal/tui`: vendor hub + agent-status board) |
| Failure model | lease/recovery/requeue state machine (`pkg/recovery`, `pkg/mutations`) | classified `Result` `error_code` envelope (`spawn/types.go`, `subagent/types.go`) |
| Tests | ~36k LOC; reliability harness (RFC 0105); golangci pinned | stated-b 633 tests; actual 79 `*_test.go`, ~19k LOC; `go test -race`+`vet`+`gofmt`, no golangci |
| Release | `make install` + make release-archives; no goreleaser/npm/curl/plugin | goreleaser + npm OIDC + curl installer + CC plugin marketplace |
| Maturity | v2.11.0; 110 RFCs | v0.1.2; 35 commits |

**Where docs and code disagree.**

- *striatum:* a prior architecture note in my own working memory (from the v2.4.0 era) recorded
  "the Go web service isn't mounted in the daemon." **actual (now):** `go/cmd/striatumd/`
  carries `web_service.go` plus `web_mux_test.go` and `web_routes_test.go`, and
  `go/pkg/webservice/service.go` exists — the web surface is wired and route-tested. The stale
  note is wrong; the code says mounted. (The maintainer's recent README doc-truth pass is the
  likely reason it drifted closed.)
- *cc-fleet:* `README.md` lists macOS as a "tested platform," but `internal/fingerprint/capture.go:62`
  branches to `captureFromPidDarwin` because "macOS can't read another process's environ" — so
  on darwin the *env half* of the recipe is the two-constant `envAllowlist`
  (`CLAUDECODE`, `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`), assumed rather than captured. stated-b
  "supported"; actual "supported with a narrower capture." Real, minor.
- *cc-fleet:* three version numbers across channels — `plugin.json` says `0.1.1`,
  `npm/package.json` says `0.1.0`, latest git tag is `v0.1.2`. Harmless today; a single source
  would prevent a future "which version am I actually running" support thread.

---

## 4. Axis-by-axis

### 4.1 Orchestration substrate — own a plane vs borrow the host's *(trade-off; the spine)*

striatum owns an authoritative store: daemon + PostgreSQL, per `repository_id`
(`spec.md`). Marker files, tmux panes, terminal output are explicitly "never live
control-plane state." cc-fleet owns *no* authoritative store; it writes `Member` rows into
the host's `~/.claude/teams/<team>/config.json` so panes are "discoverable and teardownable"
(`CLAUDE.md`), and drives work through CC's own team tools.

**Verdict: trade-off, with one-sided edges.** striatum's store survives restarts, gives a
queryable history, and lets a run be reconstructed after every pane is gone — cc-fleet has
none of that. cc-fleet's borrow means zero infra, instant native UX, and that an operator
already running Claude Code needs no new daemon. **mine:** the one-sided edge inside the
trade-off is *availability cost*: striatum made the daemon a hard prerequisite for *every*
verb and retired `--no-daemon`. For a single-operator homelab tool, gating read-only,
scaffold, and config verbs behind a live daemon is a self-imposed tax — and cc-fleet's design
is the existence proof that a large class of operations needs no central authority at all.
**I disagree with stated-a here:** "daemon required for every verb" is correct as a *write*
invariant (D094's real point) and over-broad as a *blanket* one.

**Lesson:** decide per-verb whether it needs the authority, not per-product.

### 4.2 Coupling to Claude Code internals *(trade-off with a sharp edge)*

cc-fleet's load-bearing idea is the fingerprint "recipe": to make a vendor worker behave like
a native teammate it must launch `claude` with the *exact* argv/env CC itself uses.
`CaptureFromPid` reads a live native-Agent's `/proc/<pid>/{cmdline,environ}`, `templatize()`
replaces per-spawn values with placeholders (`--agent-id {name}@{team}`, `--parent-session-id
{lead_session_id}`, …) and strips `--model`/`--settings` (`capture.go:164`), and `Apply`
substitutes them back at spawn (`apply.go:39`). It is gated on the experimental
`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` flag. striatum has no analogue — it owns its own spawn
path and drives `claude`/`codex`/`agy` as adapters, so a CC upgrade can't drift its flags.

**Verdict: trade-off, but lopsided in *fragility*.** cc-fleet gets a real Claude Code teammate
— full tool stack, native coordination, the lead's own UX — for free, which striatum can only
approximate. The price is that cc-fleet's correctness is hostage to an upstream it doesn't
control: an experimental flag that can be removed, and undocumented argv that can change. The
mitigations are textbook (and worth stealing the *shape* of): a **bundled default recipe**
(`bundled.go`) so a fresh install spawns with no probe; `CurrentVersionExceedsRecipe` gating a
post-spawn **settle check** so a matched version pays no latency; `SPAWN_DID_NOT_SETTLE` /
`FINGERPRINT_MISSING` codes that trigger a **skill self-heal** re-probe. **mine:** this is the
right way to *carry* the debt, but it's still debt. **I disagree with the implicit stated-b
premise** that borrowing the host plane is "free" — for a tool whose entire value proposition
is *reliable delegation*, a structural dependency on the host's next release is an exposure,
not a footnote. cc-fleet has clearly internalized this (the self-heal flow exists precisely for
it), so the disagreement is with the marketing framing, not the engineering.

**Lesson:** when you reverse-engineer a host's internals to ride its UX, you take a loan against
its next release. Budget for it at construction time (bundled fallback + drift detector +
self-heal), exactly as cc-fleet did.

### 4.3 Failure-handling contract *(cc-fleet one-sided on clarity; striatum one-sided on depth)*

cc-fleet: `Spawn`/`Run` return a `Result{ok, error_code, error_msg, suggestion}` and *never* a
raw Go error (`spawn/types.go:69`, `spawn.go:498` re-categorizes every lock-region error back
into a code). The subagent classifier is richer still — `classify.go` maps vendor HTTP
behavior into `KEY_INVALID` / `RATE_LIMITED` / `INSUFFICIENT_BALANCE` / `MODEL_NOT_FOUND` /
`VENDOR_API_ERROR` and *canonicalizes* the message so vendor prose never leaks
(`classify.go:111`). The skill dispatches on the code (`SKILL.md:101`).

striatum's failure handling is deeper but less uniform: a lease/attempt/recovery state machine
(`pkg/recovery/sweep.go`, `pkg/mutations/recovery_decision_tree.go`) that can *requeue* a dead
agent and reassign work — something cc-fleet has no concept of. But its client-facing errors
are famously opaque: MCP state-changers can return a contentless `<error>method</error>` that
forces a CLI re-run to surface the real message.

**Verdict: a genuine split, not symmetry.** cc-fleet wins decisively on *contract clarity and
machine-dispatchability*. striatum wins decisively on *recoverability depth* (it can rescue a
wedged multi-step run; cc-fleet can only classify-and-report a single spawn). These are
different goods. **mine:** the clarity gap is the more *embarrassing* one because it's free to
fix — see §6.1.

**Lesson:** a multi-agent supervisor needs both — stable codes at the boundary *and* a recovery
machine behind it. Each project nailed one and under-built the other.

### 4.4 Key / credential safety *(roughly even; both strong)*

cc-fleet: the vendor key never touches `env`/`argv`/`ps`/history. `profile.GenerateForVendor`
writes `apiKeyHelper: "<cc-fleet-abs-path> keyget <vendor>"`; CC invokes that at runtime and
`secrets.Keyget` (`dispatch.go:32`) resolves from `file`|`pass`|`1password`|`vault`|`keyring`,
writing the key to stdout once. "Nothing in this package may log the key bytes." The spawn
command even prepends `env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN` (`spawn.go:704`) so a
main session in API-key mode can't leak its real key into a vendor pane. The `file` backend
supports multi-key sets with `off`/`round_robin`/`random` rotation, and rotation explicitly
errors on an unknown strategy rather than defaulting (`dispatch.go:174`).

striatum: session-bound capability tokens wired into lane env at supervise start (RFC 0103 W1),
plus lane attestation and write-scope guarding.

**Verdict: even, with a small edge to cc-fleet on *secret-at-rest* breadth** (five backends,
rotation) and to striatum on *scope* (the token is capability-bounded, not just a key). Both
treat the secret as something `ps aux` must never see — see §8.

**Lesson:** none new — this is a convergent floor, covered in §8.

### 4.5 Provenance / anti-hallucination *(one-sided: striatum)*

striatum's reason to exist is anti-hallucination via multi-model review and durable provenance:
interrogating review panels, decision/finding/synthesis artifacts that must validate against a
V1 schema (the publisher refuses invalid front matter with exit 6), and process-execution rows
that gate publication. cc-fleet has *none* of this — a subagent returns a result on stdout
(`internal/subagent`), a teammate's work lives in its pane and is read back ad hoc
(`SKILL.md:98`: "read the pane directly"). There is no durable record of *what a teammate did*
once the pane is gone.

**Verdict: one-sided (striatum), but it's a framing consequence, not a defect.** cc-fleet never
set out to verify work; it set out to delegate it. Judging cc-fleet for lacking provenance is
like judging a forklift for not being a scale. **mine:** that said, cc-fleet would *benefit*
from a sliver of it (§5.1) — "what did I pay for and what did it produce" is a real operator
question its current design can't answer after the fact.

**Lesson:** delegation and verification are separable, and most tools build one. The interesting
move is offering verification as an *optional mode over* delegation, not as a mandatory engine
around it (§9).

### 4.6 Vendor breadth *(one-sided: cc-fleet)*

cc-fleet's whole point is breadth: any Anthropic-compatible endpoint
(DeepSeek/GLM/Qwen/Kimi/MiniMax), registered as a profile, with a `/v1/models` probe and a
model picker. striatum pins three adapters (`claude`/`codex`/`agy`) with first-class seat
support tiers (RFC 0109).

**Verdict: one-sided (cc-fleet) on breadth; one-sided (striatum) on depth-per-adapter.**
cc-fleet can onboard a new vendor in a `vendors.toml` row; striatum graduates an adapter
through a support-tier process with a lint and a graduation guard. Different goods again, but on
the literal axis "how many backends can I use," cc-fleet wins outright.

**Lesson:** breadth is a config-schema problem; depth is a governance problem. cc-fleet shows how
cheap breadth is when you don't also owe each backend a provenance contract.

### 4.7 Distribution & packaging *(one-sided: cc-fleet)*

cc-fleet ships through four channels, all wired: GoReleaser on a `v*` tag (`.goreleaser.yaml`,
`CGO_ENABLED=0`, 4 platforms), npm via OIDC trusted publishing (`npm/`, postinstall fetches the
platform binary), a one-line `curl | sh` installer, and the Claude Code plugin marketplace
(`.claude-plugin/`, with `commands/` and a SessionStart hook in `hooks/`). striatum ships
`make install` plus make-based `release-archives`/`package-smoke` — no goreleaser, npm, curl
installer, or plugin channel.

**Verdict: one-sided (cc-fleet), unambiguously.** **mine:** this directly addresses a pain the
maintainer has hit on the striatum side — operators on target repos going stale because the
source moved and `make install` is a manual, daemon-restart-requiring step. cc-fleet's
`install.sh` + `.goreleaser.yaml` are a near-liftable template.

**Lesson:** for a tool you want *other people's repos* to adopt, packaging is a feature, and it's
the cheapest high-leverage one here.

### 4.8 Concurrency model & deadlock surface *(trade-off, leaning to cc-fleet on simplicity-at-scale)*

cc-fleet, being many short-lived external processes, "cannot serialize in-process" and uses
`flock` with three *disjoint* scopes and a written, cycle-free order: vendors-config outermost,
team, server innermost (`internal/config/lock.go:96-101`, and the lock dance is re-explained
inline at `spawn.go:393-402`). The scopes guard disjoint resources, so "no acquisition cycle
exists today." striatum owns a long-lived daemon and a far larger shared-state surface; it had
a real **sessions↔runs deadlock** that RFC 0104 (per-run serialization invariant) was authored
to fix, with a gate-first reproduction at `go/pkg/mutations/run_lock_deadlock_test.go`.

**Verdict: trade-off in principle, but the evidence leans one way.** striatum's owned plane gives
it transactional guarantees cc-fleet can't make (cc-fleet's flock model can't, e.g., atomically
reassign a job across lanes). But the owned plane is also what *created* a deadlock big enough to
need a rescue RFC. cc-fleet avoided cycles the cheap way — disjoint scopes + a documented order —
and hasn't needed a rescue. **mine:** at single-operator scale, cc-fleet's discipline is the
better default; striatum re-learned "every lock you own is a deadlock you can author" the hard way.

**Lesson:** write the lock order down *before* you have three locks, and keep scopes disjoint. The
cheapest concurrency bug is the one your ordering rule made impossible.

### 4.9 Conceptual surface area & governance *(trade-off: governance vs velocity)*

striatum carries 110 RFCs, a decision log, a ubiquitous-language doc, a command-authority matrix
(28 KB), and a "read these 7 docs in order" onboarding. cc-fleet carries one `CLAUDE.md` that
packs the entire architecture — the two execution modes, the fingerprint idea, key safety, the
lock order, the JSON envelope contract — into a single navigable file, plus `CONTRIBUTING.md`
and `docs/cli.md`.

**Verdict: trade-off.** striatum's governance is *appropriate to its stakes* (a daemon owning a
DB across multi-step autonomous runs needs traceable decisions) and is *also* a real cold-start
tax. cc-fleet's lean surface is *appropriate to its stakes* (a CLI + one skill) and would be
under-documented if it owned what striatum owns. **mine:** the lesson runs both ways — striatum
would benefit from a single CLAUDE.md-style architecture map *in addition to* the RFCs (§6.4);
cc-fleet would need striatum's discipline the moment it grew a durable store.

**Lesson:** match documentation weight to the blast radius of being wrong. Neither project is
mis-calibrated *for itself*; the error would be copying either's doc weight to the other.

---

## 5. Ideas worth stealing — striatum → cc-fleet

**5.1 A durable provenance ledger for subagent/teammate jobs.** *(effort: days; risk: low)*
cc-fleet's jobs are fire-and-poll (`internal/subagent/job.go`, a jobs dir) and teammate work
lives only in a pane. Steal striatum's "every execution leaves a durable, hashed record" idea in
miniature: a single append-only JSONL under `~/.config/cc-fleet/` of
`(job_id, vendor, model, prompt_hash, result_hash, cost_usd, ts)`. Rationale: answers "what did
I pay for and what did it produce" after the panes are gone; enables cost audit and crude
reproducibility. **Risk to guard:** scope-creep toward a DB — keep it a flat file, no daemon.
This is the one place cc-fleet's framing leaves a real operator question unanswerable.

**5.2 Content-level settle, not just pane-liveness.** *(effort: days; risk: false negatives on
slow models)* `settleOK` (`spawn.go:612`) checks only that the pane survived 2s — a
cross-platform liveness signal, deliberately not `/proc`-based. But striatum learned (its
lane-attestation work; publish requires a `process_executions` row) that *process alive ≠
process did the work*. cc-fleet already documents the exact failure: weaker models "finish and go
idle without calling SendMessage" (`SKILL.md:98`). A lightweight output-presence probe would
catch the silent-finish case in the tool rather than leaving it to skill-side discipline.

**5.3 Server-side reap policy for wedged teammates.** *(effort: days; risk: low)* A vendor
teammate can wedge forever on a 429/balance/401 and "never go idle and never messages you"
(`SKILL.md:97`); today the *skill* works around it with client-side polling + timeouts. striatum's
lease model makes liveness the *system's* job, not the operator's. Move the timeout/health into
`cc-fleet ps --check` as an optional reap-after-N-failures policy, so an unattended fan-out can't
leave a quietly-billing zombie. cc-fleet already has the reap primitive
(`spawn.go:54 defaultReapAgentProcess`, by `--agent-id`) — this is wiring, not new machinery.

---

## 6. Ideas worth stealing — cc-fleet → striatum

**6.1 The classified `Result` envelope as the universal RPC contract.** *(effort: weeks; risk:
large retrofit surface; leverage: very high)* striatum's opaque `<error>method</error>` is the
single most-cited friction on the striatum side. Adopt cc-fleet's discipline at the RPC envelope
layer *once*: every failure carries a stable `error_code` + `suggestion`, dispatched on by
clients and skills, with the raw/vendor message canonicalized (cc-fleet's `classify.go:111` is
the model). The payoff isn't just readability — it's that the *self-heal* pattern
(`FINGERPRINT_MISSING` → re-probe) is exactly the shape striatum's recovery flow wants to expose
to its agents. Do it at the boundary, not per-handler.

**6.2 Real distribution: goreleaser + curl installer + npm.** *(effort: days; risk: low)*
cc-fleet's `.goreleaser.yaml` + `install.sh` + `npm/install.js` is a near-liftable template. This
is the concrete fix for striatum's operator version-staleness problem (operators on target repos
falling behind source; `make install` not even restarting the daemon). Ship a tagged release
pipeline and a one-line installer; it's the highest leverage-per-hour item on this whole list.

**6.3 Carve out a daemon-not-required tier.** *(effort: days–weeks, mostly boundary-drawing;
risk: must not become a second write path)* cc-fleet is the existence proof that config, listing,
scaffold, and doctor-style verbs need no central authority. striatum's blanket "daemon required
for every verb" (`spec.md`) is over-broad for a single-operator tool. Define a pure-local verb
tier that runs daemon-down. **The hard constraint:** it must never become a second authoritative
write path — that's the whole point of D094 — so confine it to reads + local scaffolding + the
already-local one-shot operations.

**6.4 One navigable architecture map.** *(effort: hours; risk: none)* cc-fleet's `CLAUDE.md`
fits the entire system — modes, fingerprint, key safety, lock order, envelope — into one file an
agent can hold in working memory. striatum's onboarding is seven docs. Add a single
CLAUDE.md-style map *alongside* (not replacing) the RFCs; it cuts cold-start cost for both humans
and agents, which for a project the maintainer drives autonomously is a direct productivity win.

---

## 7. Dead ends and anti-patterns

**cc-fleet — the fingerprint coupling is structural debt, well-carried.** The entire
`internal/fingerprint` package exists for one reason: "Native `Agent({model})` cannot accept
`--settings <path>` or vendor model ids — that's exactly the gap cc-fleet fills" (`SKILL.md:10`).
If Claude Code ever exposes a backend override for Agents, `capture.go`/`apply.go`/`bundled.go`
become dead weight overnight. The failure mode it teaches: *riding a host's UX by
reverse-engineering its internals is a bet on the host's stability you must price in.* cc-fleet
priced it well (bundled default, version-gated settle, self-heal probe), which is the lesson worth
keeping even after the package someday dies.

**striatum — a self-inflicted deadlock from the owned-plane choice.** The sessions↔runs deadlock
that RFC 0104 fixes (`go/pkg/mutations/run_lock_deadlock_test.go`) was a global serialization
invariant that inverted under concurrency — a class of bug cc-fleet's disjoint-flock model can't
have. The teaching: owning the plane means owning every lock, and every lock is a deadlock you
might author. The mitigation (per-run lock, gate-first reproduction) is right; the cheaper path
was cc-fleet's — keep scopes disjoint and write the order down (`config/lock.go:96`).

**Shared hazard, found independently — the orphaned billing agent.** cc-fleet reaps reparented
`claude` processes by `--agent-id` after a failed spawn (`spawn.go:54`, and on teardown);
striatum had the masked-dead-agent wedge (#147, `pkg/recovery/sweep.go`) where an
operator-heartbeated lease hid a dead process and a job stuck `running` forever. Same hazard:
in a local multi-agent system, **process liveness and logical liveness diverge**, and the fix on
both sides was the same — probe the actual PID, don't trust the bookkeeping. That two independent
designs hit it is the signal it's intrinsic.

---

## 8. Convergent decisions (load-bearing)

These are the places both projects, independently, arrived at the same design. They keep getting
re-discovered because they are the irreducible floor of "run agent processes locally for one
operator."

1. **One static Go binary, CGO off.** cc-fleet `.goreleaser.yaml` (`CGO_ENABLED=0`, 4 platforms);
   striatum's three binaries. Cross-compiles, no runtime deps, drops onto a homelab box. For
   local-first single-operator tools this is the correct substrate, full stop.
2. **`flock` with an explicit, cycle-free, *written* lock order.** cc-fleet
   (`config/lock.go`, order stated in the doc comment); striatum (per-run lock invariant,
   RFC 0104). Both learned the concurrency model must be *stated*, not implicit.
3. **Classified results over raw errors.** cc-fleet's `Result`/`error_code`; striatum's recovery
   decision tree (and, per §6.1, where it should go next). A multi-agent supervisor must turn
   failures into machine-dispatchable categories or its automation can't reason about them.
4. **Name validation as a security boundary.** cc-fleet `internal/ids`
   (`ValidateVendorName`/`ValidateTeamName`/`EnsureUnderRoot`) because names flow into
   `filepath.Join` (traversal) and the `apiKeyHelper` shell string (injection); striatum's
   capability-token + write-scope path guarding. Both treat caller-supplied names as hostile.
5. **The key never reaches argv/env.** cc-fleet's `apiKeyHelper` + `env -u` scrub
   (`spawn.go:704`); striatum's session-bound capability tokens in lane env (RFC 0103 W1). Both
   refuse to put the secret where `ps aux` can read it.

**mine:** any third attempt in this domain should start with all five as defaults, not
re-derive them. Their independent rediscovery here is about as strong a signal as a two-sample
study can give.

---

## 9. The synthesis project

Not a merge — a synthesis. Start from **cc-fleet's bet** (borrow the host's spawn + UX) and add
*only* the two striatum strengths the borrow can't provide — durable provenance and multi-model
adjudication — as a *thin local layer*, never as a daemon+Postgres plane.

- **Substrate.** Live coordination stays in the host's existing dirs (`~/.claude/teams`) the
  cc-fleet way. Provenance goes in a *single append-only local ledger* — SQLite or JSONL, not a
  server, not a required daemon. striatum's D094 lesson ("one authoritative store beats
  marker-file soup") is right; cc-fleet's lesson ("the store needn't be a daemon you must keep
  alive for every verb") is *also* right. Reconcile them: one store, but local and optional-to-write.
- **Spawn.** cc-fleet's fingerprint-or-native path, carried with its full mitigation kit. Drop the
  fingerprint package the day CC exposes a backend override; until then, bundled default + settle
  gate + self-heal.
- **Orchestration.** Borrow agent-teams for live driving (cc-fleet), but record every delegation
  as a durable, hashed artifact (striatum) so a run is reproducible and auditable after the panes
  close.
- **Verification.** striatum's interrogating review panel is the genuinely novel asset of the two
  codebases. Keep it — but as an *invokable mode over* cc-fleet-style teammates ("spawn three
  vendors, have them cross-examine this diff"), not a workflow engine you must adopt wholesale.
  This is exactly the §4.5 move: verification as an option over delegation.
- **Failure & liveness.** cc-fleet's `error_code` envelope at every boundary; striatum's PID-level
  liveness probe for reaping (the §7 shared lesson) as the default health check.
- **Boundary.** Daemon *optional*. Pure-local verbs (config, scaffold, one-shot subagent,
  provenance read) never touch it; only a multi-run scheduler/lease arbiter does, and that can be
  a per-run lock (RFC 0104) rather than an always-on server.

**Net shape:** roughly cc-fleet's footprint, plus a provenance ledger, plus an optional
adjudication mode. The third attempt is *cc-fleet that remembers what it did and can be told to
verify itself* — without becoming striatum.

---

## 10. Open questions

- **striatum:** I read `spec.md`, the package layout, `recovery/sweep.go`, the `mutations`
  references, and the RFC index, but did not read `command-authority-matrix.md` line-by-line or
  run the daemon — so I can't confirm *from code* which verbs actually enforce the daemon-required
  invariant versus degrade. That matrix is the oracle for §4.1/§6.3; my read there leans on the
  spec's stated rule.
- **striatum:** whether the interrogating panel's verdicts are *semantically scored* or only
  *structurally checked*. Prior context (the discharge-evidence threat-model note) says the
  contract only typechecks row presence; I couldn't confirm the current state this session. It
  matters for how much §4.5's "anti-hallucination" claim is load-bearing vs aspirational.
- **cc-fleet:** the *real fingerprint drift rate* — how often a CC upgrade actually breaks the
  bundled recipe — is an operational fact invisible in the repo. The entire value of the
  settle/self-heal machinery is a function of it.
- **cc-fleet:** whether the macOS two-constant `envAllowlist` (`capture.go:21`) is sufficient
  across CC versions, or whether some darwin spawns need a third env var that can't be captured.
  The code assumes two; I can't verify completeness without a Mac and multiple CC builds.
- **Both:** neither test suite, as far as a static read shows, exercises the hardest behavior —
  the *cross-model consensus/coordination* path end to end (striatum's panel adjudication under
  genuine model disagreement; cc-fleet's multi-teammate fan-out + result merge). On both sides the
  most important behavior is the least unit-testable, and both lean on integration/dogfood runs to
  cover it. That's not a criticism so much as the shared frontier of the domain.
