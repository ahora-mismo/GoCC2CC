package mcp

import (
	"context"
	"sync"

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
	c, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
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
