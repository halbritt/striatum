# Adjudicate The Ratification Gate

Read only the dialogue trajectory for this run: the holder case and the two
falsifier challenges. Publish the collaboration ledger verdict at the
expected path. Follow the `collaboration_ledger` V1 front-matter schema; the
publisher refuses invalid front matter. Include the exact lowercase `author:`
byline.

For each falsifier objection, rule it:

- `binding` — load-bearing and unrebutted; it becomes a binding constraint
  the RFC text must discharge (tighten the non-goals, the registry naming,
  the eviction list, or the scaffold-time-only guarantee).
- `rebutted` — the case or the falsifier's own rebuttal answers it.
- `out_of_scope` — it asks RFC 0119 to do something it does not propose.

Then render the gate verdict:

- **Clear** when every binding constraint is dischargeable by amending the
  RFC without weakening a corpus invariant or the durable-provenance boundary.
- `needs_revision` when the dialogue lacks substance — falsifier objections
  are not load-bearing or rebuttals were never engaged — so the challenge
  round repeats once.
- **Refuse** (recommend the operator not accept) when a binding constraint
  shows RFC 0119 breaks a corpus invariant, makes a state transition depend
  on memory, or erodes durable provenance, and cannot be discharged by
  amending the RFC.
