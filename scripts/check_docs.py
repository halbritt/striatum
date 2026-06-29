#!/usr/bin/env python3
"""Guard against stale docs by checking mechanically-detectable rot.

Default repo-agnostic, low-false-positive checks:

1. Markdown link integrity: every inline ``[text](target)`` / ``![alt](target)``
   whose target is a repo-relative path must resolve to a file or directory that
   exists inside this repo. URLs, ``mailto:``, anchors, globbed paths, and any
   target that resolves outside the repo root are out of scope (the last avoids
   false positives on links into sibling checkouts).

2. Version consistency: if ``scripts/check_release_version.py`` exists, run it so
   VERSION / README / CHANGELOG stay in lockstep.

Optional Striatum-specific checks:

- Validate ``striatum://artifact/...`` and ``striatum://run/...`` Markdown links
  through an explicit cached JSON index, without daemon access.
- Reject newly added generated operator bodies when a base ref is supplied; new
  bodies should use blob storage plus a docket or pointer manifest.

Semantic staleness — a status doc or board that contradicts the actual state of
the repo — is a human/agent responsibility per AGENTS.md, not something this
guard can detect reliably; cross-repo decision/commit references make automated
ID resolution unsafe here. This catches broken links and version drift only.

Adapted from engram's ``scripts/check_artifact_refs.py`` (reference-integrity
idea), generalized to plain markdown links.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

LINK_RE = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
STRIATUM_URI_PREFIXES = ("striatum://artifact/", "striatum://run/")
SKIP_PREFIXES = ("http://", "https://", "mailto:", "tel:", "file:", "data:", "#", "//")
SKIP_DIRS = frozenset(
    {".git", "__pycache__", ".venv", "node_modules", ".pytest_cache", ".ruff_cache", ".striatum"}
)
IGNORE_FILE = ".check-docs-ignore"
GENERATED_OPERATOR_BODY_PREFIXES = (
    "docs/operator/artifacts/",
    "docs/records/audits/",
    "docs/dogfood/",
    "dogfoods/",
)
GENERATED_OPERATOR_BODY_ALLOWED_NAMES = {
    "DOCKET.md",
    "POINTER_MANIFEST.md",
    "POINTER_MANIFEST.json",
    "RECORD_DOCKET.md",
    "RUN_DOCKET.md",
}


def ignore_prefixes(root: Path) -> tuple[str, ...]:
    """Repo-relative path prefixes to skip, from an optional ``.check-docs-ignore``.

    Lets a repo exclude frozen provenance (accepted RFCs, archives, review
    records) whose outbound links are point-in-time and must not be rewritten.
    """
    ignore = root / IGNORE_FILE
    if not ignore.is_file():
        return ()
    prefixes = []
    for line in ignore.read_text(encoding="utf-8").splitlines():
        entry = line.strip()
        if entry and not entry.startswith("#"):
            prefixes.append(entry.rstrip("/") + "/")
    return tuple(prefixes)


def tracked_markdown(root: Path, skip: tuple[str, ...]) -> list[Path]:
    """Return tracked ``*.md`` files, falling back to a filtered walk."""
    try:
        out = subprocess.run(
            ["git", "-C", str(root), "ls-files"],
            capture_output=True,
            text=True,
            check=True,
        ).stdout
        rels = [line for line in out.splitlines() if line.endswith(".md")]
        if rels:
            return sorted({root / rel for rel in rels if not rel.startswith(skip)})
    except (subprocess.CalledProcessError, FileNotFoundError):
        pass
    out_paths = []
    for path in root.rglob("*.md"):
        rel = path.relative_to(root)
        if any(part in SKIP_DIRS for part in rel.parts):
            continue
        if str(rel).startswith(skip):
            continue
        out_paths.append(path)
    return sorted(out_paths)


def link_destination(raw: str) -> str:
    """Return the URL/path destination token from a markdown link target."""
    text = raw.strip()
    if text.startswith("<") and ">" in text:
        text = text[1 : text.index(">")]
    else:
        text = text.split()[0] if text.split() else ""
    return text


def normalize_striatum_uri(text: str) -> str:
    """Return the stable lookup form for a supported ``striatum://`` URI."""
    return text.split("#", 1)[0].split("?", 1)[0]


