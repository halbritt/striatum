# Doc Map: which doc owns what

This is the boundary contract for striatum's documentation set. It
exists because the same content kept appearing in two places (RFC
prose absorbed into SPEC, decision rows growing into RFC reference
material, README duplicating SPEC paragraphs) and readers couldn't
tell which copy was canonical.

The rule is: **one home per concept, every other doc cites it.**
When two docs disagree, the home wins.

## Per-doc rules

### `docs/reference/prd.md` — product requirements

- **What it is:** the original product framing.
- **What it owns:** the user-visible problem, the V1 scope, the
  non-goals.
- **What it doesn't:** implementation surface, schema details,
  CLI shape.
- **When it changes:** rarely. The PRD is treated as a frozen
  artifact; product re-scoping happens via a new RFC plus a
  decision-log row, not a PRD edit.

### `docs/reference/spec.md` — implementation contract

- **What it is:** what the runner accepts, refuses, and emits
  *today*. The contract a first-time reader of `src/` should be
  able to verify against.
- **What it owns:** state-store schema, workflow validator
  rules, CLI surface, exit codes, recovery semantics, adapter
  boundaries.
- **What it doesn't:** *why* a contract was chosen (that's the
  RFC); the history of how it got there (that's the decision
  log).
- **When it changes:** every time runtime behavior changes.
  Sections are *edited in place* — they don't grow per-RFC
  paragraphs at the bottom. The heading stays stable; the body
  reflects current behavior.
- **Rule of thumb:** if you find yourself adding `(RFC NNNN
  V*)` to a SPEC heading, you're appending instead of editing.
  Edit the existing section, cite the RFC inline (e.g. `> Design
  rationale: [RFC 0020](../rfcs/0020-autonomous-stalled-run-recovery.md).`),
  and drop the suffix.

### `docs/rfcs/<NNNN>-*.md` — design proposals

- **What it is:** a *design* document, written before
  implementation lands.
- **What it owns:** the problem statement, the rationale, the
  non-goals, the implementation path, the open questions, the
  test plan.
- **What it doesn't:** post-acceptance reference material. An
  accepted RFC is a frozen artifact — it records *why we made
  this choice at this date*. It does not grow into a
  feature manual.
- **When it changes:** only the status field after acceptance.
  Body edits happen during the proposed → accepted window;
  after that the RFC is historical.
- **Rule of thumb:** if a reader needs current behavior, point
  them at SPEC. If they need rationale, point them at the RFC.
  If both, link both.

### `docs/decisions/decision-log.md` — receipts

- **What it is:** one row per accepted RFC (or non-RFC
  decision). The receipt that says "we made this call, here's
  the one-line why, here's where to look for detail."
- **Row shape:** four cells, **one to two sentences each**. No
  walls of text.
  - **Decision:** what flipped. Verb + object.
  - **Reason:** the binding constraint.
  - **Consequences:** observable surface change + version bump.
    Cite the RFC and the dogfood BUILD_HANDOFF for the full
    surface, e.g. `See [RFC 0020](../rfcs/0020-autonomous-stalled-run-recovery.md)
    and dogfood-014.`
  - **Revisit trigger:** when the decision should be revisited.
- **What it doesn't:** test names, function signatures,
  per-file change lists, multi-paragraph rationale. Those live
  in the RFC and the BUILD_HANDOFF.
- **Rule of thumb:** a decision-log row is the receipt, not the
  invoice. If a row is over ~150 words, the detail belongs in
  the RFC.

### `docs/reference/ubiquitous-language.md` — glossary

- **What it is:** the authoritative term list.
- **What it owns:** every striatum-specific noun (run, session,
  lease, work packet, lane, role, posture, byline, …) and its
  one-paragraph definition.
- **What it doesn't:** anything that isn't a term. No procedure
  text, no schema details — just definitions.
- **When it changes:** every time a new RFC introduces a
  concept. Per RFC 0019 / `domain-driven-design.md`, glossary changes come *first*,
  then validator + introspection.

### `docs/explanation/domain-driven-design.md` — framing

- **What it is:** the domain-driven framing the codebase
  already has, written down.
