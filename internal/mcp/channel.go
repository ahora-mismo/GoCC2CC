package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"gocc2cc/internal/core"
)

// ChannelMethod is the notification Claude Code renders inline as a <channel>
// tag in the receiving agent's context. It is not part of MCP — it is a
// Claude Code extension that travels over the same connection.
const ChannelMethod = "notifications/claude/channel"

// ChannelSource identifies us in the rendered tag.
const ChannelSource = "gocc2cc"

// channelParams is the notification body.
//
// Meta values must be strings and its keys must be plain identifiers —
// snake_case, no hyphens. A key that does not conform is dropped silently,
// which is why this shape is defined once, here, and asserted byte for byte in
// the tests rather than being assembled at each call site.
type channelParams struct {
	Content string            `json:"content"`
	Meta    map[string]string `json:"meta"`
}

func buildChannelParams(m core.Message) channelParams {
	return channelParams{
		Content: m.Content,
		Meta: map[string]string{
			"source": ChannelSource,
			"from":   m.From,
			"at":     m.At.UTC().Format(time.RFC3339),
		},
	}
}

// emitChannel pushes a message into the agent's context.
//
// This is the one operation the whole application exists to perform, and the
// one whose failure is invisible: if the shape is wrong, Claude Code renders
// nothing and reports nothing. Keeping it to a single function means a change
// to the contract is a change to one place, and one test.
func emitChannel(ctx context.Context, conn sdk.Connection, m core.Message) error {
	note, err := channelNotification(m)
	if err != nil {
		return err
	}
	return conn.Write(ctx, note)
}

// channelNotification builds the frame. A JSON-RPC notification is a request
// with no id — that absence is what marks it as expecting no reply, and it is
// the same shape the SDK's own notifications take.
func channelNotification(m core.Message) (*jsonrpc.Request, error) {
	params, err := json.Marshal(buildChannelParams(m))
	if err != nil {
		return nil, err
	}
	return &jsonrpc.Request{Method: ChannelMethod, Params: params}, nil
}

func nowUTC() time.Time { return time.Now().UTC() }
