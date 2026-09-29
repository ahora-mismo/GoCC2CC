package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"gocc2cc/internal/core"
)

// ErrNoBroker means gocc2cc is not running. This is an ordinary condition
// rather than a fault: the operator may not have started it yet, or may have
// quit it while sessions are still open.
var ErrNoBroker = errors.New("gocc2cc is not running")

// client is the shim's connection to the broker.
type client struct {
	socket string

	mu      sync.Mutex
	ws      *websocket.Conn
	handle  string
	pending map[string]chan core.Frame
	nextID  int

	// onPush is called for each message arriving for this agent.
	onPush func(core.Message)
	// onDisconnect is called when the broker drops us, with its reason.
	onDisconnect func(core.Reason)
}

func newClient(socket string) *client {
	return &client{socket: socket, pending: map[string]chan core.Frame{}}
}

func (c *client) connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws != nil
}

func (c *client) currentHandle() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handle
}

// dial opens the socket and starts reading. A WebSocket handshake is an HTTP
// request, so the dialler is pointed at the Unix socket and the host in the URL
// is ignored.
func (c *client) dial(ctx context.Context) error {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", c.socket)
			},
		},
	}

	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(dialCtx, "ws://gocc2cc/", &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoBroker, err)
	}
	// Messages are small, but a long conversation turn could be sizeable.
	ws.SetReadLimit(1 << 20)

	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()

	go c.readLoop(context.Background(), ws)
	return nil
}

func (c *client) readLoop(ctx context.Context, ws *websocket.Conn) {
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			c.dropped()
			return
		}
		var f core.Frame
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		c.route(f)
	}
}

func (c *client) route(f core.Frame) {
	// A reply to something we asked.
	if f.ReqID != "" {
		c.mu.Lock()
		ch, ok := c.pending[f.ReqID]
		delete(c.pending, f.ReqID)
		c.mu.Unlock()
		if ok {
			ch <- f
			return
		}
	}

	switch f.Kind {
	case core.KindPush:
		if c.onPush != nil {
			c.onPush(core.Message{From: f.From, To: f.To, Content: f.Content, At: f.At})
		}
	case core.KindDisconnect:
		if c.onDisconnect != nil {
			c.onDisconnect(f.Reason)
		}
		c.dropped()
	}
}

// dropped forgets the connection. Nothing reconnects on its own: after an
// operator kill the session stays out until the user asks for it back, and the
// same applies when the broker was never there.
func (c *client) dropped() {
	c.mu.Lock()
	ws := c.ws
	c.ws = nil
	c.handle = ""
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if ws != nil {
		_ = ws.Close(websocket.StatusNormalClosure, "")
	}
}

// request sends a frame and waits for the matching reply.
func (c *client) request(ctx context.Context, f core.Frame) (core.Frame, error) {
	c.mu.Lock()
	ws := c.ws
	if ws == nil {
		c.mu.Unlock()
		return core.Frame{}, ErrNoBroker
	}
	c.nextID++
	id := fmt.Sprintf("r%d", c.nextID)
	f.ReqID = id
	ch := make(chan core.Frame, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	data, err := json.Marshal(f)
	if err != nil {
		return core.Frame{}, err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ws.Write(writeCtx, websocket.MessageText, data); err != nil {
		return core.Frame{}, fmt.Errorf("%w: %v", ErrNoBroker, err)
	}

	select {
	case reply, ok := <-ch:
		if !ok {
			return core.Frame{}, ErrNoBroker
		}
		return reply, nil
	case <-ctx.Done():
		return core.Frame{}, ctx.Err()
	case <-time.After(10 * time.Second):
		return core.Frame{}, errors.New("gocc2cc did not respond")
	}
}

// register joins the broker. The agent supplies the parts; the handle itself is
// built by gocc2cc, so nothing else can compose a malformed one.
func (c *client) register(ctx context.Context, app, topic string) (string, error) {
	reply, err := c.request(ctx, core.Frame{Kind: core.KindRegister, App: app, Topic: topic})
	if err != nil {
		return "", err
	}
	if reply.Kind == core.KindError {
		return "", errors.New(reply.Message)
	}
	c.mu.Lock()
	c.handle = reply.Handle
	c.mu.Unlock()
	return reply.Handle, nil
}

func (c *client) list(ctx context.Context) ([]core.Instance, error) {
	reply, err := c.request(ctx, core.Frame{Kind: core.KindList})
	if err != nil {
		return nil, err
	}
	return reply.Instances, nil
}

// send asks the broker to deliver a message, and reports whether the frame
// actually reached a live connection.
func (c *client) send(ctx context.Context, to, content string) (bool, string, error) {
	reply, err := c.request(ctx, core.Frame{Kind: core.KindSend, To: to, Content: content})
	if err != nil {
		return false, "", err
	}
	return reply.Delivered, reply.Message, nil
}

func (c *client) close() {
	c.dropped()
}
