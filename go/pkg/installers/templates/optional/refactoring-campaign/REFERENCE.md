# Refactoring Campaign — Reference

Companion to [SKILL.md](SKILL.md). Everything here is striatum-version
sensitive; when a claim disagrees with `docs/reference/cli-reference.md`
or the generated `striatum-*` skills, those win.

## Lanes

Default binding (override per campaign when the user asks):

| Lane role | Model | Notes |
|---|---|---|
| Author / synthesis / arbitration | claude | `["claude", "--dangerously-skip-permissions"]` — a bare `claude` parks on the MCP permission prompt |
| Panel proposer / reviewer B | codex | `["codex", "--yolo"]` |
| Panel proposer / reviewer C | agy | supported tier; keep `.gemini/` excluded from write scopes |

Lane shape for every real lane (agent-loop, interrogation-capable):

```json
{
  "adapter": "process",
  "command": ["claude", "--dangerously-skip-permissions"],
  "adapter_capabilities": {"agent_loop": true},
  "supervision": {"transport": "pty_helper", "compatible": true, "require_tmux": true},
  "capabilities": ["write"]
}
```

- Never add one-shot flags (`--print`, `-p`, `codex exec`) — a fresh
  process per packet cannot claim work or be interrogated truthfully.
- Keep the stage-2 reviewer on a different model family from the author.
- Keep `worktree_isolation: "per_job"` on the stage-2 author lane.
- Daemon-spawned lanes only see `~/.local/bin` on PATH; if codex/agy live
  in `~/.npm-global/bin`, symlink them into `~/.local/bin` first.

## Per-stage drive recipe

For each stage, in order:

1. `striatum --repo <target> workflow validate --allow-same-model-pairing <workflow.json>`
2. `striatum --repo <target> run prepare --workflow <workflow.json>` and
   confirm the branch (`branch.mode: confirm` — `striatum branch confirm`).
3. `striatum --repo <target> run start --run-id <id>` — auto-drives the run by
   default (#212), so steps 4 below normally happen on their own; just wait for
   terminal (`scripts/wait-run.sh`). Pass `--no-drive` to own driving yourself.
4. `striatum --repo <target> run drive --run-id <id>` blocks until the run is
   terminal, registering/supervising one fresh session per role/lane as the DAG
   unblocks and closing terminal or superseded launched lanes before fresh
   reviewers. With auto-drive on, this explicit driver is **optional** — it is
   idempotent, so it composes safely with the background driver (it then just
   serves as a foreground terminal-state waiter).
   Use `--json` for machine-readable progress or `--once` when an external
   harness owns the polling. A `needs_revision` verdict still auto-spawns the
   next attempt through daemon state; do not fight it with manual session loops.
5. Read the stage's terminal artifact from the repository tree (artifacts
   finalize to the repo) and apply the stop matrix.

## Stage 0 skip (operator-named goal)

When the user supplies the goal, author
`striatum/refactoring/<slug>/00-goal/GOAL_DECISION.md` directly instead of
running stage 0: decision V1 front matter (`owner: human`,
`outcome: accepted`), the goal as one named behavior-preserving structural
change, frozen surfaces, verification commands, and explicit non-goals.
Stage 1's prompts consume it identically either way.

## Stage 2 write-scope derivation

The instantiated `execute_slices` job ships placeholder
`allowed_paths: ["src/example/", ...]`. Before `run prepare`:

- Replace `src/example/` with the committed plan's blast-radius paths.
- Add path-shaped frozen surfaces (migrations, generated files, docs the
  plan freezes) to `forbidden_paths`.
- Frozen surfaces that are not path-shaped (exported signatures, CLI
  output, wire formats) cannot be write-scoped — the falsifiers and the
  preservation review enforce them; do not try to encode them as paths.
- Keep the step-ledger artifact path in `allowed_paths`.

Runs execute a frozen workflow snapshot: editing a workflow after
`run prepare` does nothing — prepare a fresh run instead.

## Driving discipline (hard-won)

- Trust only returned JSON; verify every state-changing call with a
  follow-up read. Never invent run/job/artifact ids.
- Make state-changing MCP calls sequentially; a failing parallel call
  cancels its siblings.
- Opaque MCP errors (`<error>method</error>`) → re-run the equivalent
  CLI verb to get the real message.
- Use blocking `work.await_packet`; never background a claim-poll and
  defer the ack — stalled-lease recovery will transfer the job and close
  your session.
- Long local work inside a supervised lane decays attestation → heartbeat
  (`work.heartbeat`) or publish rejects the role byline.
- Verdict intents are `accept` / `accept_with_findings` /
  `needs_revision` / `reject` (never "approve").
- After `make install`, the running daemon still executes the old image —
  `systemctl --user restart striatumd` before relying on a fix.
- Do not gate the campaign on human confirmations the workflow's own
  gates don't require; record decisions and continue. Report failures
  with the daemon's evidence, not optimism.

## Integrate

After an `accept` preservation review and the final report:

```sh
# serialized, gated merge of the run worktree — never hand-merge
striatum --repo <target> run integrate --run-id <stage-2-run-id>
```

`merge_conflict` never auto-resolves; surface it to the operator with the
conflicting paths. The campaign's provenance lives at
`striatum/refactoring/<slug>/` — goal decision, gated plan, ledgers,
final report — and travels with the repo.

If `run integrate` refuses with `capability_missing` (the token lacks the
`apply` capability), mint an apply-capable token and retry:

```sh
striatum daemon token-create --capability apply --display-name operator-apply --json
striatum --repo <target> --capability-token <token> run integrate --run-id <stage-2-run-id>
```

Only if you cannot obtain an apply token, fall back to a strict fast-forward
of the run branch — never a conflict resolution:

```sh
git merge --ff-only <run-branch>   # refuses unless strictly ahead; never hand-resolve
```

Record the manual fast-forward in the commit message. See
`docs/how-to/how-to-human.md` ("Mint a capability token").
