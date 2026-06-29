# Engram Developer Request: RFC 0044 Phase 1 Engram-side Implementation

**Audience:** Engram operator AI / developer working in `<engram-repo>`
**Status:** Striatum-side V1 already landed (Striatum v1.35.0, dogfood-046)
**Current-context note:** This is an external Engram follow-up handoff.
Striatum corpus export was introduced in v1.35.0; see the current
CHANGELOG and RFC 0057 before treating the bundle contract as final.
**Striatum reference:** [`docs/rfcs/0044-engram-phase-1-implementation-spec.md`](../rfcs/0044-engram-phase-1-implementation-spec.md) (in this repo) — read this whole file first
**Constraints:** Engram's `AGENTS.md` / `CLAUDE.md` is authoritative for changes in Engram's repo. Augmentation-not-dependency: Engram must keep working without Striatum, and Striatum must keep working without Engram.

## Context

Striatum is a local-first orchestrator for terminal-based AI coding agents. The Striatum-side corpus-export verb (`striatum corpus export --since <ref> --out <dir>`) emits a redacted JSONL bundle of Striatum's software-building corpus (RFCs, decisions, operator reports, run summaries, commits, audit-chain rows, etc.) in a format designed for Engram to ingest.

The Engram half of the integration is **not implemented**. That's this request. Once it lands, an operator working in Striatum can run `striatum corpus export ...` and then `engram ingest-striatum ...` to give Engram a memory of the Striatum codebase's own evolution. The MCP server you build then lets future Striatum-operator AIs query that memory as an optional memory layer.

## What Striatum already provides

Run `striatum corpus export --since <ref> --out <dir>` on the Striatum repo. It writes nine JSONL files plus `manifest.json` to `<dir>`:

| File | Contains |
|------|----------|
| `commits.jsonl` | Each Striatum commit since `<ref>`, with redacted co-author emails and 64-char token scrubbing |
| `rfcs.jsonl` | Each `docs/rfcs/*.md` |
| `decisions.jsonl` | Each `docs/DECISION_LOG.md` row + each `docs/dogfood/*/decisions/*.md` |
| `operator_reports.jsonl` | Each `docs/dogfood/*/OPERATOR_REPORT.md` |
| `run_summaries.jsonl` | Output of `striatum run summary` for each landed run |
| `audit_chain.jsonl` | Audit-chain entries (run events) |
| `handoffs.jsonl` | Each `*/build/HANDOFF.md` and `*/build/*/HANDOFF.md` |
| `phase_notes.jsonl` | Each `docs/dogfood/*/PHASE_1_OPERATOR_NOTES.md` |
| `manifest.json` | Bundle metadata: `bundle_sha256`, per-file row counts, `since` ref, `generated_at` |

**Replay-stable:** Two `--since X --out Y` runs over the same commit range produce byte-identical JSONLs (other than `generated_at` in the manifest).

**Redaction:** `.env`, `.env.local`, `.striatum/`, SQLite migration tombstones, `transcripts/*`, `raw_model_output/*`, `keys/private.pem`, and similar paths are denylisted. Commit messages have co-author emails and 64-char tokens scrubbed.

**Augmentation boundary regression-pinned:** Striatum has zero `import engram` / `from engram` / `memory.*` references; `pyproject.toml` is Engram-free; a regression test (`tests/test_cli_corpus_export.py::test_no_engram_imports_or_memory_capabilities_in_striatum`) keeps it that way.

## What to build on the Engram side

All paths below are inside `<engram-repo>`.

### 1. New `source_kind='striatum'` in the raw evidence enum

Engram already has `source_kind` enum values `chatgpt`, `claude`, `gemini`, `obsidian`, `capture`, `future` per `<engram-repo>/migrations/001_raw_evidence.sql` (and follow-up migrations 003/005 added `claude`/`gemini`). Add migration `00X_source_kind_striatum.sql` that extends the enum with `'striatum'`. Same shape as the existing migrations.

### 2. New `corpus_id` column on raw_evidence + claims (or wherever corpus separation lives)

Currently Engram has a single implicit personal-life corpus. Add a `corpus_id` column (text, default `'personal'`) that distinguishes the **personal** corpus from the new **striatum** corpus. Backfill all existing rows to `'personal'`. New `striatum` ingests write `corpus_id='striatum'`. Same approach as RFC 0044 §3 in the Striatum repo — read it for the exact framing.

### 3. `engram ingest-striatum` CLI verb

Add a CLI subcommand to Engram's existing CLI:

```
engram ingest-striatum --bundle <dir> [--repo <name>]
```

- Reads `<dir>/manifest.json`, validates `bundle_sha256` against the file hashes.
- Reads each of the nine JSONL files and inserts into `raw_evidence` (or wherever Engram puts incoming material) with `source_kind='striatum'`, `corpus_id='striatum'`, and a `bundle_id` so re-ingesting a newer bundle replaces or upserts cleanly.
- `--repo <name>` is the human label (e.g. `striatum`, `kayak-gen`) so future bundles from other Striatum-orchestrated repos don't collide.
- Idempotent: re-running with the same `bundle_sha256` should be a no-op.
- Augmentation boundary: Engram must NOT call back into Striatum. The ingester reads files from disk only.

### 4. `engram-mcp-stdio` standalone MCP server

