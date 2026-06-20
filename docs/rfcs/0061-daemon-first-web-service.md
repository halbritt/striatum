# RFC 0061: Daemon-First Web Service

## Status
implemented (residual: optional polish) — the load-bearing daemon-first
web/service boundary shipped: daemon-backed read/mutation routes, `/v1/invoke`
RPC routing, and deleted legacy SQLite service helpers (see `go/pkg/webservice/`).
The residual is continued service-layer modularity cleanup, made moot by the
Python-runtime retirement (RFC 0078). Currency-promoted in D245 (2026-06-20,
RSA-007) after a verifying grep found no live registry/Python fallback in error
paths.

## Summary
The production local web service is moving behind daemon DTO/RPC boundaries.
Read and mutation routes that have landed use daemon-backed APIs; legacy
SQLite service helpers are quarantined as fixture fallback only. `service.py`
continues to shrink into route wrappers while domain shaping moves into
`striatum.web.*`, `striatum.service_*`, and daemon handlers.

## Motivation
The service had become a large mixed authority surface with direct SQLite
fallbacks, rendering code, DTO shaping, and HTTP plumbing in one file. D094
requires the production service to honor daemon-owned PostgreSQL as live state.

## Proposed Implementation
Landed slices include daemon-backed doctor, run/event/artifact/API paths,
daemon-routed run mutations, raw artifact and workflow-browser splits, request
security/io helpers, route dispatch extraction, and service lifecycle
extraction. The only residual is optional polish — continued service-layer
decomposition and stricter gating of leftover fixture-only seams — and even
that is largely moot now that the Python runtime (and its `service.py`/SQLite
fallback seams) has been retired under RFC 0078. The load-bearing daemon-first
boundary itself has shipped.
