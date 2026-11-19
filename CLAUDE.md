# Personal Finance Tracker

A Go CLI for tracking income, expenses, and liabilities in HKD.

## Attribution

Never add Claude or AI attribution to commits, pull requests, code comments, or any
generated message. No `Co-Authored-By: Claude`, no "Generated with Claude Code", no
footer of any kind.

## Commands

```bash
go build ./...
go test ./...
go vet ./...
gofmt -w .
```

## Layout

- `internal/models` — domain models, validation, and typed errors.
- `internal/parser` — CSV and single-record ingestion into `models.TransactionEvent`.
- `internal/store` — SQLite persistence via `database/sql` and `sqlx`.
- `internal/rules` — rule implementations.
- `cmd` — Cobra commands.

## Domain rules

- Amounts use `github.com/govalues/decimal` only; never floats.
- The only currency is HKD, with at most 2 decimal places.
- Amounts are always positive; the event `type` defines direction.
- Timestamps are UTC.
- Double entry: each transaction has exactly two entries, one SOURCE and one
  DESTINATION, with equal amounts.

## Code style

- Do not add unnecessary or redundant comments. The code must be clear from reading it.
- Only comment non-obvious intent, invariants, or workarounds.

## Tests

- Do not add tests unless explicitly requested.
- When requested, keep them table-driven and beside the code under test.
