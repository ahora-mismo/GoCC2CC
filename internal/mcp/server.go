// Package mcp is the `gocc2cc mcp` run mode: a stdio MCP server that Claude
// Code spawns once per session, and which connects to the broker over the
// socket. It is a translator — MCP comes in one side, our own protocol goes out
// the other.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"gocc2cc/internal/core"
)

const instructions = `You are connected to gocc2cc, which lets Claude Code sessions on this machine
message each other directly.

Handles look like <app>::<topic>::<id>, where topic is this window's name — what
/rename sets. Always address a peer by its FULL handle from list_instances;
never compose one yourself.

YOUR WINDOW'S NAME is the name set with /rename, and nothing else.

When the user runs /rename during this session you see it: the command, its
output, and a note saying the session was renamed. Use exactly that name, and
call connect again each time it changes.

If no /rename is visible in this conversation — for example the window was named
before the session was resumed — then you do not know the name. Call
connect(topic: "") and ask the user what the window is called.

Never take the name from anywhere else. Not from ListAgents, not from a session
label, not from the project or directory name. Those are different things and
registering one of them is worse than registering nothing.`

// initGrace is how long registration waits after MCP initialization completes.
//
// A push is dropped silently if it arrives before Claude Code has registered
// the server as a channel — no error, nothing logged, the message simply never
// appears. Registering only once initialization has finished, and then pausing
// briefly, keeps that window closed.
const initGrace = 500 * time.Millisecond

// Shim is the MCP server together with its connection to the broker.
type Shim struct {
	paths     core.Paths
	client    *client
	transport *channelTransport

	ready sync.Once
	init  chan struct{}

	mu   sync.Mutex
	conn sdk.Connection // the live MCP connection, for pushing notifications
}

// Run starts the MCP server on stdin/stdout and blocks until it ends.
func Run(ctx context.Context, paths core.Paths) error {
	s := &Shim{
		paths:  paths,
		client: newClient(paths.Socket()),
		init:   make(chan struct{}),
	}

	s.client.onPush = s.push
	s.client.onDisconnect = s.disconnected

	server := sdk.NewServer(
		&sdk.Implementation{Name: "gocc2cc", Version: version()},
		&sdk.ServerOptions{
			Instructions: instructions,
			// Fires when the client sends notifications/initialized, which is
			// the earliest point a pushed message can actually be rendered.
			InitializedHandler: func(context.Context, *sdk.InitializedRequest) {
				s.ready.Do(func() { close(s.init) })
			},
			Capabilities: &sdk.ServerCapabilities{
				// Declaring this is what registers the notification listener.
				// If it ever stops being announced, push stops working with no
				// other symptom.
				Experimental: map[string]any{"claude/channel": map[string]any{}},
			},
		},
	)
	s.addTools(server)

	s.transport = newChannelTransport(&sdk.StdioTransport{})

	// The connection appears when the SDK connects; hold on to it so inbound
	// broker messages can be pushed through the same write path.
	go func() {
		conn, err := s.transport.wait(ctx)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conn = conn
		s.mu.Unlock()
	}()

	// Join the broker once the session can actually receive, never before, then
	// keep the registration in step with the session.
	go s.join(ctx)
	defer s.client.close()

	return server.Run(ctx, s.transport)
}

// join waits for initialization, then registers.
//
// The topic is left blank — it becomes "default" — because nothing here knows
// what the window is called. The agent does, and the session-start hook asks it
// to say so.
func (s *Shim) join(ctx context.Context) {
	select {
	case <-s.init:
	case <-ctx.Done():
		return
	}
	select {
	case <-time.After(initGrace):
	case <-ctx.Done():
		return
	}

	// No broker is an ordinary condition, not a fault: stay alive and say so
	// when asked, rather than failing the session.
	if err := s.client.dial(ctx); err != nil {
		return
	}
	if _, err := s.client.register(ctx, core.AppFromCwd(), ""); err != nil {
		fmt.Fprintf(os.Stderr, "gocc2cc: could not register: %v\n", err)
	}
}

func version() string {
	if v := os.Getenv("GOCC2CC_VERSION"); v != "" {
		return v
	}
	return "0.1.0"
}

// push renders an inbound message into the agent's context.
func (s *Shim) push(m core.Message) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return
	}
	if err := emitChannel(context.Background(), conn, m); err != nil {
		// stdout is the MCP stream; everything else goes to stderr.
		fmt.Fprintf(os.Stderr, "gocc2cc: could not deliver a message: %v\n", err)
	}
}