- **What it owns:** the bounded context, the aggregate-roots
  table, the value-object list, the events-log explanation, the
  daemon-method write-boundary invariant, the "Adding to the
  model" pattern future RFCs cite.
- **What it doesn't:** any current-behavior detail (that's
  SPEC), any historical decision (that's the log), any glossary
  definitions (that's `ubiquitous-language.md`).

### `CHANGELOG.md` — release notes

- **What it is:** one entry per shipped version.
- **What it owns:** what changed user-visibly per release.
- **Rule of thumb:** a CHANGELOG entry is for someone who reads
  it after an installation to understand what they
  just got. It's not a PR description.

### `README.md` — first contact

- **What it is:** the GitHub front door.
- **What it owns:** the elevator pitch, install, two quick
  starts (human / coding agent), and pointers into `docs/`.
- **What it doesn't:** behavior model paragraphs, sequential
  walkthroughs, dogfood history, per-RFC subsections, command
  reference. Per RFC 0017, those moved out and a test enforces
  the README line budget so they don't drift back.

### `docs/how-to/how-to-human.md`, `docs/how-to/how-to-agent.md`, `docs/tutorials/getting-started.md`, `docs/how-to/writing-workflows.md`, `docs/reference/cli-reference.md`

- **What they are:** operator-facing playbooks (RFC 0017).
- **What they own:** the verb sequences a human or agent runs,
  in the order they run them.
- **Rule of thumb:** these documents *use* the SPEC's
  contracts; they don't *redefine* them. If the verb shape
  changes, edit SPEC and the playbook in the same PR.

### `docs/how-to/daemon-runbook.md` — daemon operability runbook

- **What it is:** the RFC 0079 operator reference for the `striatumd`
  lifecycle: `striatum daemon install/uninstall/status`, the portable
  systemd user unit, runtime layout, the `daemon.toml` DSN, logs, and
  troubleshooting.
- **What it owns:** how to install/start/stop the daemon and where its
  runtime files live (`daemon-go.sock`, `client-token`,
  `mcp-http-endpoint`, pidfile).
- **What it doesn't:** Postgres role/grant provisioning (that's
  `postgres-transition.md`) or the workflow verb sequences (`how-to-*`).
  `GETTING_STARTED.md` links here for the lifecycle rather than
  duplicating it.

### `docs/reference/workflow-types.md` — workflow selection guide

- **What it is:** the current operator-facing map of workflow
  families and lane-set choices: what each type is for, what graph
  shape it has, which starter or example is closest, which lane shapes
  fit common runs, and what "default workflow" means today.
- **What it owns:** workflow and lane selection guidance,
  starter-vs-example language, and the roadmap from current examples
  to a future template chooser.
- **What it doesn't:** validator rules (SPEC), verb sequences
  (`how-to/*`), or historical rationale for the browser/editor surface
  (RFC 0024).

### `docs/dogfood/<id>/BUILD_HANDOFF.md` — implementer notes

- **What it is:** the implementer's notes from a specific
  dogfood run.
- **What it owns:** the file-by-file change list, the test
  names, the smoke-test results, the reviewer-facing notes.
- **When it changes:** never, after the run completes. It's the
  primary source for "what actually shipped in this RFC's V1."

### `docs/index.md` — pointer index

- **What it is:** a one-line summary of every doc.
- **When it changes:** when a new doc is added.

## Direction of citation

Cross-references go in one direction:

- README → `docs/`
- `docs/how-to/*` → SPEC
- SPEC → RFC (for rationale only)
- RFC → SPEC (for "after acceptance, the contract lives in
  SPEC")
- decision log → RFC + dogfood
- DDD framing → ubiquitous language

If you find yourself writing a back-edge (SPEC explains an RFC's
*reasoning*; an RFC describes *current behavior*; the
decision-log row tells you *what tests were written*), you're
crossing a boundary. Stop, find the right home, and put a link
there instead.

## Enforcement

A small invariant test in `tests/test_doc_links.py` asserts that
no decision-log row exceeds 200 words. Other rules in this map
are not mechanically enforced; the test guards the loudest
failure mode (D-row wall-of-text) and the rest is review
discipline.
