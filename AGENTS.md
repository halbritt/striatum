# Project Instructions

Striatum is a standalone, local-first workflow runner for terminal-based AI
coding agents. It is a generic orchestration tool for target repositories,
not an Engram-specific process script. The product boundary in `docs/reference/spec.md`
is the source of truth; if a doc claim disagrees with current source
behavior, fix the doc.

## Start Here

For normal AI-operator cold start, run `striatum operator bootstrap --markdown`
first and follow its `next_actions` and bounded `reading_plan`. Use
`--json` when another tool will consume the packet. For source changes,
bootstrap failure, or tasks that require deeper project archaeology, read these
first, in order:

1. `README.md`
2. `ARCHITECTURE.md` for the one-page substrate map
3. `docs/index.md`
4. `docs/reference/spec.md`
5. `docs/decisions/decision-log.md`
6. `docs/reference/ubiquitous-language.md`
7. `docs/reference/todo.md` (archived pointer — current work lives in
   `docs/operator/BRIEF.md`, `docs/operator/rfc-roadmap.md`, and the local
   `Striatum` Plane project)
8. `docs/operator/BRIEF.md` for current operator state and the bounded
   plan links that supersede older handoffs.
9. `docs/operator/rfc-roadmap.md` when the task is to advance the RFC
   backlog (a proposed, accepted-but-unbuilt, or partially-implemented RFC).
   It is the sequenced, themed "do the next one" roadmap: it defines the
   Design → Build → Verify ship path every RFC goes through and orders the
   open RFCs into waves (reliability spine first, features last). For "work
   the next RFC", take the lowest-numbered unshipped item whose blocker is
   clear. Each file's `Status:` line under `docs/rfcs/` remains the
   authoritative per-RFC truth.

Treat `docs/records/_frozen/ENGRAM_INCUBATION_CONTEXT.md`,
`examples/rfc-0014-operational-artifact-home/`, and the older P00x prompts as
historical/reference fixtures unless a current task explicitly asks you to
work on Engram dogfood history.

## Product Boundary

- Striatum's authoritative live state is the daemon-owned PostgreSQL
  instance (RFC 0033 + D094 / RFC 0043), scoped per registered target
  repository. `.striatum/` next to each target repo is operational
  scratch (PTY FIFOs, daemon-owned interactive lanes, pidfiles, the
  capability-token cache); the daemon is a hard prerequisite for every
  Striatum verb, and `--no-daemon` is retired. See `docs/how-to/postgres-transition.md`
  for the operator runbook and native PostgreSQL repository adoption.
- Repository files are durable provenance, not the live message bus.
- Marker files, tmux panes, terminal output, and provider hooks are not
  authoritative workflow state.
- Do not introduce hosted services, cloud APIs, telemetry, durable
  transcript capture/export, or external persistence without an explicit
  product decision. Operator-local PTY logs under `.striatum/scratch/`
  are private diagnostics only, not workflow state or durable provenance.
- Keep workflow examples generic unless they are clearly labeled as
  historical Engram reference fixtures.

## Working As A Striatum Agent

When you are running inside a striatum workflow (not just editing the repo),
the runner moves work through daemon MCP/RPC state transitions. Do not advance
state by printing phrases, scraping terminal output, or touching PostgreSQL
directly.

The workflow loop, work-packet shape, supervisor mode, decision artifacts,
front-matter rules, and stale-lease recovery instructions all live in
**[`docs/how-to/how-to-agent.md`](docs/how-to/how-to-agent.md)**. Read that doc — and the
RFC 0015 skill bundle when one is installed — before claiming work. The
short version:

- Use daemon MCP tools for live workflow control when an endpoint and
  capability token are available. Treat the CLI verbs supplied in each work
  packet's `commands` block as exact compatibility fallbacks and parameter
  references; if you must use CLI fallback, run them verbatim.
- Stay inside `write_scope.allowed_paths`. Never write to
  `forbidden_paths` or `.striatum/`.
- Match `expected_artifacts[].author_line` exactly when an artifact's title
  block includes `author:`.