// disconnected tells the agent why it was dropped, so it can explain itself
// rather than discovering an opaque failure on its next send.
func (s *Shim) disconnected(reason core.Reason) {
	var text string
	switch reason {
	case core.ReasonKilled:
		text = "You have been disconnected from gocc2cc by the operator. " +
			"Agent-to-agent messaging is unavailable in this session. " +
			"Tell the user, and mention they can ask you to connect again."
	case core.ReasonShutdown:
		text = "gocc2cc has shut down. Agent-to-agent messaging is unavailable " +
			"until it is running again; the user can then ask you to connect."
	default:
		text = "The connection to gocc2cc ended."
	}
	s.push(core.Message{From: core.SystemSender, Content: text, At: nowUTC()})
}

// ── Tools ────────────────────────────────────────────────────────────────────

type listInput struct{}

type listOutput struct {
	Instances []instanceView `json:"instances"`
	Note      string         `json:"note,omitempty"`
}

type instanceView struct {
	Handle string `json:"handle"`
	App    string `json:"app"`
	Topic  string `json:"topic"`
	Self   bool   `json:"self,omitempty"`
}

type sendInput struct {
	To      string `json:"to" jsonschema:"the recipient's FULL handle, exactly as list_instances reported it"`
	Content string `json:"content" jsonschema:"what to say; state your intent in the text itself"`
}

type sendOutput struct {
	Delivered bool   `json:"delivered"`
	Detail    string `json:"detail,omitempty"`
}

type connectInput struct {
	Topic string `json:"topic,omitempty" jsonschema:"this window's name, or empty if it has none"`
}

type connectOutput struct {
	Handle string `json:"handle,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func (s *Shim) addTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{
		Name:        "list_instances",
		Description: "List the Claude Code sessions currently connected to gocc2cc. Use the full handle when sending.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ listInput) (*sdk.CallToolResult, listOutput, error) {
		if !s.client.connected() {
			return nil, listOutput{}, errNotConnected()
		}
		instances, err := s.client.list(ctx)
		if err != nil {
			return nil, listOutput{}, err
		}
		self := s.client.currentHandle()
		out := listOutput{}
		for _, i := range instances {
			out.Instances = append(out.Instances, instanceView{
				Handle: i.Handle, App: i.App, Topic: i.Topic, Self: i.Handle == self,
			})
		}
		if len(out.Instances) <= 1 {
			out.Note = "No other sessions are connected."
		}
		return nil, out, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name: "send_message",
		Description: "Send a message to one other Claude Code session. " +
			"If it reports delivered=false, tell the user — the message was not received and nothing is queued.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in sendInput) (*sdk.CallToolResult, sendOutput, error) {
		if !s.client.connected() {
			return nil, sendOutput{}, errNotConnected()
		}
		if strings.TrimSpace(in.To) == "" {
			return nil, sendOutput{}, errors.New("a recipient handle is required")
		}
		delivered, detail, err := s.client.send(ctx, in.To, in.Content)
		if err != nil {
			return nil, sendOutput{}, err
		}
		return nil, sendOutput{Delivered: delivered, Detail: detail}, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name: "connect",
		Description: "Tell gocc2cc what this window is called, and join if not already connected. " +
			"Pass the window's name as topic, or an empty topic if it has none. " +
			"Call this when asked, and again every time the user renames the window.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in connectInput) (*sdk.CallToolResult, connectOutput, error) {
		topic := strings.TrimSpace(in.Topic)

		// Safe to call when nothing has changed, so an agent can answer every
		// time it is asked without re-registering for no reason — a new
		// registration mints a new id, which would strand any peer holding the
		// old handle.
		if s.client.connected() {
			if current, err := core.ParseHandle(s.client.currentHandle()); err == nil {
				same := current.Topic == topic ||
					(topic == "" && current.Topic == core.DefaultTopic)
				if same {
					return nil, connectOutput{
						Handle: current.String(),
						Detail: "Already connected under this name; nothing changed.",
					}, nil
				}
			}
		}

		if !s.client.connected() {
			if err := s.client.dial(ctx); err != nil {
				return nil, connectOutput{}, errNotConnected()
			}
		}
		handle, err := s.client.register(ctx, core.AppFromCwd(), topic)
		if err != nil {
			return nil, connectOutput{}, err
		}
		detail := "Connected."
		if topic == "" {
			detail = "Connected without a window name."
		}
		return nil, connectOutput{Handle: handle, Detail: detail}, nil
	})
}

func errNotConnected() error {
	return errors.New("not connected to gocc2cc — it may not be running, or this session was " +
		"disconnected by the operator. Tell the user, and offer to connect again")
}
