---
type: record
status: frozen
owner: OPUS
expires: null
---

# Striatum — Repo Hygiene Pass (CLAUDE_OPUS_4_8, 2026-05-30)

Project: **striatum** — a Go-only, local-first workflow runner for terminal AI
coding agents; daemon-owned PostgreSQL is the authoritative live state; the
legacy Python runtime was retired per RFC 0078.

Audit was fanned out across four read-only sub-agents (root docs, `src/`,
`.agents/`, `docs/`), then executed serially on a `cleanup/2026-05-30` branch so
no two deletes raced the git index. Preflight: working tree was clean; I was on
`main`, so per the prompt's own rule I branched rather than editing `main`
destructively.

## 0. What I checked

- **Repo root** — every top-level file and directory; classified conventional
  (README, AGENTS.md, CLAUDE.md, CHANGELOG.md, CONTRIBUTING.md, LICENSE,
  Makefile, VERSION, .gitignore, .github/) vs. suspect.
- **Tracked-vs-ignored** — confirmed `.venv`, `.mypy_cache`, `.pytest_cache`,
  `.ruff_cache`, `build/`, `.scratch/`, `.gemini/`, `dist/`, `go/bin/` are
  already gitignored (0 tracked files) — not cruft.
- **`src/`** (72 tracked files) — committed binary, TS/React frontend, legacy web
  assets, vite build output, frontend config; cross-checked against Makefile,
  `.github/workflows/ci.yml`, and Go `embed`/static-serving code.
- **`.agents/`** (198 tracked files, 1.4M) — provenance, structure, and every
  reference path.
- **`docs/`** — the "Start Here" set (README, docs/index.md,
  docs/reference/spec.md, decision-log, ubiquitous-language, todo), `contracts/`,
  single-file directories, `docs/_archive/`, `scripts/`, `skills/`.
- **Skips:** `.git/`, `.claude/worktrees/*` (615M of *locked, in-use* git
  worktrees — out of scope), `docs/_archive/` content (intentional history per
  AGENTS.md), `docs/dogfood/` (gitignored historical blob).

## 1. Executive summary

- **205 files changed, +3 / −11,197 lines.** Root markdown dropped from 10 files
  to the 5 conventional ones.
- Removed **`.agents/`** — 198 accidentally-committed multi-agent "teamwork
  preview" scratch files (handoff/plan/progress/briefing), zero code references;
  added it to `.gitignore`.
- Removed a **19M committed `striatumd-linux-x86_64` binary** under the retired
  `src/` tree — zero references; the real daemon builds from `go/`.
- Deleted **2 stale root planning docs** (`implementation_plan.md`, `PROJECT.md`)
  for completed GH #28 work; their only referrers were inside the removed
  `.agents/`.
- Archived **3 root artifacts** (`ORIGINAL_REQUEST.md` + two
  `STRIATUM_ARCHITECTURE_REVIEW_*.md`) into `docs/_archive/` via `git mv`.
- **Deferred** the bulk of `src/` (the TS frontend is still wired into the
  Makefile `ui-check-bundle` gate — removing it is RFC 0078 Gate G, a coordinated
  CI change, not a hygiene delete) and two stale-doc rewrites.
- Go build stayed green after every commit; the deferred frontend is untouched.

## 2. Branch and commits

Branch: **`cleanup/2026-05-30`** (off `main` @ `6a43c13`). Each category is its
own commit; revert any one with `git revert <sha>`.

| sha | subject |
|---|---|
| `2d5d7a8` | chore(hygiene): remove accidentally-committed .agents/ teamwork scratch (198 files) |
| `ff5ee60` | chore(hygiene): delete stale root planning docs (implementation_plan.md, PROJECT.md) |
| `605aeb2` | chore(hygiene): archive root request/review artifacts into docs/_archive/ |
| `bddbf62` | chore(hygiene): remove 19M committed striatumd-linux-x86_64 binary |

## 3. Done: deletions

- **`.agents/`** (198 files, 1.4M) — `2d5d7a8`. Added in `335c349` "docs: add
  teamwork artifact set"; pure agent-run output (`teamwork_preview_*`,
  `victory_auditor*`, `orchestrator_gen*`). Safety: `grep` for `.agents` in
  `go/`, `Makefile`, `scripts/`, `.github/`, and `docs/reference/` returned no
  load-bearing reads (the only hits were self-referential paths inside the
  artifacts themselves, plus a *theoretical* `<worktree>/.agents/` mention in a
  research doc, which is a different path). Not in the product boundary
  (`AGENTS.md` documents `.striatum/` as the scratch boundary, never `.agents/`).
  Added `.agents/` to `.gitignore`.
- **`implementation_plan.md`, `PROJECT.md`** — `ff5ee60`. GH #28 planning docs
  (milestones M1–M4 marked DONE, dated 2026-05-20/29). Safety: zero references
  outside the removed `.agents/`; `AGENTS.md` points new agents to
  `docs/operator/BRIEF.md`, not these.