- Front-matter–carrying artifacts (`decision`, `finding`, `findings_ledger`,
  `synthesis`, `support_ledger`, `action_item_ledger`,
  `harness_improvement_proposal`, `escalation`, `operator_brief`,
  `work_plan`, `progress_note`, `operator_report`) must validate
  against their V1 schema —
  the publisher refuses invalid front matter with exit code 6.
- Lease expiry is lazy. If MCP or CLI fallback refuses because a lease is
  stale, ask the operator to recover stale work through the local UI or daemon
  MCP recovery tools. CLI recovery verbs remain diagnostic/compatibility
  clients of the same daemon boundary.

`striatum dashboard --run-id <id>` is the compact terminal view for humans
watching a run; `--once` produces a single frame to stdout for scripts and
CI assertions.

## Development

Use the Makefile targets:

- `make install`
- `make lint`
- `make typecheck`
- `make test`
- `make smoke`

The project is Go-only: the root Makefile delegates to `go/` for `striatum`,
`striatumd`, and `striatum-supervisor-helper` binaries. The legacy Python
runtime, source, and tests have been retired and removed per RFC 0078.
Examples live under `examples`. Historical execution prompts live under
`prompts`.

## Change Discipline

- Keep changes aligned with `docs/reference/todo.md` and accepted decisions in
  `docs/decisions/decision-log.md`.
- Until generated daemon contracts land, new RPC methods or handwritten
  route maps must update `docs/reference/command-authority-matrix.md`
  and the authority guardrail tests.
- Update `docs/decisions/decision-log.md` for product or architecture decisions.
- Add or update tests for behavior changes.
- Prefer generic terms: target repository, workflow fixture, runner state,
  artifact, adapter, lane, session, work packet.
- Do not add new Engram-specific paths, branch names, prompt ordinals, or
  marker names to product docs or core code.
- New durable Markdown artifacts should use the lowercase privacy-safe
  byline: `author: <role-name>-<model-name>-<ordinal>`.
- Do not commit `.striatum/`, caches, transcripts, or private diagnostics.
- Avoid hardcoded home-directory absolute paths in tracked docs and
  fixtures; use repository-relative paths, environment variables, or
  generalized `~/` paths when a path shape matters.
- **Do not strand pushed branches.** Source reaches `main` two ways only: the
  daemon's run-integration for lane work (reviewed in-band by the workflow's
  reviewer lanes and verdicts), or a direct, sync-guarded commit to `main` for
  operator changes. **Not GitHub pull requests** — Plane is the issue tracker
  in the local/private `Proximal` workspace; GitHub is the repository remote
  only, never the merge mechanism; do not open PRs to land work. Completed work
  must not sit unmerged on a feature branch: integrate it promptly; if a blocker
  prevents it (e.g. missing push/merge credentials), record the concrete blocker
  rather than leaving the branch silently stranded.
