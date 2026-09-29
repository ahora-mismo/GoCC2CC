package broker

import (
	"sort"
	"sync"
	"time"

	"gocc2cc/internal/core"
)

// Conn is one agent's connection, as the registry sees it.
type Conn interface {
	// Send writes a frame and returns the write error. The error matters: it is
	// the only honest signal that a push reached the other side.
	Send(core.Frame) error
	// Close ends the connection, telling the agent why.
	Close(core.Reason)
}

type entry struct {
	handle      core.Handle
	conn        Conn
	connectedAt time.Time
	sent        int
	received    int
}

// Registry holds the live connections. There is no expiry and no sweeping: an
// entry is added when an agent registers and removed when its socket closes,
// which the operating system reports immediately on a local connection. The
// list is therefore exactly the set of open sockets, and cannot go stale.
type Registry struct {
	mu    sync.RWMutex
	items map[string]*entry
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{items: map[string]*entry{}}
}

// Add records a newly registered agent.
func (r *Registry) Add(h core.Handle, c Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[h.String()] = &entry{handle: h, conn: c, connectedAt: time.Now()}
}

// Remove drops an agent, typically because its socket closed.
func (r *Registry) Remove(handle string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, handle)
}

// Lookup finds an agent by its full handle.
func (r *Registry) Lookup(handle string) (Conn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.items[handle]
	if !ok {
		return nil, false
	}
	return e.conn, true
}

// MatchTopic returns every connected agent whose topic matches, which is how an
// ambiguous address is detected before anything is sent.
func (r *Registry) MatchTopic(topic string) []core.Handle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []core.Handle
	for _, e := range r.items {
		if e.handle.Topic == topic {
			out = append(out, e.handle)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Count is how many agents are connected.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}

// Instances lists the connected agents newest first.
//
// The ordering falls out of the handle itself: a UUIDv7 begins with a
// millisecond timestamp, so sorting by id descending is sorting by connection
// time, with no separate field to keep in step.
func (r *Registry) Instances() []core.Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]core.Instance, 0, len(r.items))
	for _, e := range r.items {
		out = append(out, core.Instance{
			Handle:      e.handle.String(),
			App:         e.handle.App,
			Topic:       e.handle.Topic,
			ID:          e.handle.ID,
			ConnectedAt: e.connectedAt,
			Sent:        e.sent,
			Received:    e.received,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// All returns every live connection, used when telling everyone the broker is
// going away.
func (r *Registry) All() []Conn {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Conn, 0, len(r.items))
	for _, e := range r.items {
		out = append(out, e.conn)
	}
	return out
}

func (r *Registry) countSent(handle string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.items[handle]; ok {
		e.sent++
	}
}

func (r *Registry) countReceived(handle string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.items[handle]; ok {
		e.received++
	}
}
