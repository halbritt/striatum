# Context Hygiene

This doc is for anyone driving a coding agent against a target
repository — the AI operator opening a session, the human principal
reviewing an escalation, or the workflow author setting up a repo so
future sessions inherit a clean baseline.

The core observation: **the size of the context window is not the
determining factor of session quality.** Two sessions with the same
model, the same token budget, and the same nominal task can produce
wildly different output. The variable is curation, ordering, and
labeled negative space — not raw bytes.

## The asymmetry

A 200-line curated `AGENTS.md` outperforms a 5000-line wiki dump
every time. Quality of a context window is roughly:

- **Signal-to-noise.** Every token of irrelevant material crowds
  out something load-bearing.
- **Ordering.** A short, ordered reading list lets the agent build
  the right mental model on the first pass. An exhaustive index
  forces it to triage, which costs tokens *and* attention.
- **Labeled negative space.** Explicitly marking what is historical,
  reference-only, or off-distribution prevents the agent from
  silently drifting into it.

When sessions go bad, the failure is almost never "too small." It
is polluted (live and historical mixed without labels), unordered
(agent has to triage), or re-litigated (agent silently re-derives a
settled decision and proposes it as new).

## Repo-side practices

These are the things you put in the repo so every session inherits
a clean baseline. They are durable; you set them up once.

- **Short, ordered entry-point reading list.** Point `AGENTS.md`
  (or equivalent) at five to eight files in order. An exhaustive
  index is worse than a curated one because it forces the agent to
  triage.
- **A frozen decision log.** A `DECISION_LOG.md` means past calls
  are settled, not silently re-litigated. The worst sessions are
  ones where the agent re-derives an answer the team already
  converged on and proposes it as novel.
- **Shared vocabulary.** A `UBIQUITOUS_LANGUAGE.md` (or glossary)
  makes the cost of saying things zero. When the user says
  "stalled run waiting on human input," the agent does not have
  to translate.
- **Labeled negative space.** Mark historical, reference-only, and
  off-distribution material explicitly. `AGENTS.md` should call out
  which dirs and files are historical fixtures unless a current task
  says otherwise. Without the label, the agent reads them as live.
- **Visible cadence in git log.** Commit conventions
  (`Land RFC 0013 step 7`, `Add RFC 0020`) are self-documenting.
  The agent does not have to ask whether you use RFCs; it can see
  the convention in the last ten commits.
- **Clean working tree at session start.** No half-finished
  refactors, no orphaned files. The code is authoritative; the
  agent trusts what it reads.

## Session-side practices

These are the things you do as the human steering a single session.

- **Open with intent, not instruction.** "I'm staring at this
  thinking that half the magic is the DDD approach" outperforms
  "write me a 3000-word RFC with sections X, Y, Z." The first lets
  the agent connect intent to artifact and recognize *which*
  artifact is right; the second locks the lane before the
  recognition step.
- **One thread per session.** Piling unrelated work into the same
  context pollutes both. The high-taste sessions are usually the
  ones doing one coherent thing.
- **Do not recite what is already in the repo.** If
  `DECISION_LOG.md` says it, the agent can read it. Re-pasting eats
  the same context twice and signals "I do not trust the doc,"
  which makes the agent hedge.
- **Notice the moment to crystallize.** When a conversation has
  produced something durable — an architectural insight, an
  unaddressed concern, a pattern worth naming — say so explicitly
  ("ought to have an RFC that encodes that"). This is taste, not
  process. It is also the single most replicable habit.
- **Trust silence.** If the agent did not ask a clarifying
  question, it is because the context was sufficient. Forcing one
  ("any questions before you start?") often derails a session that
  was about to land cleanly.

## Private project memory

Private project memory is context available to one operator or local
environment but not shared through the repo, daemon state, durable
artifacts, or an explicit corpus export. It is useful for a person or
agent that has it, but it is not provenance and it is not a product
dependency.

Treat private memory as a convenience cache. If a fact matters to the
next operator, put it in a current repo-shared artifact: a decision row,
operator brief, RFC, TODO item, support ledger, remediation plan, or
source comment where appropriate. Do not rely on private memory to make
day-zero setup, recovery triage, or RFC status understandable.

This is distinct from repo-shared context. `AGENTS.md`,
`docs/operator/BRIEF.md`, `docs/DECISION_LOG.md`, and durable workflow
artifacts are visible to future sessions. Private memory is not.

## Model-side practices

These are the things the agent should not need to be told, but
often does.

- **Read the entry-point file first.** `AGENTS.md` framing is
  load-bearing; skipping it to "save time" is the single most
  common context self-injury.
- **Do not accumulate redundant tool output.** Re-reading the same
  file, re-running the same grep, dumping a 2000-line log when 20
  lines suffice — all of that crowds out the framing that drives
  quality.
- **Resist mid-session summarization.** Compressing at the wrong
  moment loses original framing. If the harness compresses, fine;
  if the agent volunteers a recap, it is usually displacing better
  tokens.
- **Stay inside the stated write scope.** Agents driving a
  striatum workflow have an explicit `write_scope.allowed_paths`.
  Outside a workflow, the equivalent is "do not write files the
  user did not ask for." A surprise file is a context pollutant
  for the next session.

## Failure modes

How contexts go bad, in rough order of frequency:

1. **Mixed live/historical material with no label.** Agent reads
   a deprecated doc as authoritative and propagates the old shape.
2. **Re-litigated decisions.** Agent re-derives something
   `DECISION_LOG.md` already settles, proposes it as novel, and the
   user has to manually re-frame.
3. **Over-specified prompt.** User locks the lane before the
   recognition step; agent produces a competent-but-flat artifact
   instead of the one the situation actually called for.
4. **Polluted session.** Two unrelated tasks share one context; the
   second inherits residue from the first.
5. **Tool-output bloat.** Repeated greps, redundant reads, large
   logs not trimmed before continuing.
6. **Forced clarifications.** User asks for questions; agent
   produces them out of compliance rather than need; the resulting
   framing is worse than the original.

## Replication checklist

If you are setting up a new repo so future sessions can replicate
the practices above:

- [ ] An `AGENTS.md` (or equivalent) at the repo root with a
      five- to eight-file ordered reading list.
- [ ] A `DECISION_LOG.md` with frozen architectural decisions.
- [ ] A `UBIQUITOUS_LANGUAGE.md` (or glossary) for project-specific
      terms.
- [ ] Explicit labels on any historical, reference, or
      off-distribution material.
- [ ] Commit-message convention visible in `git log`.
- [ ] Clean working tree at the start of each session.

If you are *driving* a session, the per-conversation practices
above are the playbook; there is no checklist that substitutes for
opening with intent and noticing the moment to crystallize.

## Related

- `AGENTS.md` — the entry-point reading list this doc describes.
- `docs/HOW_TO_AGENT.md` — what to do once a striatum workflow has
  handed you a work packet.
- `docs/HOW_TO_HUMAN.md` — human-principal escalation playbook, with
  operator-by-hand reference material.
