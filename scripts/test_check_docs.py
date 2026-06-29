#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import io
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path


SCRIPT = Path(__file__).with_name("check_docs.py")
SPEC = importlib.util.spec_from_file_location("check_docs", SCRIPT)
assert SPEC is not None
check_docs = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(check_docs)


class CheckDocsTest(unittest.TestCase):
    def write(self, root: Path, rel: str, body: str) -> Path:
        path = root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(body, encoding="utf-8")
        return path

    def test_local_markdown_link_behavior_is_unchanged(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.write(root, "docs/target.md", "# Target\n")
            self.write(
                root,
                "docs/source.md",
                "[ok](target.md)\n[missing](missing.md)\n[external](https://example.test/x)\n",
            )

            errors = check_docs.check_links(root)

        self.assertEqual(errors, ["docs/source.md:2: broken link -> missing.md"])

    def test_include_ignored_audits_skipped_sources(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.write(root, ".check-docs-ignore", "docs/frozen/\n")
            self.write(root, "docs/frozen/source.md", "[missing](missing.md)\n")

            default_errors = check_docs.check_links(root)
            audit_errors = check_docs.check_links(root, include_ignored=True)

        self.assertEqual(default_errors, [])
        self.assertEqual(audit_errors, ["docs/frozen/source.md:1: broken link -> missing.md"])

    def test_striatum_uri_links_resolve_through_explicit_cached_index(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.write(root, "docs/target.md", "# Target\n")
            self.write(
                root,
                "docs/source.md",
                "[artifact](striatum://artifact/art_ok)\n"
                "[run](striatum://run/run_ok)\n"
                "[local](target.md)\n",
            )
            index_path = root / "striatum-uri-index.json"
            index_path.write_text(
                json.dumps(
                    {
                        "artifacts": [{"artifact_id": "art_ok"}],
                        "runs": ["run_ok"],
                    }
                ),
                encoding="utf-8",
            )
            uris, source = check_docs.load_striatum_uri_index(index_path)

            errors = check_docs.check_links(root, uris, source)

        self.assertEqual(errors, [])

    def test_cli_accepts_root_relative_striatum_uri_index(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.write(root, "docs/source.md", "[artifact](striatum://artifact/art_ok)\n")
            self.write(root, "index.json", json.dumps({"uris": ["striatum://artifact/art_ok"]}))

            with redirect_stdout(io.StringIO()):
                rc = check_docs.main(["--root", str(root), "--striatum-uri-index", "index.json"])

        self.assertEqual(rc, 0)

    def test_unresolved_striatum_uri_names_uri_and_resolution_source(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.write(root, "docs/source.md", "[missing](striatum://run/run_missing)\n")
            index_path = root / "striatum-uri-index.json"
            index_path.write_text(json.dumps({"runs": ["run_ok"]}), encoding="utf-8")
            uris, source = check_docs.load_striatum_uri_index(index_path)

            errors = check_docs.check_links(root, uris, source)

        self.assertEqual(len(errors), 1)
        self.assertIn("striatum://run/run_missing", errors[0])
        self.assertIn(f"cached index {index_path}", errors[0])

    def test_striatum_uri_without_index_fails_with_source_hint(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.write(root, "docs/source.md", "[artifact](striatum://artifact/art_missing)\n")

            errors = check_docs.check_links(root)

        self.assertEqual(len(errors), 1)
        self.assertIn("striatum://artifact/art_missing", errors[0])
        self.assertIn("no Striatum URI index", errors[0])

    def test_generated_operator_body_hygiene_flags_new_bodies_only(self) -> None:
        errors = check_docs.check_generated_operator_body_hygiene(
            Path("/unused"),
            "origin/main",
            added_paths=[
                "docs/operator/artifacts/new-run/final/SUMMARY.md",
                "docs/operator/artifacts/new-run/final/RUN_DOCKET.md",
                "docs/rfcs/0172-example.md",
                "docs/operator/BRIEF.md",
            ],
        )

        self.assertEqual(
            errors,
            [
                "docs/operator/artifacts/new-run/final/SUMMARY.md: newly tracked generated "
                "operator body should use blob+docket or an explicit docket/pointer manifest "
                "(source: git additions since origin/main)"
            ],
        )


if __name__ == "__main__":
    unittest.main()