def supported_striatum_uri(raw: str) -> str | None:
    """Return a supported Striatum virtual-record URI, or None."""
    text = normalize_striatum_uri(link_destination(raw))
    if text.startswith(STRIATUM_URI_PREFIXES):
        return text
    return None


def link_target(raw: str) -> str | None:
    """Return a checkable repo-relative path from a markdown link, or None."""
    text = link_destination(raw)
    if not text or text.startswith(SKIP_PREFIXES) or text.startswith(("~", "/")):
        return None
    if text.startswith("striatum://"):
        return None
    if "*" in text:
        return None
    text = text.split("#", 1)[0].split("?", 1)[0]
    return text or None


def collect_striatum_uris(value: object) -> set[str]:
    """Collect supported ``striatum://`` URIs from a cached index object.

    The index reader accepts a deliberately small set of common export shapes:
    direct URI strings, ``{"uris": [...]}``, ``{"artifacts": [...]}``, and
    ``{"runs": [...]}`` with either IDs or objects carrying ``*_id`` / ``uri``.
    """
    uris: set[str] = set()

    def add_uri(uri: str) -> None:
        normalized = normalize_striatum_uri(uri)
        if normalized.startswith(STRIATUM_URI_PREFIXES):
            uris.add(normalized)

    def add_entity_ids(entity_value: object, kind: str) -> None:
        prefix = f"striatum://{kind}/"
        if isinstance(entity_value, str):
            add_uri(entity_value if entity_value.startswith("striatum://") else prefix + entity_value)
        elif isinstance(entity_value, list):
            for item in entity_value:
                add_entity_ids(item, kind)
        elif isinstance(entity_value, dict):
            for key, item in entity_value.items():
                if isinstance(key, str) and isinstance(item, dict):
                    if key not in {"id", "artifact_id", "run_id", "uri", "items"}:
                        add_uri(prefix + key)
                collect(item)

    def collect(node: object) -> None:
        if isinstance(node, str):
            add_uri(node)
        elif isinstance(node, list):
            for item in node:
                collect(item)
        elif isinstance(node, dict):
            if isinstance(node.get("uri"), str):
                add_uri(node["uri"])
            if isinstance(node.get("artifact_id"), str):
                add_uri("striatum://artifact/" + node["artifact_id"])
            if isinstance(node.get("run_id"), str):
                add_uri("striatum://run/" + node["run_id"])
            if "artifacts" in node:
                add_entity_ids(node["artifacts"], "artifact")
            if "artifact_ids" in node:
                add_entity_ids(node["artifact_ids"], "artifact")
            if "runs" in node:
                add_entity_ids(node["runs"], "run")
            if "run_ids" in node:
                add_entity_ids(node["run_ids"], "run")
            for item in node.values():
                collect(item)

    collect(value)
    return uris


def load_striatum_uri_index(path: Path) -> tuple[frozenset[str], str]:
    """Load an explicit cached Striatum URI index file."""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except OSError as exc:
        raise ValueError(f"cannot read Striatum URI index {path}: {exc}") from exc
    except json.JSONDecodeError as exc:
        raise ValueError(f"cannot parse Striatum URI index {path}: {exc}") from exc
    return frozenset(collect_striatum_uris(data)), f"cached index {path}"


