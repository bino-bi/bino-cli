package plugin

import goplugin "github.com/hashicorp/go-plugin"

// Handshake is shared between host and plugins. It prevents accidental
// execution of non-plugin binaries.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "BINO_PLUGIN",
	MagicCookieValue: "bino-plugin-v1",
}
