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

## Contributor License Agreement

bino-cli is distributed under **AGPLv3** and is also offered under separate **commercial/SaaS licenses** by the Project Owner. To make this dual-licensing model possible, all contributors must sign a Contributor License Agreement (CLA) before their pull request can be merged.

- **Individuals:** sign the [Individual CLA (ICLA)](CLA.md). The [CLA Assistant](https://cla-assistant.io/) bot will prompt you automatically on your first pull request.
- **Companies:** an authorized officer must also sign the [Corporate CLA (CCLA)](CCLA.md) and list authorized contributors in Schedule A.

Under the CLA, you retain copyright in your Contributions and grant the Project Owner the rights needed to distribute your Contributions under AGPLv3 and under commercial terms. See [CLA.md](CLA.md) for the full grant summary.

## License

bino-cli is licensed under the **GNU Affero General Public License v3.0 (AGPLv3)** — see the [LICENCE](LICENCE) file.