def check_links(
    root: Path,
    striatum_index: frozenset[str] | None = None,
    striatum_source: str | None = None,
    include_ignored: bool = False,
) -> list[str]:
    errors: list[str] = []
    skip = () if include_ignored else ignore_prefixes(root)
    source = striatum_source or "no Striatum URI index (--striatum-uri-index not provided)"
    for md in tracked_markdown(root, skip):
        try:
            lines = md.read_text(encoding="utf-8").splitlines()
        except (OSError, UnicodeDecodeError):
            continue
        for lineno, line in enumerate(lines, start=1):
            for match in LINK_RE.finditer(line):
                striatum_uri = supported_striatum_uri(match.group(1))
                if striatum_uri is not None:
                    if striatum_index is None or striatum_uri not in striatum_index:
                        errors.append(
                            f"{md.relative_to(root)}:{lineno}: unresolved striatum URI -> "
                            f"{striatum_uri} (source: {source})"
                        )
                    continue
                target = link_target(match.group(1))
                if target is None:
                    continue
                dest = (md.parent / target).resolve()
                try:
                    dest.relative_to(root)
                except ValueError:
                    continue  # outside the repo root — out of scope
                if not dest.exists():
                    errors.append(f"{md.relative_to(root)}:{lineno}: broken link -> {target}")
    return errors


def check_version(root: Path) -> list[str]:
    script = root / "scripts" / "check_release_version.py"
    if not script.exists():
        return []
    proc = subprocess.run([sys.executable, str(script)], capture_output=True, text=True)
    if proc.returncode != 0:
        return [f"version consistency failed: {(proc.stdout + proc.stderr).strip()}"]
    return []


def added_paths_since(root: Path, base_ref: str) -> list[str]:
    proc = subprocess.run(
        ["git", "-C", str(root), "diff", "--name-only", "--diff-filter=A", f"{base_ref}...HEAD"],
        capture_output=True,
        text=True,
        check=True,
    )
    return [line.strip() for line in proc.stdout.splitlines() if line.strip()]


def generated_operator_body_path(rel: str) -> bool:
    normalized = rel.replace("\\", "/")
    if not normalized.endswith(".md"):
        return False
    if Path(normalized).name in GENERATED_OPERATOR_BODY_ALLOWED_NAMES:
        return False
    return normalized.startswith(GENERATED_OPERATOR_BODY_PREFIXES)


def check_generated_operator_body_hygiene(
    root: Path, base_ref: str, added_paths: list[str] | None = None
) -> list[str]:
    source = f"git additions since {base_ref}"
    try:
        paths = added_paths if added_paths is not None else added_paths_since(root, base_ref)
    except (subprocess.CalledProcessError, FileNotFoundError) as exc:
        return [f"generated-operator-body hygiene failed: cannot read {source}: {exc}"]
    return [
        f"{rel}: newly tracked generated operator body should use blob+docket "
        f"or an explicit docket/pointer manifest (source: {source})"
        for rel in paths
        if generated_operator_body_path(rel)
    ]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Check docs for broken local references.")
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument(
        "--striatum-uri-index",
        type=Path,
        help="Explicit cached JSON index for striatum://artifact/... and striatum://run/... links.",
    )
    parser.add_argument(
        "--hygiene-base-ref",
        help="Optional base ref for rejecting newly tracked generated operator bodies.",
    )
    parser.add_argument(
        "--include-ignored",
        action="store_true",
        help=f"Audit links in sources normally skipped by {IGNORE_FILE}.",
    )
    args = parser.parse_args(argv)
    root = args.root.resolve()

    striatum_index = None
    striatum_source = None
    errors: list[str] = []
    if args.striatum_uri_index is not None:
        index_path = args.striatum_uri_index
        if not index_path.is_absolute():
            index_path = root / index_path
        try:
            striatum_index, striatum_source = load_striatum_uri_index(index_path)
        except ValueError as exc:
            errors.append(str(exc))

    errors.extend(check_links(root, striatum_index, striatum_source, args.include_ignored))
    errors.extend(check_version(root))
    if args.hygiene_base_ref:
        errors.extend(check_generated_operator_body_hygiene(root, args.hygiene_base_ref))
    for error in errors:
        print(f"[FAIL] {error}")
    if errors:
        print(f"\ncheck-docs: {len(errors)} problem(s)")
        return 1
    print("check-docs: OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
