# Contributing to striatum

striatum is a small repo. This file is the front door for human
contributors. The quick path:

```bash
git clone https://github.com/halbritt/striatum
cd striatum
make install          # builds and installs Go binaries, then scaffolds daemon config
make lint             # Go lint via the root Makefile
make typecheck        # Go type/test gate via the root Makefile
make test             # go test
make smoke            # fresh-clone/package smoke
```

Every PR must keep `lint`, `typecheck`, and `test` green. The project is
Go-only; the root Makefile delegates to `go/` for `striatum`, `striatumd`,
and `striatum-supervisor-helper`. CI and release checks also exercise the Go
package smoke and fresh-clone smoke scripts.

striatum is local-first orchestration software. Do not add
hosted-service dependencies, telemetry, transcript capture, or
external persistence without an explicit product decision (see
[`docs/decisions/decision-log.md`](docs/decisions/decision-log.md)).

## Where to put what

The doc system has explicit boundaries — see
[`docs/DOC_MAP.md`](docs/reference/doc-map.md). Briefly:

- Behavior changes edit [`docs/reference/spec.md`](docs/reference/spec.md) and add a
  one-sentence-per-cell row to
  [`docs/decisions/decision-log.md`](docs/decisions/decision-log.md).
- New concepts add a glossary entry to
  [`docs/reference/ubiquitous-language.md`](docs/reference/ubiquitous-language.md)
  *first*, validator + introspection second
  (see [`docs/DDD.md`](docs/reference/domain-driven-design.md) § "Adding to the model").
- Significant design changes go through an RFC under
  [`docs/rfcs/`](docs/rfcs/) before implementation.
- README is first-contact material capped at 250 lines by a
  test; per-feature detail belongs under `docs/`.

## Sending a PR

1. Branch from `main`.
2. Keep the PR scoped to one concern. Big features land via an
   RFC + dogfood run; small fixes don't need that ceremony.
3. Update the relevant doc in the same PR — for behavior
   changes, that means `SPEC.md` + `CHANGELOG.md` + (if it's an
   accepted RFC's V1) `DECISION_LOG.md`.
4. Add or update tests for behavior changes.
5. Don't commit `.striatum/`, caches, build outputs, transcripts, or private
   diagnostics.
6. Push the branch; open the PR against `main`.

## Working as an agent contributor

If you are an LLM agent (Claude Code, Codex, Gemini CLI, …)
working on this codebase, read [`AGENTS.md`](AGENTS.md). It
points at [`docs/how-to/how-to-agent.md`](docs/how-to/how-to-agent.md) for
how to drive striatum *as a runner inside a target repo* —
that's a different role from contributing to striatum's source.

## Releases

Releases are tagged on `main` with `vX.Y.Z` and shipped as Go binary archives
through [`.github/workflows/release.yml`](.github/workflows/release.yml).
See [`docs/how-to/releasing.md`](docs/how-to/releasing.md) for the full
release policy.

To cut a release:

```bash
# 1. Bump VERSION.
# 2. Promote `## Unreleased` to `## vX.Y.Z — YYYY-MM-DD` in CHANGELOG.md.
# 3. Run make release-check.
# 4. Commit on main. The release workflow rejects a tag that does not
#    match VERSION.
git commit -am "vX.Y.Z: <one-line summary>"
git push origin main

# 5. Tag and push the tag. The Release workflow fires on the tag push.
git tag -a vX.Y.Z -m "vX.Y.Z: <one-line summary>"
git push origin vX.Y.Z
```

The release workflow:

1. Verifies the tag's version matches the root `VERSION` file.
2. Builds Linux/macOS Go archives and `SHA256SUMS`.
3. Checks the archives and runs the Go package smoke.
4. Creates a GitHub Release for the tag with the matching CHANGELOG slice and
   archive files attached.

## Versioning policy

- Major: breaking product or packaging transitions.
- Minor: new behavior or meaningful operator-visible changes.
- Patch: fixes to an already-tagged release.

The root `VERSION` file is the single version source for release builds.

## License

Apache-2.0. See [`LICENSE`](LICENSE). Unless noted otherwise,
contributions are licensed under the Apache License, Version 2.0.
