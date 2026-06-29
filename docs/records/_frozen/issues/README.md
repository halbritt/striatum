# Frozen GH-Issue-Driven Workflows

Status: frozen provenance. This directory archives early GitHub-issue-driven
workflow packets. Each numbered subdirectory captures the specs, prompts, roles,
handoffs, reviews, and workflow fixtures used to close or investigate one
historical issue. These files are not the current issue tracker or operator
queue; use current Plane work items, `docs/operator/`, RFCs, and decision records
for live work.

Lightweight workflows for closing individual GitHub issues. Sister to
`docs/dogfood/` in the historical record, and to the named fixtures now kept in
`docs/dogfoods/`.

## When This Shape Was Used

Use the issue-packet shape when the work was bounded by a single GitHub issue
with a clear "Definition of done" or "Expected outcome" section.

Use the dogfood shape when the work is a phase of an RFC, requires synthesis from
multiple inputs, or spans more than one deliverable file/module that needs
coordinated review.

| Property | Issue packet | Dogfood fixture |
|---|---|---|
| Spec source | GH issue body | RFC + design synthesis |
| Jobs | 3 (triage -> fix -> verify) | 6 (synth -> review_design -> implement -> 3-way build review) |
| Verifier count | 1, or 3 for high-severity work | 3 (codex threat_model + claude ergonomics_dx + gemini adversarial) |
| Synth phase | No; issue body is the spec | Yes; `DESIGN_SYNTHESIS.md` |
| Branch name | `striatum/gh-<N>-<slug>` | `striatum/dogfood-<N>-<slug>` |
| Typical duration | 1-3 hours | 4-12 hours |

## Historical Job Shape

1. **triage** reads the GH issue body plus linked code, docs, and prompts, then
   produces `SCOPE.md` naming files to change and acceptance checks derived from
   the issue's "Definition of done", "Expected outcome", or "Required behavior"
   sections.
2. **fix** implements the change. The spec is the GH issue body plus `SCOPE.md`.
   The output is `HANDOFF.md` citing each closed definition-of-done bullet.
3. **verify** is a fresh-session reviewer that reads only the GH issue body and
   changed files, then records whether each definition-of-done bullet is closed.
   The output is `REVIEW.md` with verdict `accept`, `accept_with_findings`, or
   `needs_revision`.

`review_posture: compliance_license` scoped reviewer findings to license,
attribution, telemetry, hosted-service, data-handling, regulatory, and
external-persistence risks. It did not restrict evidence: the reviewer still
read the implementer handoff, changed files named by the handoff, relevant
tests, and command outputs needed to verify the issue.

For high-severity issues such as security, data loss, or run-state corruption,
the verifier fanned out to 3 lanes: codex threat_model, claude ergonomics_dx,
and gemini adversarial.

## Historical Lane Selection

| Issue type | Suggested triage | Suggested fix | Suggested verify |
|---|---|---|---|
| Documentation / prompt | claude_code | codex | claude_code (fresh) |
| Bug fix (Python) | codex | codex | claude_code |
| Bug fix (Go / shell / wrapper) | claude_code | claude_code | codex (fresh) |
| Web UI ergonomics | claude_code | codex | gemini + claude (2-way) |
| Security (high) | codex | codex | 3-way (codex/claude/gemini) |
| Test gap | claude_code | codex | claude_code |

The historical guidance avoided same-lane triage/fix/verify, especially the
codex/codex anti-pattern recorded by D095-D098.

## Archived Packets

- [16/](16/) — Add complete operator initialization prompt
  ([gh#16](https://github.com/halbritt/striatum/issues/16))
- [22/](22/) — daemon migration path requires owner role but has no
  `--admin-url` ([gh#22](https://github.com/halbritt/striatum/issues/22))
- [23/](23/) — daemon status reads `striatumd.pid` but no code path writes it
  ([gh#23](https://github.com/halbritt/striatum/issues/23))
- [24/](24/) — supervise send `packet_id` discoverability and release requeue
  blocked ([gh#24](https://github.com/halbritt/striatum/issues/24))
- [25/](25/) — repo list without `--json` gives misleading
  `repo_not_migrated` error ([gh#25](https://github.com/halbritt/striatum/issues/25))
- [26/](26/) — RFC 0073 blob diagnostics through `striatum daemon doctor`
  ([gh#26](https://github.com/halbritt/striatum/issues/26))
- [27/](27/) — `artifacts_no_update` trigger should allow blob-column updates
  ([gh#27](https://github.com/halbritt/striatum/issues/27))
- [28/](28/) — `review_posture: compliance_license` evidence-scope regression
  ([gh#28](https://github.com/halbritt/striatum/issues/28))
- [30/](30/) — No operator recovery path for stale-leased repo-write jobs
  ([gh#30](https://github.com/halbritt/striatum/issues/30))
- [32/](32/) — skills install does not copy supervised wrappers to consumer
  repositories ([gh#32](https://github.com/halbritt/striatum/issues/32))
- [33/](33/) — Concurrent supervise start RPCs can deadlock in Postgres
  ([gh#33](https://github.com/halbritt/striatum/issues/33))
- [34/](34/) — Write-scope escape from repo-write `allowed_paths`
  ([gh#34](https://github.com/halbritt/striatum/issues/34))
- [35/](35/) — Zombie supervised lanes block `supervise.stop`
  ([gh#35](https://github.com/halbritt/striatum/issues/35))
- [36/](36/) — Gemini review jobs complete without verdict and need override
  recovery ([gh#36](https://github.com/halbritt/striatum/issues/36))
- [38/](38/) — RFC 0075 MCP liveness timestamps and deadline classifications
  ([gh#38](https://github.com/halbritt/striatum/issues/38))
- [40/](40/) — RFC 0075 tmux attach metadata and status surfaces
  ([gh#40](https://github.com/halbritt/striatum/issues/40))
- [43/](43/) — status treats `accept_with_findings` as non-accepting
  ([gh#43](https://github.com/halbritt/striatum/issues/43))
- [44/](44/) — workflow details reports `repo_write_without_worktree_isolation`
  on serial issue workflows ([gh#44](https://github.com/halbritt/striatum/issues/44))