- **Do not leave stale docs.** The product-boundary rule above ("if a doc claim
  disagrees with current source behavior, fix the doc") also covers state docs:
  when a change moves the state a doc or board describes, update it in the same
  change — `docs/reference/spec.md`, `docs/reference/todo.md`, the decision log,
  `CHANGELOG.md`, and `docs/operator/BRIEF.md`. When an RFC ships (or its status
  changes), also mark it in `docs/operator/rfc-roadmap.md` and re-triage the wave.
  `make check-docs` flags broken
  local doc links (frozen provenance under `docs/rfcs/`, `docs/records/_frozen/`, and
  similar is excluded via `.check-docs-ignore`). It currently passes; keep it
  green (it can be promoted into `make check` when the team is ready).
- **Do not paste over a broken runner.** The daemon's own state machine is the
  only legitimate way a lane's work reaches a run branch and then `main`. When a
  verb fails, a lane strands its edits in its per-job worktree, a run wedges, or
  `striatum doctor` reports integrity problems (`job_completed_without_anchor`,
  `worktree_head_unreachable`, `artifact_anchor_missing_file` /
  `artifact_anchor_hash_mismatch`, `artifact_blob_metadata_missing`), do **not**
  hand-finish the work — manual worktree capture, cherry-pick, or a direct
  hand-commit — and report it complete. That lands the deliverable while leaving
  the defect recorded as a success, and it corrupts the daemon-owned provenance
  the product depends on. Route recovery back through the daemon instead
  (`recovery requeue-stale`, `recovery resume`, `recovery complete-stalled`,
  `checkpoint resolve`, or the matching MCP recovery methods); if it cannot be
  cleanly recovered, the run is exposing a real runner defect — **surface it**:
  file or update a Plane work item, record the friction in the operator report
  and `docs/operator/BRIEF.md`, and fix the runner (or escalate) before continuing. A
  red `doctor` is a stop-and-fix condition, not a thing to route around: do not
  launch or continue dogfoods on top of accumulating integrity problems.
- **Keep the shared checkout clean and current — no dirty trees, no stale
  branches, no merge hell.** A registered target repository's primary checkout
  (this repo at its canonical path included) is shared by the operator, by AFK
  lanes, and by other concurrent agents. Treat it as a clean view of
  `origin/main`, never a scratchpad. The four rules below are one policy:
  - *Sync before you touch source.* `git fetch`, then fast-forward `main` to
    `origin/main` (or rebase your branch onto freshly-fetched `origin/main`)
    before editing. Never start work on a `main` that is behind `origin/main`;
    a checkout that silently drifts dozens of commits behind is merge hell
    waiting to surface as a conflict.
  - *Isolate concurrent work.* Make every change on a short-lived feature
    branch, and whenever another agent or lane may hold the primary checkout,
    work in an isolated `git worktree` cut from freshly-fetched `origin/main`.
    Do not co-edit the shared tree: uncommitted work there gets swept,
    clobbered, or superseded by the next agent, and two agents editing one tree
    is how merge hell starts. Confirm no other session has its cwd in the tree
    (e.g. another agent's `git status`/open edits) before a `reset`, `stash`,
    `clean`, or fast-forward that rewrites files under them; if one does,
    snapshot first (below) and surface it rather than sweeping their work.
  - *End every turn clean.* Commit and push your branch, or revert it; never
    hand the next session uncommitted changes, untracked deliverables, or a
    behind-by-N `main`. If you must pause mid-change — or must clear someone
    else's dirty tree — snapshot the full state (tracked + untracked) to a
    named branch (e.g. `backup/<topic>-<YYYY-MM-DD>`), not the working tree,
    and say so, so nothing is destroyed and everything is recoverable.
  - *Reports are not repository litter.* Audits, triage dumps, reconcile
    notes, and other scratch output belong outside the tracked tree (or under
    an ignored scratch path), never as untracked `*.md` files at the repo root
    (see "Do not commit `.striatum/`, caches, transcripts, or private
    diagnostics" above). Integrated work must also reach `main` and have its
    branch deleted without lingering — a stranded branch is a dirty tree
    deferred (see "Do not strand pushed branches" above).

## Historical Prompts

The P001-P004 prompts are retained as incubation provenance. They may
mention Engram, old branch names, or `--repo ..` command shapes. Do not
execute them as current standalone instructions without first rewriting
them for the standalone repository and the intended target repository.

<!-- BEGIN PROXIMAL PLANE TRACKING -->
## Plane Tracking

This repository is represented in the local/private Plane workspace `Proximal`.

- Plane project: `Striatum` (`STRIATUM`)
- Issue tracker: Plane (`Proximal` workspace), project `Striatum` (`STRIATUM`).
- Plane URL: `https://proximal.tail0ecc2e.ts.net:10000/`
- GitHub repo: `https://github.com/halbritt/striatum`
- GitHub Issues: deprecated; use Plane work items for new issue tracking, claims, reviews, and issue-state changes.
- Use Plane work items for multi-agent planning, claims, submitted artifacts, reviews, and acceptance decisions.
- When updating Plane, include the repo, branch/worktree, `run_id`, `base_sha`, artifact links, verification evidence, and authority scope in the work item description or comments.
- Do not commit Plane API tokens. Local tokens and MCP env files live outside git under `~/.config/plane/`.
<!-- END PROXIMAL PLANE TRACKING -->


## Branch hygiene

Do not leave unmerged code lying around. If a task uses a branch, merge its authorized work into the intended target branch before reporting completion. If merge authority is absent, report that as a blocker instead of treating the branch as finished. Clean up branches and associated worktrees after merge.
