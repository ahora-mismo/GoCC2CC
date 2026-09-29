package core

import "time"

// Kind names a frame travelling over the socket between a shim and the broker.
// This protocol is entirely ours — it is not MCP, which is the separate
// conversation between Claude Code and the shim.
type Kind string

const (
	// Shim → broker.
	KindRegister Kind = "register" // join, carrying the handle's parts
	KindList     Kind = "list"     // who else is connected
	KindSend     Kind = "send"     // deliver this to one handle

	// Broker → shim.
	KindRegistered Kind = "registered"  // you are now this handle
	KindInstances  Kind = "instances"   // the answer to list
	KindSendResult Kind = "send_result" // whether the push was written
	KindPush       Kind = "push"        // a message for you
	KindDisconnect Kind = "disconnect"  // you are being dropped, and why
	KindError      Kind = "error"
)

// Reason says why a connection is ending. The distinction is load-bearing: an
// operator kill leaves a session disconnected until the user asks for it back,
// whereas a shutdown is simply gocc2cc going away. Collapsing them into one
// "disconnected" would make one case or the other behave wrongly.
type Reason string

const (
	ReasonKilled   Reason = "killed_by_operator"
	ReasonShutdown Reason = "broker_shutdown"
)

// Frame is the single envelope for everything on the socket. One struct rather
// than a union keeps encoding trivial; unused fields are omitted.
type Frame struct {
	Kind  Kind   `json:"kind"`
	ReqID string `json:"req_id,omitempty"` // correlates a reply to its request

	// KindRegister — the agent supplies the parts, never a composed handle.
	App   string `json:"app,omitempty"`
	Topic string `json:"topic,omitempty"`

	// KindRegistered
	Handle string `json:"handle,omitempty"`

	// KindInstances
	Instances []Instance `json:"instances,omitempty"`

	// KindSend, KindPush
	To      string    `json:"to,omitempty"`
	From    string    `json:"from,omitempty"`
	Content string    `json:"content,omitempty"`
	At      time.Time `json:"at,omitzero"`

	// KindSendResult — true only when the frame reached a live connection.
	Delivered bool `json:"delivered,omitempty"`

	// KindDisconnect
	Reason Reason `json:"reason,omitempty"`

	// KindError, and the explanation accompanying a failed send.
	Message string `json:"message,omitempty"`
}

// Instance is one connected agent, as reported to a peer that asked who is
// available.
type Instance struct {
	Handle      string    `json:"handle"`
	App         string    `json:"app"`
	Topic       string    `json:"topic"`
	ID          string    `json:"id"`
	ConnectedAt time.Time `json:"connected_at"`
	Sent        int       `json:"sent"`
	Received    int       `json:"received"`
}

// Message is one entry in the broker's log, as shown in the interface. A
// message from gocc2cc itself carries SystemSender as From.
type Message struct {
	From    string
	To      string
	Content string
	At      time.Time
}

// IsSystem reports whether this came from gocc2cc rather than from a peer.
func (m Message) IsSystem() bool { return m.From == SystemSender }
