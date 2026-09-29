package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"gocc2cc/internal/core"
)

// The channel notification is the one operation the whole application depends
// on, and the only one whose failure is invisible: if the shape is wrong,
// Claude Code renders nothing and reports nothing. So this asserts the exact
// bytes rather than the Go values, which also means a change to the contract
// surfaces here rather than as a tool that quietly stopped delivering.
func TestChannelNotificationWireFormat(t *testing.T) {
	at, err := time.Parse(time.RFC3339, "2026-09-11T09:43:55Z")
	if err != nil {
		t.Fatal(err)
	}
	m := core.Message{
		From:    "other/api::api-refactor::0192f3a1-b2c3-7def-8abc-123456789abc",
		To:      "work/webapp::docs-fix::0192f39c-1a2b-7cde-9fab-987654321abc",
		Content: "Done. 3 files changed.",
		At:      at,
	}

	note, err := channelNotification(m)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonrpc.EncodeMessage(note)
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"jsonrpc":"2.0","method":"notifications/claude/channel","params":{"content":"Done. 3 files changed.","meta":{"at":"2026-09-11T09:43:55Z","from":"other/api::api-refactor::0192f3a1-b2c3-7def-8abc-123456789abc","source":"gocc2cc"}}}`

	if string(raw) != want {
		t.Fatalf("wire format changed:\n want %s\n got  %s", want, raw)
	}
}

// A notification carries no id. With one it becomes a request, and Claude Code
// would be waiting for a reply that never comes.
func TestChannelNotificationHasNoID(t *testing.T) {
	note, err := channelNotification(core.Message{Content: "hi", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if note.IsCall() {
		t.Fatal("the channel frame must be a notification, not a call")
	}
}

// Meta values must be strings and its keys plain identifiers — snake_case, no
// hyphens. A key that does not conform is dropped silently.
func TestChannelMetaKeysAndValuesConform(t *testing.T) {
	note, err := channelNotification(core.Message{
		From: "a::b::c", Content: "x", At: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	var params struct {
		Content string          `json:"content"`
		Meta    json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal(note.Params, &params); err != nil {
		t.Fatal(err)
	}

	var meta map[string]any
	if err := json.Unmarshal(params.Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta) == 0 {
		t.Fatal("meta should not be empty")
	}
	for k, v := range meta {
		if _, ok := v.(string); !ok {
			t.Errorf("meta[%q] must be a string, got %T", k, v)
		}
		for _, c := range k {
			isIdentifier := c == '_' ||
				(c >= 'a' && c <= 'z') ||
				(c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9')
			if !isIdentifier {
				t.Errorf("meta key %q contains %q, which is not an identifier character", k, c)
			}
		}
	}
}

// Declining this revision is what keeps push working regardless of how the
// machine is configured: a server that negotiates it is silently not
// registered as a channel.
func TestTransportDeclinesTheRevisionThatBreaksChannels(t *testing.T) {
	tr := newChannelTransport(nil)
	if tr.SupportsProtocolVersion(declinedProtocolVersion) {
		t.Fatalf("%s must not be advertised — channels stop working on it", declinedProtocolVersion)
	}
	for _, v := range []string{"2025-03-26", "2025-06-18", "2025-11-25"} {
		if !tr.SupportsProtocolVersion(v) {
			t.Errorf("%s should still be supported", v)
		}
	}
}
