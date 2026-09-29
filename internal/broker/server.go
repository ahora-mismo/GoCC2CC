package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"

	"gocc2cc/internal/core"
)

// Broker accepts agent connections over a Unix socket and routes messages
// between them. It knows nothing about the interface: it emits no events and
// holds no display state, so it runs perfectly well with nobody watching.
type Broker struct {
	paths    core.Paths
	registry *Registry
	log      *Log

	mu       sync.Mutex
	listener net.Listener
	server   *http.Server
}

// New creates a broker for the given layout.
func New(paths core.Paths) *Broker {
	return &Broker{paths: paths, registry: NewRegistry(), log: NewLog()}
}

// Instances lists connected agents, newest first.
func (b *Broker) Instances() []core.Instance { return b.registry.Instances() }

// Messages returns the log, newest first.
func (b *Broker) Messages() []core.Message { return b.log.Messages() }

// Count is how many agents are connected.
func (b *Broker) Count() int { return b.registry.Count() }

// Listen binds the socket and starts accepting connections.
//
// A socket file left behind by an unclean exit would otherwise block the bind
// forever, and its presence alone says nothing about whether another copy is
// running. So the question is settled by evidence: try to connect. If something
// answers, a broker is already live and this one refuses to start. If nothing
// does, the file is stale and is replaced.
func (b *Broker) Listen() error {
	if err := b.paths.EnsureRoot(); err != nil {
		return err
	}
	path := b.paths.Socket()

	if _, err := os.Stat(path); err == nil {
		conn, dialErr := net.DialTimeout("unix", path, 500*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return fmt.Errorf("gocc2cc is already running (socket %s is live)", path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing stale socket %s: %w", path, err)
		}
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return fmt.Errorf("securing %s: %w", path, err)
	}

	// A WebSocket connection opens as an HTTP upgrade, so the handshake needs
	// something that speaks HTTP. This is not a web server in any meaningful
	// sense: no port, no routes, one handler, on a socket only local processes
	// can open. After the upgrade, HTTP is out of the picture entirely.
	srv := &http.Server{Handler: http.HandlerFunc(b.handle)}

	b.mu.Lock()
	b.listener, b.server = ln, srv
	b.mu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "gocc2cc: broker stopped: %v\n", err)
		}
	}()
	return nil
}

// Shutdown tells every connected agent that gocc2cc is going away, then stops.
//
// Nothing is waited for. On a local socket a successful write leaves the data
// in the receiving side's buffer, where it stays readable, so closing straight
// afterwards does not discard it — and quitting can never hang on a session
// that has stopped reading.
func (b *Broker) Shutdown() {
	for _, c := range b.registry.All() {
		c.Close(core.ReasonShutdown)
	}

	b.mu.Lock()
	srv, ln := b.server, b.listener
	b.mu.Unlock()

	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	if ln != nil {
		_ = ln.Close()
	}
	_ = os.Remove(b.paths.Socket())
}

// Kill disconnects one agent at the operator's request. The agent is told why
// before its socket closes, so it can explain itself to its user rather than
// reporting an opaque failure later.
func (b *Broker) Kill(handle string) bool {
	conn, ok := b.registry.Lookup(handle)
	if !ok {
		return false
	}
	b.log.Add(core.Message{
		From:    core.SystemSender,
		To:      handle,
		Content: "Disconnected from gocc2cc by the operator.",
		At:      time.Now(),
	})
	conn.Close(core.ReasonKilled)
	return true
}

func (b *Broker) handle(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c := &wsConn{ws: ws}
	defer func() {
		if c.handle != "" {
			b.registry.Remove(c.handle)
		}
		c.closeSocket()
	}()

	ctx := r.Context()
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return // the peer went away; the operating system told us at once
		}
		var f core.Frame
		if err := json.Unmarshal(data, &f); err != nil {
			_ = c.Send(core.Frame{Kind: core.KindError, Message: "malformed frame"})
			continue
		}
		b.dispatch(c, f)
	}
}

func (b *Broker) dispatch(c *wsConn, f core.Frame) {
	switch f.Kind {
	case core.KindRegister:
		h, err := core.NewHandle(f.App, f.Topic)
		if err != nil {
			_ = c.Send(core.Frame{Kind: core.KindError, ReqID: f.ReqID, Message: err.Error()})
			return
		}
		if c.handle != "" {
			b.registry.Remove(c.handle)
		}
		c.handle = h.String()
		b.registry.Add(h, c)
		_ = c.Send(core.Frame{Kind: core.KindRegistered, ReqID: f.ReqID, Handle: c.handle})
		b.welcome(h)

	case core.KindList:
		_ = c.Send(core.Frame{Kind: core.KindInstances, ReqID: f.ReqID, Instances: b.registry.Instances()})

	case core.KindSend:
		if c.handle == "" {
			_ = c.Send(core.Frame{Kind: core.KindSendResult, ReqID: f.ReqID, Message: "not registered"})
			return
		}
		// The sender is stamped from the connection rather than taken from the
		// frame, so an agent cannot claim to be someone else.
		delivered, why := b.Deliver(c.handle, f.To, f.Content)
		_ = c.Send(core.Frame{Kind: core.KindSendResult, ReqID: f.ReqID, Delivered: delivered, Message: why})

	default:
		_ = c.Send(core.Frame{Kind: core.KindError, ReqID: f.ReqID, Message: fmt.Sprintf("unknown frame %q", f.Kind)})
	}
}

// wsConn adapts a WebSocket to the Conn the registry uses.
type wsConn struct {
	ws     *websocket.Conn
	handle string
	mu     sync.Mutex
}

func (c *wsConn) Send(f core.Frame) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, data)
}

func (c *wsConn) Close(reason core.Reason) {
	// Tell the agent why before the socket goes, so it can say something useful
	// to its user instead of discovering an opaque failure on its next send.
	_ = c.Send(core.Frame{Kind: core.KindDisconnect, Reason: reason})
	c.closeSocket()
}

func (c *wsConn) closeSocket() {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
}
