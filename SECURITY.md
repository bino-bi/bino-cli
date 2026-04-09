# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in bino, please report it responsibly.

**Do not open a public issue for security vulnerabilities.**

Instead, please email **sven@bino.bi** with:

- A description of the vulnerability
- Steps to reproduce the issue
- The potential impact
- Any suggested fixes (if applicable)

You should receive an acknowledgement within 72 hours. We will work with you to understand the issue and coordinate a fix before any public disclosure.

## Supported Versions

Security fixes are applied to the latest release only. We recommend always running the most recent version of bino.

| Version | Supported |
|---------|-----------|
| Latest  | Yes       |
| Older   | No        |

## Scope

The following are in scope for security reports:

- The `bino` CLI binary and its Go source code
- The install scripts (`install.sh`, `install.ps1`)
- The VS Code extension (`vscode-bino/`)

The following are out of scope:

- The documentation website (cli.bino.bi)
- Third-party dependencies (report these to their respective maintainers)
