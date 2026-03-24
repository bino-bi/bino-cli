package plugin

import (
	"context"

	pluginv1 "github.com/bino-bi/bino-plugin-sdk/proto/v1"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// binoGRPCPlugin implements go-plugin's GRPCPlugin interface.
// The host only uses the GRPCClient side. When a BinoHostServer is set,
// it is exposed to the plugin via the GRPCBroker for bidirectional calls.
type binoGRPCPlugin struct {
	goplugin.Plugin

	// hostServer is the BinoHost service exposed to plugins.
	// May be nil when no host service is needed.
	hostServer *BinoHostServer
}

func (p *binoGRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, _ *grpc.Server) error {
	// Server side is implemented by the SDK, not the host.
	return nil
}

func (p *binoGRPCPlugin) GRPCClient(_ context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	var hostServiceID uint32
	if p.hostServer != nil {
		hostServiceID = broker.NextId()
		go broker.AcceptAndServe(hostServiceID, func(opts []grpc.ServerOption) *grpc.Server {
			s := grpc.NewServer(opts...)
			pluginv1.RegisterBinoHostServer(s, p.hostServer)
			return s
		})
	}

	return &clientWithHost{
		client:        pluginv1.NewBinoPluginClient(c),
		hostServiceID: hostServiceID,
		hostServer:    p.hostServer,
	}, nil
}

// clientWithHost wraps a BinoPlugin gRPC client alongside the host service ID
// so that Init can pass the broker ID to the plugin.
type clientWithHost struct {
	client        pluginv1.BinoPluginClient
	hostServiceID uint32
	hostServer    *BinoHostServer
}

// pluginMapWithHost creates the go-plugin plugin map with an optional host service.
func pluginMapWithHost(hostServer *BinoHostServer) map[string]goplugin.Plugin {
	return map[string]goplugin.Plugin{
		"bino": &binoGRPCPlugin{hostServer: hostServer},
	}
}
