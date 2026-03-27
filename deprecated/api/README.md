# Deprecated: API Direct-Call Implementation

This directory contains the original HTTP API direct-call implementation, deprecated in favor of the browser automation approach.

## Reason for Deprecation

The API approach hits Bilibili's cumulative rate-limit threshold on large-scale tasks (600+ authors), causing all subsequent requests to fail with risk-control errors (`-352`). This is not a recoverable rate-limit — once triggered, the IP/Cookie is blocked for an extended period.

## Files

- `crawler/main.go` — Original CLI entry point (hardcoded API initialization)
- `benchmark/main.go` — Benchmark tool for API approach
- `httpclient/client.go` — HTTP client with retry/backoff (API-specific)
- `bilibili/` — Bilibili API implementations (search, author, wbi signing, types)

## Note

These files are kept for reference only. They are **not compiled** and **not imported** by the active codebase.
