# Agent Guidance

This directory is the single-purpose home for repo-local guidance aimed at AI
coding agents working on Striatum. Use it to align agent behavior with the
project's vocabulary, context expectations, tracker conventions, and
role-specific instructions before doing work.

Product behavior belongs in `../reference/`; these files should point there
instead of restating the product contract.

- `domain.md` explains which domain and decision docs agents should consult.
- `context-hygiene.md` covers context-window and handoff discipline.
- `issue-tracker.md` records the repo's issue-tracker operating convention.
- `triage-labels.md` defines label vocabulary used during triage.
- `roles/` contains reusable role-specific agent instructions.
