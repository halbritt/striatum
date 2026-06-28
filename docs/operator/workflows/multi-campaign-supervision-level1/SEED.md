---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: level1-divergent-ideation-seed
author: operator-codex-gpt-5-003
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION Level-1 Seed

## Purpose

This workflow starts fresh-context Level-1 design ideation for
`MULTI_CAMPAIGN_SUPERVISION`. It exists to widen the design space before any
architecture is accepted.

The Level-0 bootstrap is complete. Do not redo the Stage 1 human interview and
do not recreate the Level-0 artifact set unless the human changes scope.

## Current State At Seed

- Repository: `/home/halbritt/git/striatum`
- Source head when this seed was written: `6ef11e89`
- Bootstrap command run first: `./go/bin/striatum operator bootstrap --markdown`
- Bootstrap/doctor state: daemon reachable and authorized; `doctor ok=true`;
  zero active runs, open blockers, claimable jobs, and human checkpoints
- Level-1 setup mode: scaffolded Striatum-native `divergent_ideation` plus an
  operator ADHD scout pass

## Problem

Design a process and product shape for supervising many Striatum development
arcs at once. The human wants to accept an arc-level plan across RFC proposals,
then let a constrained meta-agent coordinate design, build, verifier, and newly
discovered slice work until explicit stop conditions. Every major stage must be
able to start in a fresh context window using durable artifacts, tickets, and
handoff payloads rather than live chat history.

## Required Coverage

Each candidate family must address:

- durable coordination unit: campaign, RFC arc, slice, ticket, workflow group,
  run set, or another concept
- local-first ticketing substrate and whether GitHub issues are only tracker
  mirrors
- UI/dashboard surface for portfolio status, blockers, stop pressure, and
  evidence handles
- meta-agent authority after arc acceptance
- fresh-context handoff payloads and restart proof
- deferral accountability for discovered slices
- completion proof across daemon state, workflow artifacts, Git state, docs,
  tickets or issues, and verifier receipts
- stop conditions and human-confirmed boundaries

## Hard Boundaries

- Do not implement product code.
- Do not add daemon methods, route maps, schema tables, UI routes, or build
  tickets.
- Do not choose a final architecture during the divergence pass.
- Do not make a dashboard or ticket artifact the live workflow state machine.
- Do not bypass daemon MCP/RPC authority.
- Do not depend on hosted services, external persistence, durable transcript
  capture, GitHub PR merge flow, or provider-specific cloud APIs.

## ADHD Scout Pass

Before this scaffold was written, an explicit ADHD pass ran in isolated
sub-agents under five frames: regulator, logistics, competitor trying to break
it, remove the load-bearing assumption, and 10-year-old. That pass is recorded
in:

- `docs/operator/artifacts/multi-campaign-supervision-level1/PROBLEM_BRIEF.md`
- `docs/operator/artifacts/multi-campaign-supervision-level1/DIVERGENCE_LEDGER.md`
- `docs/operator/artifacts/multi-campaign-supervision-level1/IDEATION_SYNTHESIS.md`

Treat those files as seed provenance, not as accepted architecture.

## How To Run This Workflow

Before running, refresh live state:

```bash
./go/bin/striatum operator bootstrap --markdown
./go/bin/striatum status --json --run-limit 0
./go/bin/striatum workflow validate docs/operator/workflows/multi-campaign-supervision-level1/workflow.json --json
```

Stop if `doctor` is red, if the primary checkout is dirty in unrelated files,
or if any proposed work drifts into implementation before design review.

The generated workflow uses `striatum codex
--dangerously-bypass-approvals-and-sandbox --no-alt-screen` lanes over
`pty_helper` with `worktree_isolation: "per_job"`. It is intended for a fresh
Striatum run after the operator confirms the branch and lane cost.
