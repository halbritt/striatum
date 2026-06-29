# RFC 0050 — HTTP/SSE MCP fixture

Striatum dogfood fixture for implementing
[RFC 0130](../../docs/rfcs/0130-go-daemon-http-sse-mcp.md): native
HTTP/SSE MCP server in the Go `striatumd` daemon.

Current source note: RFC 0050 Phase A-C and the follow-on agentloop/Python-MCP
cleanup have landed on `main`. This fixture preserves the original dogfood
shape and is useful for replay, audit, or regression-design exercises.

**Action covered:** action 1 of the operator brief —
"Implement the HTTP/SSE MCP server in the Go daemon as per RFC 0050."

**Original out of scope for this fixture run:**
- Agentloop PTY refactor (action 2).
- `src/striatum/mcp.py` deletion (action 3).

## Shape

Three parallel design lanes (codex / claude_code / gemini) → synthesis
→ ergonomics_dx design review → implementer (codex) → three parallel
build reviews (threat_model / ergonomics_dx / devils_advocate).

`max_active_jobs: 3` lets design and build-review fan out simultaneously.

## How to run

Before starting the workflow, fill
[`../../prompts/OPERATOR_INITIALIZATION_PROMPT.md`](../../prompts/OPERATOR_INITIALIZATION_PROMPT.md)
with this fixture's scope:

- Operator assignment and active scope: RFC 0050 Phase A-C / action 1.
- Hard product boundaries: local-only Go daemon, daemon-owned PostgreSQL,
  capability-token authorization, no hosted services, no telemetry, no
  repo-local SQLite authority.
- Original out of scope: agentloop PTY refactor, deleting `src/striatum/mcp.py`,
  full CLI retirement.
- Definition of done: daemon MCP initialize, `tools/list`, one read-only
  `tools/call`, one low-risk mutating `tools/call`, denial tests, docs update,
  and reviewer-ready verification commands.
- Lane policy: Codex as operator/implementer, Claude/Opus-family ergonomics
  verifier, Gemini-family adversarial scanner unless the human overrides.

```bash
striatum --repo /path/to/striatum workflow validate examples/rfc-0050-http-sse-mcp/workflow.json
striatum --repo /path/to/striatum run prepare --workflow examples/rfc-0050-http-sse-mcp/workflow.json --json
striatum --repo /path/to/striatum branch confirm --run-id <id> --create
striatum --repo /path/to/striatum run start --run-id <id> --json
```

Watch with `striatum dashboard --run-id <id>`.

## Artifact layout

```
docs/rfc-0050/
├── design/
│   ├── codex/DESIGN.md
│   ├── claude_code/DESIGN.md
│   └── gemini/DESIGN.md
├── DESIGN_SYNTHESIS.md
├── review/
│   ├── design/REVIEW.md
│   └── build/
│       ├── codex/REVIEW.md
│       ├── claude_code/REVIEW.md
│       └── gemini/REVIEW.md
└── build/HANDOFF.md
```
