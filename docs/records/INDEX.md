---
type: record
status: frozen
owner: halbritt
expires: null
---

# Records index (striatum)

`docs/records/` is the single home for write-once machine exhaust under the
[documentation convention](../reference/doc-convention.md). Curated docs live in
the Diataxis quadrants (`tutorials/`, `how-to/`, `reference/`, `explanation/`);
this tree holds the time-ordered, write-once tail.

| Kind | Path | Files | What |
|---|---|---|---|
| Frozen | [`_frozen/`](_frozen/) | 277 tracked files / 236 Markdown files | Historical per-issue / per-review / design archive (was `docs/_archive/`). |
| Audits | [`audits/`](audits/) | 20 tracked Markdown files | Dated whole-repo audits & retrospectives (`STRIATUM_<TASK>_<MODEL>_<DATE>.md`, previously loose at the repo root). |

## Not folded here (intentional)

`docs/operator/` and `docs/campaigns/` are **not** in `docs/records/`. They are a
**runtime contract**, not relocatable exhaust: RFC 0058 defines `docs/operator/`
as the canonical operator tree the daemon reads at runtime (`BRIEF.md`, `plans/`,
`progress/`, `briefs/`), ~18 Go files and tests resolve those paths, and many
accepted decisions cite them as frozen provenance. Relocating them is an
operator-subsystem change that belongs in its own RFC — or, more simply, the
convention should sanction `docs/operator/` as a first-class operational region
in place. They are left where they are by design.
