package pathutil

import _ "embed"

// ProjectConfigSchema is the JSON schema of bino.toml (ProjectConfig). The
// VS Code extension ships a copy for TOML language servers.
//
//go:embed bino-toml.schema.json
var ProjectConfigSchema []byte