- **`src/striatum/_daemongo/binaries/striatumd-linux-x86_64`** (19M) — `bddbf62`.
  Safety: `grep -r '_daemongo/binaries|striatumd-linux-x86_64'` across `go/`,
  `Makefile`, `scripts/`, `.github/` returned nothing; the daemon builds from
  `go/` (`make -C go build`). Committed binaries do not belong in the tree.

## 4. Done: moves and flattenings

All via `git mv` (history follows). No directory flattenings — the single-file
directories found (`contracts/`, `docs/decisions/`, `docs/agents/roles/`,
`docs/operator/briefs/`) are conventional or load-bearing (see §7).

| old path | new path | reason | sha |
|---|---|---|---|
| `ORIGINAL_REQUEST.md` | `docs/_archive/requests/ORIGINAL_REQUEST_2026-05-29.md` | Historical request log for completed dogfood work; only referrers were in removed `.agents/`. | `605aeb2` |
| `STRIATUM_ARCHITECTURE_REVIEW_ANTIGRAVITY_2026-05-20.md` | `docs/_archive/reviews/internal/…` | Superseded interim review; all concerns (dual Py/Go, SQLite, Py PTY) resolved at HEAD. | `605aeb2` |
| `STRIATUM_ARCHITECTURE_REVIEW_TEAMWORK_PREVIEW_2026-05-29.md` | `docs/_archive/reviews/internal/…` | Recent review artifact; root is non-canonical (`docs/_archive/reviews/external/` already exists). Preserved, not deleted, since it is still substantively current. | `605aeb2` |

## 5. Done: doc updates and merges

None executed this pass. Two stale-doc rewrites were identified but deferred
(§6) because mapping retired `src/striatum/...` Python paths to their correct
`go/pkg/...` equivalents is judgment work better done with the maintainer's eye,
and the prompt caps a single doc-fix finding at ~50 net lines.

## 6. Deferred: needs maintainer decision

- **The rest of `src/`** (~70 files: TS/React frontend, legacy web assets, vite
  build output, frontend config) — *stop condition: would break the build.* The
  Makefile (`ui-check-bundle`, lines 71–77) runs `npm ci/install/audit
  --prefix src/striatum/web/frontend`, and `.github/workflows/ci.yml` references
  `src/striatum/web/frontend/package-lock.json`. The audit's strong read is that
  RFC 0092's Go-native server-rendered SSE UI superseded this React frontend and
  the daemon embeds only a minimal Go `app.js`/`base.css` — i.e. `src/` is dead —
  but removing it is **RFC 0078 Gate G** (a coordinated CI + Makefile change), not
  a hygiene delete. *Question:* close out RFC 0078 Gate G — delete `src/` and its
  `ui-*` Makefile/CI gates together?
- **`docs/reference/harness-friction-patterns.md`** — references retired
  `src/striatum/...` Python paths (`workflow_templates/catalog.json`,
  `workflow_generator/core.py`, `cli/daemon_rpc_route.py`) that have `go/pkg/...`
  equivalents. *Stop condition: ambiguous correct rewrite* — needs the exact Go
  path mapping confirmed. *Question:* are these tables worth updating, or is the
  doc itself historical and better banner-marked?
- **`docs/reference/prd.md`** — retains "D018: implement v1 in Python" framing
  without a historical banner. Low-risk to add a one-line RFC-0078 closure banner;
  deferred only because it's editorial framing, not a factual error.

## 7. Verified clean

- **`contracts/daemon_methods.json`** — single-file dir, but load-bearing:
  `go:generate routergen` (go/pkg/rpc/registry.go) + `docs/index.md` source it.
  Keep.
- **`scripts/`** — 10 shell scripts, several invoked by Makefile (`smoke`,
  `release-archives`, `package-smoke`). Keep.
- **`docs/_archive/`** — intentional history, named in `AGENTS.md`. Keep.
- **Single-file dirs** `docs/decisions/`, `docs/agents/roles/`,
  `docs/operator/briefs/` — conventional / load-bearing. Keep.
- **Tooling caches** (`.venv`, `.mypy_cache`, `.pytest_cache`, `.ruff_cache`,
  `build/`, `dist/`, `go/bin/`, `.scratch/`, `.gemini/`) — already gitignored,
  nothing tracked. Clean.
- **`skills/optional/adhd/`** — kept; it is the divergent-ideation skill the
  maintainer actively uses.

## 8. Follow-ups

- **Close RFC 0078 Gate G**: delete `src/` wholesale together with its `ui-*`
  Makefile targets and the frontend CI step, once confirmed the Go SSE UI is the
  only shipped web surface. This removes ~70 files and the last Python/Node
  remnants. (The single most valuable remaining cruft removal — gated only on the
  coordinated build change.)
- **Doc accuracy sweep**: a focused pass over `docs/explanation/` and
  `docs/reference/` for residual `src/striatum/...` path references now that the
  binary + planning docs are gone.
- **`.gitignore` already hardened** for `.agents/` this pass; consider whether any
  other agent-tool scratch dirs (e.g. `.codex/`) warrant the same.