A new binary/entry-point at (suggestion) `<engram-repo>/agent-runner/engram-mcp-stdio` that speaks MCP over stdio (loopback-only, zero outbound network) and exposes four **read-only** retrieval tools:

| Tool | Args | Returns |
|------|------|---------|
| `engram.search` | `{query: str, corpus: str = "striatum", limit: int = 10}` | Top-k matching rows (mix of commits / rfcs / decisions / operator reports / etc.) with source attribution |
| `engram.fetch_reference` | `{reference_id: str}` | Full row for a search result (e.g. full RFC body, full operator report) |
| `engram.describe_corpus` | `{corpus: str}` | Corpus metadata: row counts per source_kind, latest ingest timestamp, available repos |
| `engram.health` | `{}` | Server health: DB reachable, last ingest, schema version |

**Capability vocabulary** (Engram-local, NOT shared with Striatum's RFC 0030 set):
- `memory.read_striatum` — read the `striatum` corpus
- `memory.describe` — metadata-only access
- `memory.read_personal` — read the `personal` corpus (NOT in default Striatum operator token)
- `memory.read_cross_corpus` — cross-corpus retrieval (NOT in default Striatum operator token)

Operator tokens that an Engram session creates for a Striatum-operator AI carry only `memory.read_striatum` + `memory.describe` by default.

### 5. Augmentation-not-dependency boundary (enforce on Engram side too)

Engram must continue to function with no Striatum present. Two acceptance checks:

- `engram ingest-striatum` refuses cleanly with named exit code if `<bundle-dir>` is missing or `manifest.json` doesn't validate — does NOT crash or call into Striatum.
- The `engram-mcp-stdio` server boots and answers `engram.health` with `corpus=personal` only when the `striatum` corpus is empty. No striatum-specific error spam.

Pin the boundary with an Engram-side regression test analogous to Striatum's: `grep -r 'import striatum\|from striatum' src/` returns nothing; Engram's `pyproject.toml` does not depend on `striatum-orchestrator`.

## Acceptance criteria

1. Migration `00X_source_kind_striatum.sql` adds the enum value cleanly on Engram's existing dev DB.
2. `engram ingest-striatum --bundle <dir>` ingests Striatum's bundle without errors; re-running is a no-op.
3. `engram describe-corpus striatum` shows non-zero rows for the 9 source kinds after a successful ingest.
4. `engram-mcp-stdio` boots, exposes the 4 tools, and `engram.search '{"query": "RFC 0043"}'` returns at least one row from `rfcs.jsonl` plus related decision rows.
5. Capability vocabulary is documented and the default Striatum-operator token grants only `memory.read_striatum` + `memory.describe`.
6. Engram-side augmentation boundary regression test added and green.

## How to test against real Striatum data

```bash
# In Striatum repo
cd <striatum-repo>
.venv/bin/striatum corpus export --since v1.30.0 --out /tmp/striatum-bundle

# In Engram repo
cd <engram-repo>
./run-migrations.sh   # or however you apply migrations
.venv/bin/engram ingest-striatum --bundle /tmp/striatum-bundle --repo striatum
.venv/bin/engram describe-corpus striatum
.venv/bin/engram-mcp-stdio   # in a separate terminal
# from MCP client: tools/call engram.search '{"query":"codex/codex anti-pattern"}'
```

That last query should return rows from D095/D096/D097/D098/D100 (decision artifacts under `docs/dogfood/04*/decisions/`) and operator reports describing the anti-pattern.

## Out of scope (defer)

- Write-side dogfood→claim flow (RFC 0041 Phase 3 / RFC 0044 future)
- Personal-corpus re-attack (RFC 0041 Phase 4)
- Cross-repo retrieval across multiple Striatum-orchestrated targets — design lands when the first second target appears
- Engram's existing claims/beliefs/ingestion/segmentation pipelines — Phase 1 is read-only retrieval over raw_evidence; the claim/belief overlay arrives in a later phase
- Hosted mode (network-accessible Engram MCP server). Phase 1 is loopback-only.

## Reference checklist

When done, the Engram-side handoff should include:

- `migrations/00X_source_kind_striatum.sql`
- `src/engram/cli/ingest_striatum.py` (or wherever the verb lives)
- `agent-runner/engram-mcp-stdio` (or equivalent entry point) + 4 read-only tools
- New `corpus_id` column + backfill migration
- Engram-side `tests/test_augmentation_boundary.py` regression test
- Updates to Engram's `docs/SPEC.md` / `README.md` mentioning the Striatum corpus + the four tools
- A demo script or short README walkthrough showing the ingest + MCP query cycle

## Provenance

Everything Striatum-side that motivates this request:

- RFC: `docs/rfcs/0044-engram-phase-1-implementation-spec.md`
- Striatum corpus export code: `src/striatum/corpus/`
- Striatum CLI verb: `src/striatum/cli/parser.py` (look for `corpus_export`)
- Striatum dogfood that produced the spec: `docs/dogfood/042/track_b/`
- Striatum dogfood that produced the V1 corpus exporter: `docs/dogfood/046/`
- D094 decision (Postgres-sole-substrate, which the corpus exporter respects)

Read `docs/rfcs/0044-engram-phase-1-implementation-spec.md` in full before starting — this request is the operator brief; the RFC is the spec.
