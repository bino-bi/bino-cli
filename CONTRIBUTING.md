# Contributing to bino

Thank you for your interest in contributing to bino! This document provides guidelines and information for contributors.

## How to Contribute

### Reporting Bugs

- Use the [issue tracker](https://github.com/bino-bi/bino-cli/issues) to report bugs.
- Include your OS, Go version, and bino version (`bino version`).
- Provide a minimal manifest that reproduces the issue when possible.

### Suggesting Features

- Open a [discussion](https://github.com/bino-bi/bino-cli/discussions) to propose new features before writing code.
- Describe the use case and how the feature fits bino's design philosophy of declarative, YAML-driven report bundles.

### Submitting Changes

1. Fork the repository and create a branch from `main`.
2. If you add code, add tests. Run the test suite:
   ```bash
   go test -v -race ./...
   ```
3. Run the linter:
   ```bash
   golangci-lint run ./...
   ```
4. Ensure your commit messages are clear and descriptive.
5. Open a pull request against `main`.

## Development Setup

### Prerequisites

- Go 1.25 or later
- CGO enabled (required for DuckDB)
- Chrome headless shell (`bino setup` downloads it automatically)

### Building

```bash
goreleaser build --snapshot --clean --single-target
```

### Testing

```bash
go test -v -race ./...
go test -v -race -coverprofile=coverage.out ./...
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`).
- Use `golangci-lint` with the project's configuration.
- Keep functions focused and well-documented.

## License

By contributing, you agree that your contributions will be licensed under the GNU Affero General Public License v3.0 (AGPLv3). See the [LICENCE](LICENCE) file.
