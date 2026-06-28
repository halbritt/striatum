---
type: record
status: working
feature_slug: MULTI_CAMPAIGN_SUPERVISION
source: adhd-level1-scout
author: operator-codex-gpt-5-003
created_at: 2026-06-28
updated_at: 2026-06-28
---

# MULTI_CAMPAIGN_SUPERVISION Level-1 Problem Brief

## Brief

Design a Striatum-native way for a human to accept an arc-level plan across
many RFC proposals, then let a constrained meta-agent coordinate design, build,
verify, and discovered-slice work until explicit stop conditions.

The reframe from the ADHD pass is: this is less a scheduling problem than an
authority-and-proof transfer problem. The design must make every continuation,
deferral, and fresh-context restart admissible from durable evidence rather than
from live chat momentum.

## Inputs

- Live-human Level-0 scope:
  `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/MULTI_CAMPAIGN_SUPERVISION_DESIGN_SCOPE_PRD.md`
- Repository recon:
  `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/MULTI_CAMPAIGN_SUPERVISION_REPOSITORY_RECON.md`
- Workflow selection:
  `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/MULTI_CAMPAIGN_SUPERVISION_WORKFLOW_SELECTION.md`
- Ideation brief:
  `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/MULTI_CAMPAIGN_SUPERVISION_IDEATION_BRIEF.md`
- Design process plan:
  `docs/operator/artifacts/multi-campaign-supervision-design-bootstrap/MULTI_CAMPAIGN_SUPERVISION_DESIGN_PROCESS_PLAN.md`

## Constraints

- Daemon-owned PostgreSQL remains authoritative live state.
- Repository artifacts are durable provenance, not the live message bus.
- A meta-agent cannot bypass daemon state transitions or silently repair failed
  runs.
- Local-first ticketing must be investigated before GitHub issues are treated
  as the coordination substrate.
- Fresh context per major stage is a hard human requirement.
- Deferrals must be explicit, justified, inspectable, and revisitable.
- Level 1 stops before implementation, build tickets, daemon schema, route, UI,
  or command changes.

## Design Question

What durable coordination shape lets a human accept a bounded campaign arc once,
lets a meta-agent coordinate ordinary Striatum workflows inside that accepted
authority, and still proves that every fresh context, deferral, stop condition,
and done claim is backed by daemon state plus durable provenance?

