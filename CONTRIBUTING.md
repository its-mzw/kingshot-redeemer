# Contributing

Contributions are welcome. This document covers the basics.

## Getting started

```bash
git clone https://github.com/mzw/kingshot-redeemer
cd kingshot-redeemer
go test ./...
```

Requirements: Go 1.24+ (no CGO needed — SQLite is pure Go).

## Making changes

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Add or update tests to cover your change
4. Run `go test ./...` and `go vet ./...` — both must pass
5. Open a pull request

## Code style

- Standard Go formatting (`gofmt`)
- Error messages lowercase, no trailing punctuation
- Wrap external errors with `fmt.Errorf("context: %w", err)`
- No new dependencies without prior discussion

## Reporting bugs

Open an issue with:
- What you did
- What you expected
- What actually happened
- Go version and OS

## Security issues

Do not open a public issue for security vulnerabilities. Email the maintainer directly instead.
