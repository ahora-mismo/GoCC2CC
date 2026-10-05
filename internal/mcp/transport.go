package mcp

import (
	"context"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// declinedProtocolVersion cannot carry channel messages. A server that
// negotiates it is simply not registered as a channel by Claude Code, and push
// stops working with no error anywhere — the agents still appear connected and
// sends still report success, because they do reach the other process. Only
// delivery into the conversation stops.
//
// Declining it here means the revision is never advertised, so it can never be
// negotiated regardless of how the machine is configured.
const declinedProtocolVersion = "2026-07-28"

// channelTransport wraps the SDK's stdio transport for one reason: to keep hold
// of the live connection.
//
// The channel notification is server-initiated and carries a method name the
// SDK does not know, so there is no API for sending it. But the transport is a
// documented extension point, and the connection it hands back writes whole
// JSON-RPC messages — including ones we compose. Writes are safe to make
// concurrently, so the broker-reading goroutine can push straight through it.
type channelTransport struct {
	inner sdk.Transport

	mu    sync.Mutex
	conn  sdk.Connection
	ready chan struct{}
}

func newChannelTransport(inner sdk.Transport) *channelTransport {
	return &channelTransport{inner: inner, ready: make(chan struct{})}
}

// Connect implements sdk.Transport.
func (t *channelTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	inner, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	c := &noDiscoverConn{Connection: inner}
	t.mu.Lock()
	t.conn = c
	t.mu.Unlock()
	close(t.ready)
	return c, nil
}

// SupportsProtocolVersion implements sdk.ProtocolVersionSupporter, filtering
// the versions the server advertises.
func (t *channelTransport) SupportsProtocolVersion(version string) bool {
	return version != declinedProtocolVersion
}

// wait blocks until the connection exists, or the context ends.
func (t *channelTransport) wait(ctx context.Context) (sdk.Connection, error) {
	select {
	case <-t.ready:
		t.mu.Lock()
		defer t.mu.Unlock()
		return t.conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// discoverMethod is the probe a client sends to find out whether the server
// speaks declinedProtocolVersion.
const discoverMethod = "server/discover"

// noDiscoverConn answers server/discover with "method not found", as a server
// predating it would, so the probe never reaches the SDK.
//
// It has to. The SDK's discover handler marks the session initialized, so when
// the client — told declinedProtocolVersion is unsupported — falls back to the
// legacy initialize on the same connection, the SDK rejects it as a duplicate
// and the connection fails. Claude Code shows that as "Failed to reconnect: 0".
type noDiscoverConn struct {
	sdk.Connection
}

// Read implements sdk.Connection.
func (c *noDiscoverConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	for {
		msg, err := c.Connection.Read(ctx)
		if err != nil {
			return nil, err
		}
		req, ok := msg.(*jsonrpc.Request)
		if !ok || req.Method != discoverMethod || !req.IsCall() {
			return msg, nil
		}
		resp := &jsonrpc.Response{
			ID: req.ID,
			Error: &jsonrpc.Error{
				Code:    jsonrpc.CodeMethodNotFound,
				Message: "method not found: " + discoverMethod,
			},
		}
		if err := c.Connection.Write(ctx, resp); err != nil {
			return nil, err
		}
	}
}
