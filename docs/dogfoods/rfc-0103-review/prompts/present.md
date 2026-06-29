# Present RFC 0103 for review

You are a Claude lane driven by the Striatum runner. Read **RFC 0103 —
Self-Hosting Production Hardening** (provided as a context document; it also
lives at `docs/rfcs/0103-self-hosting-production-hardening.md`). Then write a
concise review brief that a two-reviewer panel will evaluate.

Write the file at the exact path in your work packet's `expected_artifacts`
(`docs/dogfoods/rfc-0103-review/artifacts/REVIEW_BRIEF.md`). Keep it ~30–50 lines.
Near the top include the lowercase byline `author: presenter-claude-opus-4.8-001`.

Cover, faithfully and without inflating:
1. **Thesis** — what gap RFC 0103 frames (between RFC 0097 self-hosting *proven
   once* and *production-grade*), in 2–3 sentences.
2. **The seven workstreams** — one line each (W1 lane sandbox; W2 adapter seats;
   W3 transport/daemon-churn survival; W4 interrogation-window liveness; W5
   artifact-contract legibility; W6 orchestration; W7 operator surface), naming
   the issues each owns.
3. **Dependency ordering** — W1 → (W2/W3/W4) → (W5/W6) → W7, and why.
4. **The three sharpest open questions / risks** a reviewer should probe (e.g.
   is the W-grouping a real partition of the 17 issues or a loose bucketing? is
   the umbrella acceptance measurable? does any workstream actually belong to a
   different RFC?).

Then publish the artifact and complete the job using your Striatum tools. Do not
advance state by printing phrases — use the daemon.
